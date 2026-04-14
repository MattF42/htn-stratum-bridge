package pepepow

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Hoosat-Oy/htn-stratum-bridge/src/gostratum"
	"github.com/Hoosat-Oy/htn-stratum-bridge/src/pow"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PepepowHandler implements Bitcoin-style stratum protocol handlers for
// PePePow mining with hoohash.
type PepepowHandler struct {
	logger          *zap.SugaredLogger
	rpc             *RPCClient
	extranonce1Size int // bytes for extranonce1 (default 4)
	extranonce2Size int // bytes for extranonce2 (default 4)
	nextExtranonce  uint32
	mu              sync.Mutex

	// Current block template (shared across all clients)
	tmplMu       sync.RWMutex
	currentTmpl  *BlockTemplate
	jobCounter   uint64
	payoutScript []byte // default payout script (used for coinbase)
	coinbaseText string

	// Difficulty
	defaultDiff float64
}

// NewPepepowHandler creates a new handler for PePePow Bitcoin-style stratum.
func NewPepepowHandler(logger *zap.SugaredLogger, rpc *RPCClient, defaultDiff float64, coinbaseText string) *PepepowHandler {
	return &PepepowHandler{
		logger:          logger,
		rpc:             rpc,
		extranonce1Size: 4,
		extranonce2Size: 4,
		defaultDiff:     defaultDiff,
		coinbaseText:    coinbaseText,
	}
}

// allocateExtranonce1 returns a unique extranonce1 hex string for a new client.
func (h *PepepowHandler) allocateExtranonce1() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	en := h.nextExtranonce
	h.nextExtranonce++
	buf := make([]byte, h.extranonce1Size)
	binary.BigEndian.PutUint32(buf, en)
	return hex.EncodeToString(buf[4-h.extranonce1Size:])
}

// HandleSubscribe implements Bitcoin stratum mining.subscribe.
// Response: [[["mining.set_difficulty", sub_id], ["mining.notify", sub_id]], extranonce1, extranonce2_size]
func (h *PepepowHandler) HandleSubscribe(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
	state := GetState(ctx.State)
	if state == nil {
		return fmt.Errorf("no mining state")
	}

	if len(event.Params) > 0 {
		if app, ok := event.Params[0].(string); ok {
			ctx.RemoteApp = app
		}
	}

	subID := "deadbeefcafebabe7702000000000000"
	result := []any{
		[]any{
			[]any{"mining.set_difficulty", subID},
			[]any{"mining.notify", subID},
		},
		state.Extranonce1,
		h.extranonce2Size,
	}

	if err := ctx.Reply(gostratum.NewResponse(event, result, nil)); err != nil {
		return errors.Wrap(err, "failed to send subscribe response")
	}
	ctx.Logger.Debug("client subscribed (Bitcoin stratum)", zap.String("app", ctx.RemoteApp))
	return nil
}

// HandleAuthorize implements Bitcoin stratum mining.authorize.
// The PePePow address format is base58check (starts with 'P' for mainnet).
func (h *PepepowHandler) HandleAuthorize(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
	if len(event.Params) < 1 {
		return fmt.Errorf("malformed authorize: missing address param")
	}
	address, ok := event.Params[0].(string)
	if !ok {
		return fmt.Errorf("malformed authorize: param[0] not string")
	}

	parts := strings.Split(address, ".")
	var workerName string
	if len(parts) >= 2 {
		address = parts[0]
		workerName = parts[1]
	}

	// Basic validation for PePePow addresses (base58check, starts with P for mainnet)
	if len(address) < 25 || len(address) > 36 {
		return fmt.Errorf("invalid PePePow address length: %s", address)
	}

	ctx.WalletAddr = address
	ctx.WorkerName = workerName
	ctx.Logger = ctx.Logger.With(zap.String("worker", workerName), zap.String("addr", address))

	if err := ctx.Reply(gostratum.NewResponse(event, true, nil)); err != nil {
		return errors.Wrap(err, "failed to send authorize response")
	}

	// Send initial difficulty
	state := GetState(ctx.State)
	if state != nil && !state.Initialized {
		state.StratumDiff = h.defaultDiff
		state.Initialized = true
		if err := h.sendDifficulty(ctx, h.defaultDiff); err != nil {
			h.logger.Error("failed to send initial difficulty", zap.Error(err))
		}
	}

	ctx.Logger.Info("client authorized (PePePow)")
	return nil
}

// sendDifficulty sends mining.set_difficulty to the client.
func (h *PepepowHandler) sendDifficulty(ctx *gostratum.StratumContext, diff float64) error {
	return ctx.Send(gostratum.JsonRpcEvent{
		Version: "2.0",
		Method:  "mining.set_difficulty",
		Params:  []any{diff},
	})
}

