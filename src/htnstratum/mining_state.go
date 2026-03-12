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
	FeeJobs     map[int]bool
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
		FeeJobs:     map[int]bool{},
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
	ms.Jobs[idx%maxjobs] = job
	if isFeeJob {
		ms.FeeJobs[idx%maxjobs] = true
	} else {
		delete(ms.FeeJobs, idx%maxjobs)
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

// IsFeeJob reports whether the job with the given id was generated as a bridge
// fee job (i.e. the coinbase payout address was replaced with the bridge fee
// address instead of the miner's address).
func (ms *MiningState) IsFeeJob(id int) bool {
	ms.JobLock.Lock()
	isFee := ms.FeeJobs[id%maxjobs]
	ms.JobLock.Unlock()
	return isFee
}

func (ms *MiningState) RemoveJob(id int) {
	ms.JobLock.Lock()
	delete(ms.Jobs, id%maxjobs)
	delete(ms.FeeJobs, id%maxjobs)
	ms.JobLock.Unlock()
}

func (ms *MiningState) ClearJobs() {
	ms.JobLock.Lock()
	ms.Jobs = make(map[int]*appmessage.RPCBlock)
	ms.FeeJobs = make(map[int]bool)
	ms.JobLock.Unlock()
}
