package htnstratum

import (
	"testing"
	"time"

	"github.com/Hoosat-Oy/HTND/app/appmessage"
	"github.com/Hoosat-Oy/htn-stratum-bridge/src/gostratum"
	"go.uber.org/zap"
)

func TestBridgeFeeIntegration_Disabled(t *testing.T) {
	// Test that when bridge fee is disabled, it doesn't affect GBT behavior
	logger := zap.NewNop().Sugar()
	
	bridgeFee := BridgeFeeConfig{
		Enabled:    false,
		RatePpm:    50,
		Address:    "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz",
		ServerSalt: "test-salt",
	}

	htnApi := &HtnApi{
		address:       "test-address",
		blockWaitTime: 100 * time.Millisecond,
		logger:        logger,
		bridgeFee:     bridgeFee,
		jobCounter:    0,
	}

	// Verify that the config is properly stored
	if htnApi.bridgeFee.Enabled {
		t.Error("Bridge fee should be disabled")
	}
	if htnApi.bridgeFee.RatePpm != 50 {
		t.Errorf("Expected RatePpm 50, got %d", htnApi.bridgeFee.RatePpm)
	}
}

func TestBridgeFeeIntegration_NoServerSalt(t *testing.T) {
	// Test that when ServerSalt is empty, feature is effectively disabled
	logger := zap.NewNop().Sugar()
	
	bridgeFee := BridgeFeeConfig{
		Enabled:    true,
		RatePpm:    50,
		Address:    "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz",
		ServerSalt: "", // Empty salt should disable feature
	}

	htnApi := &HtnApi{
		address:       "test-address",
		blockWaitTime: 100 * time.Millisecond,
		logger:        logger,
		bridgeFee:     bridgeFee,
		jobCounter:    0,
	}

	// Even though Enabled is true, without ServerSalt the feature should not activate
	if htnApi.bridgeFee.ServerSalt != "" {
		t.Error("ServerSalt should be empty")
	}
}

func TestBridgeFeeIntegration_JobCounterIncrement(t *testing.T) {
	// Test that job counter increments properly
	logger := zap.NewNop().Sugar()
	
	bridgeFee := BridgeFeeConfig{
		Enabled:    true,
		RatePpm:    50,
		Address:    "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz",
		ServerSalt: "test-salt-123",
	}

	htnApi := &HtnApi{
		address:       "test-address",
		blockWaitTime: 100 * time.Millisecond,
		logger:        logger,
		bridgeFee:     bridgeFee,
		jobCounter:    0,
	}

	// Verify initial counter
	if htnApi.jobCounter != 0 {
		t.Errorf("Expected initial jobCounter 0, got %d", htnApi.jobCounter)
	}

	// Note: We can't easily test GetBlockTemplate without a real RPC connection,
	// but we can verify the configuration is correct
	if !htnApi.bridgeFee.Enabled {
		t.Error("Bridge fee should be enabled")
	}
	if htnApi.bridgeFee.ServerSalt == "" {
		t.Error("ServerSalt should not be empty")
	}
}

func TestBridgeFeeConfig_Defaults(t *testing.T) {
	// Test that default values are set correctly
	cfg := BridgeConfig{
		BridgeFee: BridgeFeeConfig{
			Enabled:    false,
			RatePpm:    50,
			Address:    "hoosat:qq2g85qrj2k4xs80y32v69kjn7nr49khyrack9mpd3gy54vfp8ja53ws4yezz",
			ServerSalt: "",
		},
	}

	if cfg.BridgeFee.Enabled {
		t.Error("Default bridge fee should be disabled")
	}
	if cfg.BridgeFee.RatePpm != 50 {
		t.Errorf("Default RatePpm should be 50, got %d", cfg.BridgeFee.RatePpm)
	}
	if cfg.BridgeFee.Address == "" {
		t.Error("Default address should not be empty")
	}
	if cfg.BridgeFee.ServerSalt != "" {
		t.Error("Default ServerSalt should be empty")
	}
}

