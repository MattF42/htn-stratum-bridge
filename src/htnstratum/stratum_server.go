package htnstratum

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/Hoosat-Oy/htn-stratum-bridge/src/gostratum"
	"github.com/mattn/go-colorable"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const version = "v1.6.10"
const minBlockWaitTime = 100 * time.Millisecond

// Default bridge fee configuration
const (
	defaultBridgeFeeAddress = "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz"
	defaultBridgeFeeRatePpm = 50 // 0.5%
)

type BridgeFeeConfig struct {
	Enabled    bool   `yaml:"enabled"`
	RatePpm    int    `yaml:"rate_ppm"`
	Address    string `yaml:"address"`
	ServerSalt string `yaml:"server_salt"`
}

type BridgeConfig struct {
	StratumPort  string `yaml:"stratum_port"`
	RPCServer    string `yaml:"hoosat_address"`
	PromPort     string `yaml:"prom_port"`
	PrintStats   bool   `yaml:"print_stats"`
	RollingStats bool   `yaml:"rolling_stats"`

	UseLogFile        bool          `yaml:"log_to_file"`
	HealthCheckPort   string        `yaml:"health_check_port"`
	SoloMining        bool          `yaml:"solo_mining"`
	BlockWaitTime     time.Duration `yaml:"block_wait_time"`
	MinShareDiff      float64       `yaml:"min_share_diff"`
	VarDiff           bool          `yaml:"var_diff"`
	SharesPerMin      uint          `yaml:"shares_per_min"`
	VarDiffStats      bool          `yaml:"var_diff_stats"`
	ExtranonceSize    uint          `yaml:"extranonce_size"`
	MineWhenNotSynced bool          `yaml:"mine_when_not_synced"`
	Poll              int64         `yaml:"poll"`
	Vote              int64         `yaml:"vote"`

	BridgeFee BridgeFeeConfig `yaml:"bridge_fee"`

	// GBTCacheTTL is how long to cache GetBlockTemplate responses per payout address.
	// 0 disables caching. For a 5 BPS network, ~150ms is a good starting point.
	GBTCacheTTL time.Duration `yaml:"gbt_cache_ttl"`

	// WebPort is the address:port for the miner stats web UI (e.g. ":8080").
	// Leave empty to disable the web UI.
	WebPort string `yaml:"web_port"`
	StratumAddr string `yaml:"stratum_addr"`
}

func configureZap(cfg BridgeConfig) (*zap.SugaredLogger, func()) {
	pe := zap.NewProductionEncoderConfig()
	pe.EncodeTime = zapcore.RFC3339TimeEncoder
	fileEncoder := zapcore.NewJSONEncoder(pe)
	consoleEncoder := zapcore.NewConsoleEncoder(pe)

	if !cfg.UseLogFile {
		return zap.New(zapcore.NewCore(consoleEncoder,
			zapcore.AddSync(colorable.NewColorableStdout()), zap.InfoLevel)).Sugar(), func() {}
	}

	// log file fun
	logFile, err := os.OpenFile("bridge.log", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0666)
	if err != nil {
		panic(err)
	}
	core := zapcore.NewTee(
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logFile), zap.InfoLevel),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(colorable.NewColorableStdout()), zap.InfoLevel),
	)
	return zap.New(core).Sugar(), func() { logFile.Close() }
}

func ListenAndServe(cfg BridgeConfig) error {
	logger, logCleanup := configureZap(cfg)
	defer logCleanup()

	if cfg.PromPort != "" {
		StartPromServer(logger, cfg.PromPort)
	}

	blockWaitTime := max(cfg.BlockWaitTime, minBlockWaitTime)
	// Set default bridge fee config if not provided
	if cfg.BridgeFee.Address == "" {
		cfg.BridgeFee.Address = defaultBridgeFeeAddress
	}
	if cfg.BridgeFee.RatePpm == 0 {
		cfg.BridgeFee.RatePpm = defaultBridgeFeeRatePpm
	}

	htnApi, err := NewHoosatAPI(cfg.RPCServer, blockWaitTime, logger, cfg.BridgeFee, cfg.GBTCacheTTL)
	if err != nil {
		return err
	}

	// Initialise the SQLite mining database.
	miningDB, err := InitDB("mining.db")
	if err != nil {
		return fmt.Errorf("failed to open mining database: %w", err)
	}
	defer miningDB.Close()

	if cfg.HealthCheckPort != "" {
		logger.Info("enabling health check on port " + cfg.HealthCheckPort)
		http.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		registerMinerRewardsHandlers(htnApi) // <- New Rewards handler

		go http.ListenAndServe(cfg.HealthCheckPort, nil)
	}

	shareHandler := newShareHandler(htnApi.hoosat, cfg.RollingStats, htnApi.invalidateGBTCache, miningDB)

	// Start the miner stats web UI if a port is configured.  We pass the
	// shareHandler so that the /stats page can display live worker stats.
	if cfg.WebPort != "" && cfg.StratumAddr != "" {
		StartWebUI(miningDB, cfg.WebPort, logger, shareHandler, cfg.StratumAddr)
		// Recover pending rewards on startup
		rows, err := miningDB.db.Query("SELECT block_hash FROM block_rewards WHERE status == 'pending'")
		if err != nil {
    		logger.Error("Error querying pending blocks", zap.Error(err))
		} else {
    		defer rows.Close()
    		for rows.Next() {
        		var blockHash string
        		if err := rows.Scan(&blockHash); err != nil {
            		logger.Error("Error scanning block hash", zap.Error(err))
            		continue
        		}
        		go shareHandler.fetchAndUpdateReward(blockHash)
			time.Sleep(250 * time.Millisecond) // Do not overwhelm the node on startup
    		}
		}
	}





	minDiff := cfg.MinShareDiff
	if minDiff == 0 {
		minDiff = 4
	}
	extranonceSize := min(cfg.ExtranonceSize, 3)
	clientHandler := newClientListener(logger, shareHandler, minDiff, int8(extranonceSize))
	handlers := gostratum.DefaultHandlers()
	// override the submit handler with an actual useful handler
	handlers[string(gostratum.StratumMethodSubmit)] =
		func(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
			if err := shareHandler.HandleSubmit(ctx, event, cfg.SoloMining); err != nil {
				fmt.Printf("Error handling submit: %s", err)
			}
			return nil
		}


        // Foztor - create the stats handler here instead of blindly on connect
	handlers[string(gostratum.StratumMethodAuthorize)] =
		func(ctx *gostratum.StratumContext, event gostratum.JsonRpcEvent) error {
			if err := gostratum.HandleAuthorize(ctx, event); err != nil {
				return err
			}
			clientHandler.shareHandler.getCreateStats(ctx)
			return nil
		}

	stratumConfig := gostratum.StratumListenerConfig{
		Port:           cfg.StratumPort,
		HandlerMap:     handlers,
		StateGenerator: MiningStateGenerator,
		ClientListener: clientHandler,
		Logger:         logger.Desugar(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	htnApi.Start(ctx, cfg, func() {
		clientHandler.NewBlockAvailable(htnApi, cfg.SoloMining, cfg.Poll, cfg.Vote)
	})

	if cfg.VarDiff || cfg.SoloMining {
		go shareHandler.startVardiffThread(cfg.SharesPerMin, cfg.VarDiffStats)
	}

	if cfg.PrintStats {
		go shareHandler.startStatsThread()
	}

	return gostratum.NewListener(stratumConfig).Listen(context.Background())
}
