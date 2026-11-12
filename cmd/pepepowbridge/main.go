package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/Hoosat-Oy/htn-stratum-bridge/src/pepepow"
	"gopkg.in/yaml.v2"
)

func main() {
	pwd, _ := os.Getwd()
	fullPath := path.Join(pwd, "config.yaml")
	log.Printf("loading config @ `%s`", fullPath)

	cfg := pepepow.BridgeConfig{}

	rawCfg, err := os.ReadFile(fullPath)
	if err != nil {
		log.Printf("config file not found: %s (using defaults + flags)", err)
	} else {
		if err := yaml.Unmarshal(rawCfg, &cfg); err != nil {
			log.Printf("failed parsing config file: %s", err)
			os.Exit(1)
		}
	}

	flag.StringVar(&cfg.StratumPort, "stratum", cfg.StratumPort, "stratum port to listen on, e.g. :3333")
	flag.StringVar(&cfg.NodeURL, "node", cfg.NodeURL, "PePe-core node RPC URL, e.g. http://127.0.0.1:4330")
	flag.StringVar(&cfg.NodeUser, "rpcuser", cfg.NodeUser, "PePe-core RPC username")
	flag.StringVar(&cfg.NodePass, "rpcpass", cfg.NodePass, "PePe-core RPC password")
	flag.Float64Var(&cfg.MinShareDiff, "mindiff", cfg.MinShareDiff, "minimum share difficulty")
	flag.DurationVar(&cfg.BlockWaitTime, "blockwait", cfg.BlockWaitTime, "block template poll interval")
	flag.BoolVar(&cfg.PrintStats, "stats", cfg.PrintStats, "print periodic stats to console")
	flag.BoolVar(&cfg.UseLogFile, "log", cfg.UseLogFile, "log to file")
	flag.StringVar(&cfg.PromPort, "prom", cfg.PromPort, "prometheus metrics port")
	flag.StringVar(&cfg.HealthCheckPort, "hcp", cfg.HealthCheckPort, "health check port")
	flag.StringVar(&cfg.CoinbaseText, "coinbasetext", cfg.CoinbaseText, "text to include in coinbase")
	flag.BoolVar(&cfg.MineWhenNotSynced, "minewhennotsynced", cfg.MineWhenNotSynced, "mine when node is not synced")

	// Bridge fee flags
	flag.BoolVar(&cfg.BridgeFee.Enabled, "fee", cfg.BridgeFee.Enabled, "enable bridge fee")
	flag.IntVar(&cfg.BridgeFee.RatePpm, "feerate", cfg.BridgeFee.RatePpm, "bridge fee rate in parts per 10000 (50 = 0.5%)")
	flag.StringVar(&cfg.BridgeFee.Address, "feeaddr", cfg.BridgeFee.Address, "bridge fee payout address (PePePow P2PKH)")
	flag.StringVar(&cfg.BridgeFee.ServerSalt, "feesalt", cfg.BridgeFee.ServerSalt, "bridge fee HMAC server salt")
	flag.Parse()

	// Defaults
	if cfg.StratumPort == "" {
		cfg.StratumPort = ":3333"
	}
	if cfg.NodeURL == "" {
		cfg.NodeURL = "http://127.0.0.1:4330"
	}
	if cfg.MinShareDiff == 0 {
		cfg.MinShareDiff = 1
	}
	if cfg.BlockWaitTime == 0 {
		cfg.BlockWaitTime = 1 * time.Second
	}
	// Default bridge fee rate
	if cfg.BridgeFee.Enabled && cfg.BridgeFee.RatePpm == 0 {
		cfg.BridgeFee.RatePpm = 50 // 0.5%
	}

	log.Println("============================================================")
	log.Println("  PePePow Stratum Bridge (hoohash) — Solo Mining")
	log.Println("============================================================")
	log.Printf("node:          %s", cfg.NodeURL)
	log.Printf("stratum:       %s", cfg.StratumPort)
	log.Printf("prom:          %s", cfg.PromPort)
	log.Printf("min diff:      %.4f", cfg.MinShareDiff)
	log.Printf("block wait:    %s", cfg.BlockWaitTime)
	log.Printf("coinbase text: %s", cfg.CoinbaseText)
	if cfg.BridgeFee.Enabled {
		log.Printf("bridge fee:    enabled (rate=%d/10000, addr=%s)", cfg.BridgeFee.RatePpm, cfg.BridgeFee.Address)
	} else {
		log.Printf("bridge fee:    disabled")
	}
	log.Printf("mode:          solo (each miner's address used for coinbase)")
	log.Println("============================================================")

	// Note about merged mining
	fmt.Println(pepepow.MergedMiningNote())

	if err := pepepow.ListenAndServe(cfg); err != nil {
		log.Println(err)
	}
}