// HandleSubmit processes a Bitcoin-style mining.submit.
// Params: [worker, job_id, extranonce2, ntime, nonce]
func (h *PepepowHandler) HandleSubmit(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
	if len(event.Params) < 5 {
		return ctx.Reply(gostratum.JsonRpcResponse{
			Id:    event.Id,
			Error: []any{20, "Malformed submit: need 5 params", nil},
		})
	}

	// Parse parameters
	_, ok := event.Params[0].(string) // worker name (ignored, we use ctx)
	if !ok {
		return ctx.ReplyIncorrectData(event.Id)
	}
	jobID, ok := event.Params[1].(string)
	if !ok {
		return ctx.ReplyIncorrectData(event.Id)
	}
	extranonce2Hex, ok := event.Params[2].(string)
	if !ok {
		return ctx.ReplyIncorrectData(event.Id)
	}
	ntimeHex, ok := event.Params[3].(string)
	if !ok {
		return ctx.ReplyIncorrectData(event.Id)
	}
	nonceHex, ok := event.Params[4].(string)
	if !ok {
		return ctx.ReplyIncorrectData(event.Id)
	}

	state := GetState(ctx.State)
	if state == nil {
		return ctx.ReplyBadShare(event.Id)
	}

	// Look up job
	job, exists := state.GetJob(jobID)
	if !exists {
		return ctx.ReplyStaleShare(event.Id)
	}

	// Parse ntime and nonce
	ntimeBytes := HexToBytes(ntimeHex)
	nonceBytes := HexToBytes(nonceHex)
	if len(ntimeBytes) != 4 || len(nonceBytes) != 4 {
		return ctx.ReplyIncorrectData(event.Id)
	}
	ntime := binary.LittleEndian.Uint32(ntimeBytes)
	nonce := binary.LittleEndian.Uint32(nonceBytes)

	// Reconstruct coinbase transaction
	extranonce2Bytes := HexToBytes(extranonce2Hex)
	if len(extranonce2Bytes) != h.extranonce2Size {
		return ctx.ReplyIncorrectData(event.Id)
	}

	coinbaseTx := HexToBytes(job.Coinbase1)
	coinbaseTx = append(coinbaseTx, HexToBytes(state.Extranonce1)...)
	coinbaseTx = append(coinbaseTx, extranonce2Bytes...)
	coinbaseTx = append(coinbaseTx, HexToBytes(job.Coinbase2)...)

	// Compute coinbase hash (double SHA256)
	coinbaseHash := DoubleSHA256(coinbaseTx)

	// Build merkle root
	merkleRoot := BuildMerkleRoot(coinbaseHash, job.MerkleBranches)

	// Construct 80-byte header
	header := BuildHeader(job.Version, job.PrevHash, merkleRoot[:], ntime, job.NBits, nonce)

	// Compute hoohash PoW
	powResult := pow.HoohashV110Bitcoin(header[:])

	// Convert to big.Int for comparison (result is already in LE order)
	powNum := new(big.Int).SetBytes(powResult[:])

	// Check against stratum difficulty target
	stratumTarget := DiffToTarget(state.StratumDiff)
	if powNum.Cmp(stratumTarget) > 0 {
		ctx.Logger.Debug("share below difficulty",
			zap.String("job", jobID),
			zap.String("pow", hex.EncodeToString(powResult[:])))
		return ctx.ReplyLowDiffShare(event.Id)
	}

	// Check against network target
	networkTarget := CompactToBig(job.NBits)
	if powNum.Cmp(networkTarget) <= 0 {
		// This share solves the block! Submit it.
		ctx.Logger.Info("BLOCK FOUND!",
			zap.String("job", jobID),
			zap.Int64("height", job.Height),
			zap.String("pow", hex.EncodeToString(powResult[:])))

		if err := h.submitBlock(header[:], coinbaseTx, job); err != nil {
			ctx.Logger.Error("block submission failed", zap.Error(err))
			return ctx.ReplyBadShare(event.Id)
		}
	}

	return ctx.ReplySuccess(event.Id)
}

// submitBlock constructs the full block and submits it to the node.
func (h *PepepowHandler) submitBlock(header []byte, coinbaseTx []byte, job *Job) error {
	// Build the full serialized block: header + varint(tx_count) + coinbase_tx + other_txs
	// For now we reconstruct from the template
	h.tmplMu.RLock()
	tmpl := h.currentTmpl
	h.tmplMu.RUnlock()

	var block []byte
	block = append(block, header...)

	// Transaction count (coinbase + template transactions)
	txCount := 1
	if tmpl != nil {
		txCount += len(tmpl.Transactions)
	}
	block = append(block, encodeVarInt(uint64(txCount))...)

	// Coinbase transaction
	block = append(block, coinbaseTx...)

	// Other transactions from the template
	if tmpl != nil {
		for _, tx := range tmpl.Transactions {
			txBytes := HexToBytes(tx.Data)
			block = append(block, txBytes...)
		}
	}

	return h.rpc.SubmitBlock(hex.EncodeToString(block))
}

