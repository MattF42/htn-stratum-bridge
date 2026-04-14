package pepepow

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Hoosat-Oy/htn-stratum-bridge/src/gostratum"
	"github.com/mattn/go-colorable"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const pepepowVersion = "v0.1.0-pepepow"

// BridgeConfig holds the configuration for the PePePow stratum bridge.
type BridgeConfig struct {
	StratumPort string `yaml:"stratum_port"`

	// PePe-core node Bitcoin JSON-RPC connection
	NodeURL  string `yaml:"node_url"`  // e.g. "http://127.0.0.1:4330"
	NodeUser string `yaml:"node_user"` // RPC username
	NodePass string `yaml:"node_pass"` // RPC password

	PromPort        string `yaml:"prom_port"`
	PrintStats      bool   `yaml:"print_stats"`
	UseLogFile      bool   `yaml:"log_to_file"`
	HealthCheckPort string `yaml:"health_check_port"`

	// Mining parameters
	MinShareDiff   float64       `yaml:"min_share_diff"`
	BlockWaitTime  time.Duration `yaml:"block_wait_time"`
	CoinbaseText   string        `yaml:"coinbase_text"`

	// Payout address for the bridge/pool operator
	PayoutAddress string `yaml:"payout_address"`

	MineWhenNotSynced bool `yaml:"mine_when_not_synced"`
}

// ListenAndServe starts the PePePow stratum bridge.
func ListenAndServe(cfg BridgeConfig) error {
	logger, logCleanup := configureZapSimple(cfg)
	defer logCleanup()

	logger.Infof("Starting PePePow Stratum Bridge %s", pepepowVersion)

	// Connect to PePe-core node
	rpc := NewRPCClient(cfg.NodeURL, cfg.NodeUser, cfg.NodePass)

	// Check node sync
	if !cfg.MineWhenNotSynced {
		logger.Info("checking PePe-core node sync state...")
		for {
			synced, err := rpc.IsSynced()
			if err != nil {
				logger.Warnf("failed to check sync: %v, retrying...", err)
				time.Sleep(5 * time.Second)
				continue
			}
			if synced {
				break
			}
			logger.Warn("PePe-core node is not synced, waiting...")
			time.Sleep(5 * time.Second)
		}
		logger.Info("PePe-core node synced, starting bridge")
	}

	minDiff := cfg.MinShareDiff
	if minDiff == 0 {
		minDiff = 1
	}

	coinbaseText := cfg.CoinbaseText
	if coinbaseText == "" {
		coinbaseText = "/pepepow-bridge/"
	}

	handler := NewPepepowHandler(logger, rpc, minDiff, coinbaseText)

	// Build the stratum handler map (Bitcoin-style)
	handlers := gostratum.StratumHandlerMap{
		"mining.subscribe": func(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
			return handler.HandleSubscribe(ctx, event)
		},
		"mining.authorize": func(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
			return handler.HandleAuthorize(ctx, event)
		},
		"mining.submit": func(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
			return handler.HandleSubmit(ctx, event)
		},
	}

	// Client listener for managing connected miners
	clientMgr := &pepepowClientListener{
		logger:  logger,
		handler: handler,
		rpc:     rpc,
		clients: make(map[int32]*gostratum.StratumContext),
		cfg:     cfg,
	}

	stratumConfig := gostratum.StratumListenerConfig{
		Port:           cfg.StratumPort,
		HandlerMap:     handlers,
		StateGenerator: func() any {
			en1 := handler.allocateExtranonce1()
			return NewMiningState(en1)
		},
		ClientListener: clientMgr,
		Logger:         logger.Desugar(),
	}

	// Health check endpoint
	if cfg.HealthCheckPort != "" {
		logger.Info("enabling health check on port " + cfg.HealthCheckPort)
		http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		go http.ListenAndServe(cfg.HealthCheckPort, nil)
	}

	// Start block template polling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	blockWaitTime := cfg.BlockWaitTime
	if blockWaitTime < 500*time.Millisecond {
		blockWaitTime = 500 * time.Millisecond // PePePow has ~60s block times
	}

	go func() {
		// Poll for new block templates
		ticker := time.NewTicker(blockWaitTime)
		defer ticker.Stop()
		var lastHeight int64

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				tmpl, err := rpc.GetBlockTemplate()
				if err != nil {
					logger.Warnf("failed to get block template: %v", err)
					continue
				}

				cleanJobs := tmpl.Height != lastHeight
				lastHeight = tmpl.Height

				clientMgr.broadcastJob(tmpl, cleanJobs)
			}
		}
	}()

	logger.Infof("PePePow stratum listening on %s", cfg.StratumPort)
	return gostratum.NewListener(stratumConfig).Listen(context.Background())
}

