package pepepow

import (
	"sync"
)

const maxJobs = 64

// Job holds all the data needed to validate a share submission for one
// mining.notify job.
type Job struct {
	ID             string
	PrevHash       []byte   // 32 bytes, internal byte order
	Coinbase1      string   // hex
	Coinbase2      string   // hex
	MerkleBranches [][]byte // list of 32-byte hashes
	Version        uint32
	NBits          uint32
	NTime          uint32
	Height         int64
	Target         []byte // 32-byte target from template
	CleanJobs      bool
}

// MiningState holds per-connection mining state for a PePePow miner.
type MiningState struct {
	mu          sync.Mutex
	jobs        map[string]*Job // keyed by job ID
	jobOrder    []string        // ring buffer of job IDs for eviction
	Extranonce1 string          // hex string assigned to this client
	Initialized bool
	// Current stratum difficulty
	StratumDiff float64
}

// NewMiningState creates a new per-client mining state.
func NewMiningState(extranonce1 string) *MiningState {
	return &MiningState{
		jobs:        make(map[string]*Job),
		Extranonce1: extranonce1,
	}
}

// AddJob stores a new job, evicting old ones when the ring buffer is full.
func (ms *MiningState) AddJob(job *Job) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.jobs[job.ID] = job
	ms.jobOrder = append(ms.jobOrder, job.ID)

	// Evict oldest if over limit
	for len(ms.jobOrder) > maxJobs {
		oldest := ms.jobOrder[0]
		ms.jobOrder = ms.jobOrder[1:]
		delete(ms.jobs, oldest)
	}
}

// GetJob retrieves a stored job by ID.
func (ms *MiningState) GetJob(id string) (*Job, bool) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	job, ok := ms.jobs[id]
	return job, ok
}

// ClearJobs removes all stored jobs (e.g., on clean_jobs=true).
func (ms *MiningState) ClearJobs() {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.jobs = make(map[string]*Job)
	ms.jobOrder = nil
}

// MiningStateGenerator returns a factory function suitable for the stratum
// listener's StateGenerator field.
func MiningStateGenerator(extranonce1 string) func() any {
	return func() any {
		return NewMiningState(extranonce1)
	}
}

// GetState extracts the MiningState from a stratum context's State field.
func GetState(state any) *MiningState {
	if ms, ok := state.(*MiningState); ok {
		return ms
	}
	return nil
}
