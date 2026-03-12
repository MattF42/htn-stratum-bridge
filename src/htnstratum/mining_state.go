package htnstratum

import (
	"math/big"
	"sync"
	"time"

	"github.com/Hoosat-Oy/HTND/app/appmessage"
	"github.com/Hoosat-Oy/htn-stratum-bridge/src/gostratum"
)

const maxjobs = 32

type MiningState struct {
	Jobs        map[int]*appmessage.RPCBlock
	FeeJobs     map[*appmessage.RPCBlock]bool
	JobLock     sync.Mutex
	jobCounter  int
	bigDiff     big.Int
	initialized bool
	useBigJob   bool
	connectTime time.Time
	stratumDiff *hoosatDiff
}

func MiningStateGenerator() any {
	return &MiningState{
		Jobs:        map[int]*appmessage.RPCBlock{},
		FeeJobs:     map[*appmessage.RPCBlock]bool{},
		JobLock:     sync.Mutex{},
		connectTime: time.Now(),
	}
}

func GetMiningState(ctx *gostratum.StratumContext) *MiningState {
	return ctx.State.(*MiningState)
}

func (ms *MiningState) AddJob(job *appmessage.RPCBlock, isFeeJob bool) int {
	ms.jobCounter++
	idx := ms.jobCounter
	ms.JobLock.Lock()
	// Evict the outgoing block's fee-job entry when the ring-buffer slot is reused.
	if old := ms.Jobs[idx%maxjobs]; old != nil {
		delete(ms.FeeJobs, old)
	}
	ms.Jobs[idx%maxjobs] = job
	if isFeeJob {
		ms.FeeJobs[job] = true
	}
	ms.JobLock.Unlock()
	return idx
}

func (ms *MiningState) GetJob(id int) (*appmessage.RPCBlock, bool) {
	ms.JobLock.Lock()
	job, exists := ms.Jobs[id%maxjobs]
	ms.JobLock.Unlock()
	return job, exists
}

// IsFeeJob reports whether the given block was generated as a bridge fee job
// (i.e. the coinbase payout address was replaced with the bridge fee address
// instead of the miner's address).  It uses the block pointer as the key so
// that the result is immune to Stratum jobId ring-buffer aliasing.
func (ms *MiningState) IsFeeJob(block *appmessage.RPCBlock) bool {
	if block == nil {
		return false
	}
	ms.JobLock.Lock()
	isFee := ms.FeeJobs[block]
	ms.JobLock.Unlock()
	return isFee
}

func (ms *MiningState) RemoveJob(id int) {
	ms.JobLock.Lock()
	if block := ms.Jobs[id%maxjobs]; block != nil {
		delete(ms.FeeJobs, block)
	}
	delete(ms.Jobs, id%maxjobs)
	ms.JobLock.Unlock()
}

func (ms *MiningState) ClearJobs() {
	ms.JobLock.Lock()
	ms.Jobs = make(map[int]*appmessage.RPCBlock)
	ms.FeeJobs = make(map[*appmessage.RPCBlock]bool)
	ms.JobLock.Unlock()
}