// configureZapSimple creates a zap logger, optionally with file output.
func configureZapSimple(cfg BridgeConfig) (*zap.SugaredLogger, func()) {
	pe := zap.NewProductionEncoderConfig()
	pe.EncodeTime = zapcore.RFC3339TimeEncoder
	consoleEncoder := zapcore.NewConsoleEncoder(pe)

	if !cfg.UseLogFile {
		return zap.New(zapcore.NewCore(consoleEncoder,
			zapcore.AddSync(colorable.NewColorableStdout()), zap.InfoLevel)).Sugar(), func() {}
	}

	fileEncoder := zapcore.NewJSONEncoder(pe)
	logFile, err := os.OpenFile("pepepow_bridge.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		panic(err)
	}
	core := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logFile), zap.InfoLevel),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(colorable.NewColorableStdout()), zap.InfoLevel),
	)
	return zap.New(core).Sugar(), func() { logFile.Close() }
}

// pepepowClientListener manages connected PePePow miners.
type pepepowClientListener struct {
	logger  *zap.SugaredLogger
	handler *PepepowHandler
	rpc     *RPCClient
	mu      sync.Mutex
	clients map[int32]*gostratum.StratumContext
	counter int32
	cfg     BridgeConfig
}

func (c *pepepowClientListener) OnConnect(ctx *gostratum.StratumContext) {
	c.mu.Lock()
	c.counter++
	ctx.Id = c.counter
	c.clients[ctx.Id] = ctx
	c.mu.Unlock()
	ctx.Logger = ctx.Logger.With(zap.Int("client_id", int(ctx.Id)))
	c.logger.Infof("PePePow miner connected: %s (id=%d)", ctx.RemoteAddr, ctx.Id)
}

func (c *pepepowClientListener) OnDisconnect(ctx *gostratum.StratumContext) {
	ctx.Done()
	c.mu.Lock()
	delete(c.clients, ctx.Id)
	c.mu.Unlock()
	c.logger.Infof("PePePow miner disconnected: %s (id=%d)", ctx.RemoteAddr, ctx.Id)
}

// broadcastJob sends the current block template to all connected clients.
func (c *pepepowClientListener) broadcastJob(tmpl *BlockTemplate, cleanJobs bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, client := range c.clients {
		if !client.Connected() {
			continue
		}
		if client.WalletAddr == "" {
			continue // not yet authorized
		}

		// Build payout script for this miner's address
		// For simplicity, we use the address from the config as default payout
		// In a real pool, each miner would have their own payout script
		payoutScript := defaultPayoutScript(c.cfg.PayoutAddress)
		if payoutScript == nil {
			c.logger.Errorf("invalid payout address configuration: failed to decode address '%s'", c.cfg.PayoutAddress)
			continue
		}

		go func(ctx *gostratum.StratumContext) {
			if err := c.handler.SendJob(ctx, tmpl, payoutScript, cleanJobs); err != nil {
				c.logger.Warnf("failed to send job to client %d: %v", ctx.Id, err)
			}
		}(client)
	}
}

// defaultPayoutScript creates a simple P2PKH output script from a PePePow address.
// In a production implementation this would properly decode the base58check address.
// For now, we use a placeholder that should be replaced with proper address decoding.
func defaultPayoutScript(address string) []byte {
	if address == "" {
		return nil
	}
	// Decode base58check address to get the pubkey hash
	decoded := decodeBase58Check(address)
	if decoded == nil || len(decoded) < 21 {
		return nil
	}
	// First byte is the version, remaining 20 bytes are the pubkey hash
	return P2PKHScript(decoded[1:21])
}

// base58Alphabet is the Bitcoin base58 alphabet.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58Map provides O(1) character-to-index lookup for base58 decoding.
var base58Map [256]int

func init() {
	for i := range base58Map {
		base58Map[i] = -1
	}
	for i, c := range base58Alphabet {
		base58Map[c] = i
	}
}