// encodeVarInt encodes a uint64 as a Bitcoin variable-length integer.
func encodeVarInt(n uint64) []byte {
	switch {
	case n < 0xFD:
		return []byte{byte(n)}
	case n <= 0xFFFF:
		buf := make([]byte, 3)
		buf[0] = 0xFD
		binary.LittleEndian.PutUint16(buf[1:], uint16(n))
		return buf
	case n <= 0xFFFFFFFF:
		buf := make([]byte, 5)
		buf[0] = 0xFE
		binary.LittleEndian.PutUint32(buf[1:], uint32(n))
		return buf
	default:
		buf := make([]byte, 9)
		buf[0] = 0xFF
		binary.LittleEndian.PutUint64(buf[1:], n)
		return buf
	}
}

// SendJob sends a mining.notify to a specific client with the current block template.
func (h *PepepowHandler) SendJob(ctx *gostratum.StratumContext, tmpl *BlockTemplate, payoutScript []byte, cleanJobs bool) error {
	state := GetState(ctx.State)
	if state == nil {
		return fmt.Errorf("no mining state for client")
	}

	jobID := fmt.Sprintf("%x", atomic.AddUint64(&h.jobCounter, 1))

	totalExtranonceSize := h.extranonce1Size + h.extranonce2Size
	coinbase1, coinbase2 := BuildCoinbaseFromTemplate(tmpl, payoutScript, totalExtranonceSize, h.coinbaseText)

	// Parse prevhash (getblocktemplate returns it in RPC byte order = reversed)
	prevHashBytes := HexToBytes(tmpl.PreviousBlockHash)
	// The prevhash from GBT is in display order (big-endian), reverse to internal order
	prevHashInternal := ReverseBytes(prevHashBytes)

	// In Bitcoin stratum v1, the prevhash is sent as hex of the 32 bytes
	// with each 4-byte group byte-swapped
	prevHashStratum := hex.EncodeToString(SwapEndian32(prevHashInternal))

	// Build merkle branches from template transactions
	var txHashes [][32]byte
	for _, tx := range tmpl.Transactions {
		hashBytes := HexToBytes(tx.TxID)
		// TxID from GBT is in display order (big-endian), reverse to internal
		reversed := ReverseBytes(hashBytes)
		var h32 [32]byte
		copy(h32[:], reversed)
		txHashes = append(txHashes, h32)
	}
	branches := BuildMerkleBranches(txHashes)
	var branchesHex []string
	for _, b := range branches {
		branchesHex = append(branchesHex, hex.EncodeToString(b))
	}
	// If no branches, send empty array (not nil, to match JSON encoding)
	if branchesHex == nil {
		branchesHex = []string{}
	}

	// Version as hex (4 bytes, sent in the stratum protocol as big-endian hex)
	versionBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(versionBuf, tmpl.Version)
	versionHex := hex.EncodeToString(versionBuf)

	// nBits
	nbitsHex := tmpl.Bits

	// nTime as hex (4 bytes LE)
	ntimeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(ntimeBuf, uint32(tmpl.CurTime))
	ntimeHex := hex.EncodeToString(ntimeBuf)

	// Parse nbits for job storage
	nbits, err := ParseNBits(tmpl.Bits)
	if err != nil {
		return fmt.Errorf("invalid bits in template: %w", err)
	}

	// Store job
	job := &Job{
		ID:             jobID,
		PrevHash:       prevHashInternal,
		Coinbase1:      coinbase1,
		Coinbase2:      coinbase2,
		MerkleBranches: branches,
		Version:        tmpl.Version,
		NBits:          nbits,
		NTime:          uint32(tmpl.CurTime),
		Height:         tmpl.Height,
		Target:         HexToBytes(tmpl.Target),
		CleanJobs:      cleanJobs,
	}

	if cleanJobs {
		state.ClearJobs()
	}
	state.AddJob(job)

	// Save current template for block submission
	h.tmplMu.Lock()
	h.currentTmpl = tmpl
	h.tmplMu.Unlock()

	// Send mining.notify
	params := []any{
		jobID,
		prevHashStratum,
		coinbase1,
		coinbase2,
		branchesHex,
		versionHex,
		nbitsHex,
		ntimeHex,
		cleanJobs,
	}

	return ctx.Send(gostratum.JsonRpcEvent{
		Version: "2.0",
		Method:  "mining.notify",
		Params:  params,
	})
}
