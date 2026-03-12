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
	FeeJobs     map[string]bool // Keyed by Block Hash string
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
		FeeJobs:     map[string]bool{}, // FIXED: map[string]bool
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
	if old := ms.Jobs[idx%maxjobs]; old != nil && old.VerboseData != nil {
		delete(ms.FeeJobs, old.VerboseData.Hash)
	}

	ms.Jobs[idx%maxjobs] = job
	
	if job != nil && job.VerboseData != nil {
		hash := job.VerboseData.Hash
		if isFeeJob {
			ms.FeeJobs[hash] = true
		} else {
			if _, alreadyFee := ms.FeeJobs[hash]; !alreadyFee {
                                delete(ms.FeeJobs, hash)
                        }
		}

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

// IsFeeJob reports whether the given block was generated as a bridge fee job.
// It uses the block's unique hash string as the key.
func (ms *MiningState) IsFeeJob(block *appmessage.RPCBlock) bool {
	if block == nil || block.VerboseData == nil {
		return false
	}
	ms.JobLock.Lock()
	isFee := ms.FeeJobs[block.VerboseData.Hash]
	ms.JobLock.Unlock()
	return isFee
}

func (ms *MiningState) RemoveJob(id int) {
	ms.JobLock.Lock()
	if block := ms.Jobs[id%maxjobs]; block != nil && block.VerboseData != nil {
		delete(ms.FeeJobs, block.VerboseData.Hash) // FIXED: delete by Hash string
	}
	delete(ms.Jobs, id%maxjobs)
	ms.JobLock.Unlock()
}

func (ms *MiningState) ClearJobs() {
	ms.JobLock.Lock()
	ms.Jobs = make(map[int]*appmessage.RPCBlock)
	ms.FeeJobs = make(map[string]bool) // FIXED: make(map[string]bool)
	ms.JobLock.Unlock()
}