// decodeBase58Check decodes a base58check-encoded string.
func decodeBase58Check(encoded string) []byte {
	if len(encoded) == 0 {
		return nil
	}

	// Decode base58 to big integer
	result := new(bigInt)
	for _, c := range encoded {
		if c > 255 {
			return nil // invalid character
		}
		idx := base58Map[c]
		if idx < 0 {
			return nil // invalid character
		}
		result.mul58()
		result.addSmall(idx)
	}

	// Convert to bytes
	b := result.bytes()

	// Count leading '1's for leading zero bytes
	var leadingZeros int
	for _, c := range encoded {
		if c != '1' {
			break
		}
		leadingZeros++
	}

	// Prepend leading zeros
	decoded := make([]byte, leadingZeros+len(b))
	copy(decoded[leadingZeros:], b)

	// Verify checksum (last 4 bytes)
	if len(decoded) < 5 {
		return nil
	}
	payload := decoded[:len(decoded)-4]
	checksum := decoded[len(decoded)-4:]
	hash := DoubleSHA256(payload)
	for i := 0; i < 4; i++ {
		if hash[i] != checksum[i] {
			return nil // checksum mismatch
		}
	}

	return payload
}

// bigInt is a simple big integer for base58 decoding.
type bigInt struct {
	data []byte
}

func (b *bigInt) mul58() {
	var carry int
	for i := len(b.data) - 1; i >= 0; i-- {
		val := int(b.data[i])*58 + carry
		b.data[i] = byte(val & 0xFF)
		carry = val >> 8
	}
	for carry > 0 {
		b.data = append([]byte{byte(carry & 0xFF)}, b.data...)
		carry >>= 8
	}
}

func (b *bigInt) addSmall(n int) {
	if len(b.data) == 0 {
		if n > 0 {
			b.data = []byte{byte(n)}
		}
		return
	}
	carry := n
	for i := len(b.data) - 1; i >= 0 && carry > 0; i-- {
		val := int(b.data[i]) + carry
		b.data[i] = byte(val & 0xFF)
		carry = val >> 8
	}
	for carry > 0 {
		b.data = append([]byte{byte(carry & 0xFF)}, b.data...)
		carry >>= 8
	}
}

func (b *bigInt) bytes() []byte {
	// Strip leading zeros
	for len(b.data) > 0 && b.data[0] == 0 {
		b.data = b.data[1:]
	}
	if len(b.data) == 0 {
		return nil
	}
	out := make([]byte, len(b.data))
	copy(out, b.data)
	return out
}

// MergedMiningNote documents why merged mining is not feasible between
// HTN (Kaspa-style) and PePePow (Bitcoin-style).
//
// HTN uses a Kaspa/GHOSTDAG-style block header that is serialized and hashed
// via BLAKE3 with domain-specific fields (parents, multiple merkle roots,
// DAA score, blue score, blue work, pruning point).  The PoW input is:
//     PRE_POW_HASH || TIMESTAMP || 32_ZERO_BYTES || NONCE
//
// PePePow uses a standard Bitcoin 80-byte header:
//     VERSION || PREVHASH || MERKLE_ROOT || TIME || BITS || NONCE
//
// The data being hashed for PoW is fundamentally different in both structure
// and content.  Even though both use the same hoohash matrix multiplication
// as the final PoW step, the inputs to the hash function are incompatible:
//
//  1. HTN's pre-PoW hash includes DAG-specific data (parents, multiple merkle
//     roots, DAA/blue scores) that has no equivalent in Bitcoin headers.
//
//  2. PePePow's header includes a Bitcoin-style merkle root and prevhash that
//     have no equivalent in the Kaspa header format.
//
//  3. The nonce field sizes differ: HTN uses uint64 (8 bytes) while PePePow
//     uses uint32 (4 bytes at header offset 76-79).
//
//  4. The matrix seed derivation differs: HTN zeros timestamp+nonce in the
//     Kaspa header hash, while PePePow zeros only nonce bytes [76:80] in the
//     80-byte header.
//
// Therefore, a single PoW computation cannot simultaneously satisfy both
// networks' validation rules.  Merged mining is NOT possible between these
// two chains.
func MergedMiningNote() string {
	return fmt.Sprintf("Merged mining between HTN (Kaspa-style) and PePePow (Bitcoin-style) " +
		"is NOT possible due to fundamentally incompatible header formats and PoW inputs. " +
		"See source code documentation for full analysis.")
}