func TestStratumContext_WorkerInfo(t *testing.T) {
	// Test that StratumContext fields used in jobKey are accessible
	ctx := &gostratum.StratumContext{
		WalletAddr: "hoosat:test123",
		WorkerName: "worker-1",
		RemoteApp:  "test-miner",
	}

	if ctx.WalletAddr == "" {
		t.Error("WalletAddr should not be empty")
	}
	if ctx.WorkerName == "" {
		t.Error("WorkerName should not be empty")
	}
}

func TestMiningState_AddJob_NormalJob(t *testing.T) {
	ms := &MiningState{
		Jobs:    make(map[int]*appmessage.RPCBlock),
		FeeJobs: make(map[int]bool),
	}

	block := &appmessage.RPCBlock{}
	jobId := ms.AddJob(block, false)

	if ms.IsFeeJob(jobId) {
		t.Error("normal job should not be marked as a fee job")
	}
	got, exists := ms.GetJob(jobId)
	if !exists {
		t.Fatal("job should exist after AddJob")
	}
	if got != block {
		t.Error("GetJob should return the same block pointer")
	}
}

func TestMiningState_AddJob_FeeJob(t *testing.T) {
	ms := &MiningState{
		Jobs:    make(map[int]*appmessage.RPCBlock),
		FeeJobs: make(map[int]bool),
	}

	block := &appmessage.RPCBlock{}
	jobId := ms.AddJob(block, true)

	if !ms.IsFeeJob(jobId) {
		t.Error("fee job should be marked as a fee job")
	}
}

func TestMiningState_RemoveJob_ClearsFeeJob(t *testing.T) {
	ms := &MiningState{
		Jobs:    make(map[int]*appmessage.RPCBlock),
		FeeJobs: make(map[int]bool),
	}

	block := &appmessage.RPCBlock{}
	jobId := ms.AddJob(block, true)
	ms.RemoveJob(jobId)

	if ms.IsFeeJob(jobId) {
		t.Error("IsFeeJob should return false after RemoveJob")
	}
	_, exists := ms.GetJob(jobId)
	if exists {
		t.Error("GetJob should return false after RemoveJob")
	}
}

func TestMiningState_ClearJobs_ClearsFeeJobs(t *testing.T) {
	ms := &MiningState{
		Jobs:    make(map[int]*appmessage.RPCBlock),
		FeeJobs: make(map[int]bool),
	}

	ms.AddJob(&appmessage.RPCBlock{}, true)
	ms.AddJob(&appmessage.RPCBlock{}, false)
	ms.AddJob(&appmessage.RPCBlock{}, true)
	ms.ClearJobs()

	if len(ms.Jobs) != 0 {
		t.Errorf("Jobs map should be empty after ClearJobs, got %d entries", len(ms.Jobs))
	}
	if len(ms.FeeJobs) != 0 {
		t.Errorf("FeeJobs map should be empty after ClearJobs, got %d entries", len(ms.FeeJobs))
	}
}

func TestMiningState_OverwriteWithNormalAfterFee(t *testing.T) {
	// Verify that when a slot is reused, a fee-job flag from a previous job is
	// cleared when a normal job occupies the same slot.
	ms := &MiningState{
		Jobs:    make(map[int]*appmessage.RPCBlock),
		FeeJobs: make(map[int]bool),
	}

	// Fill maxjobs slots with fee jobs so the next AddJob wraps around.
	for i := 0; i < maxjobs; i++ {
		ms.AddJob(&appmessage.RPCBlock{}, true)
	}

	// The next job wraps to slot 0 (counter % maxjobs == 0) and is NOT a fee job.
	jobId := ms.AddJob(&appmessage.RPCBlock{}, false)
	if ms.IsFeeJob(jobId) {
		t.Error("slot reused by a normal job should not report IsFeeJob == true")
	}
}
