package htnstratum

import (
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// BlockRecord represents a single mined-block entry persisted in SQLite.
type BlockRecord struct {
	ID            int64
	Timestamp     int64  // Unix milliseconds at submission time
	BlockHash     string // hex-encoded block hash
	WalletAddress string // miner payout address
	WorkerName    string // miner worker name
	RewardAtoms   uint64 // block reward in smallest unit (atoms); may be updated after confirmation
	Status	      string // "Pending", "Blue", "Red"
}

// MiningDB wraps a SQLite connection with a mutex to serialise writes,
// which is required because SQLite allows only one concurrent writer.
type MiningDB struct {
	db *sql.DB
	mu sync.Mutex
}

// InitDB opens (or creates) the SQLite database at the given file path,
// creates the required schema, and returns a ready-to-use *MiningDB.
func InitDB(path string) (*MiningDB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite db: %w", err)
	}

	// Enable WAL mode for better concurrent-read performance and reduce
	// write-contention between the recording goroutine and web queries.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	schema := `
CREATE TABLE IF NOT EXISTS block_rewards (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp      INTEGER NOT NULL,
    block_hash     TEXT    NOT NULL UNIQUE,
    wallet_address TEXT    NOT NULL,
    worker_name    TEXT    NOT NULL DEFAULT '',
    reward_atoms   INTEGER NOT NULL DEFAULT 0,
    status         TEXT DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_wallet ON block_rewards(wallet_address);
CREATE INDEX IF NOT EXISTS idx_timestamp ON block_rewards(timestamp);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &MiningDB{db: db}, nil
}

// RecordBlock inserts a new block record. Duplicate block hashes are silently
// ignored (INSERT OR IGNORE) so retries are safe.
func (d *MiningDB) RecordBlock(r BlockRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`INSERT OR IGNORE INTO block_rewards
		 (timestamp, block_hash, wallet_address, worker_name, reward_atoms)
		 VALUES (?, ?, ?, ?, ?)`,
		r.Timestamp, r.BlockHash, r.WalletAddress, r.WorkerName, r.RewardAtoms,
	)
	return err
}

// UpdateReward sets the reward_atoms for the row identified by block_hash.
// It is used by the async reward-lookup goroutine once the node confirms the block.
func (d *MiningDB) UpdateReward(blockHash string, rewardAtoms uint64, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, err := d.db.Exec(
		`UPDATE block_rewards SET reward_atoms = ?, status = ? WHERE block_hash = ?`,
		rewardAtoms, status, blockHash,
	)
	return err
}

// GetBlocksByWallet returns the most recent `limit` block records for the
// given wallet address, ordered newest-first.  If limit <= 0 it defaults to 200.
func (d *MiningDB) GetBlocksByWallet(walletAddr string, limit int) ([]BlockRecord, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := d.db.Query(
		`SELECT id, timestamp, block_hash, wallet_address, worker_name, reward_atoms
		 FROM block_rewards
		 WHERE wallet_address = ?
		 ORDER BY timestamp DESC
		 LIMIT ?`,
		walletAddr, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlockRecord
	for rows.Next() {
		var r BlockRecord
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.BlockHash, &r.WalletAddress, &r.WorkerName, &r.RewardAtoms); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountBlocksByWallet returns the total number of block records for a wallet.
func (d *MiningDB) CountBlocksByWallet(walletAddr string) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM block_rewards WHERE wallet_address = ?`,
		walletAddr,
	).Scan(&count)
	return count, err
}

// GetBlocksByWalletPaged returns `limit` block records for the given wallet
// starting at `offset`, ordered newest-first.  If limit <= 0 it defaults to 20.
func (d *MiningDB) GetBlocksByWalletPaged(walletAddr string, limit, offset int) ([]BlockRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := d.db.Query(
		`SELECT id, timestamp, block_hash, wallet_address, worker_name, reward_atoms, status
		 FROM block_rewards
		 WHERE wallet_address = ?
		 ORDER BY timestamp DESC
		 LIMIT ? OFFSET ?`,
		walletAddr, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BlockRecord
	for rows.Next() {
		var r BlockRecord
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.BlockHash, &r.WalletAddress, &r.WorkerName, &r.RewardAtoms, &r.Status); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}


func (db *MiningDB) GetBlockCountsByWallet(wallet string) (blue, red, pending int) {
    query := `SELECT 
        SUM(CASE WHEN status = 'blue' THEN 1 ELSE 0 END) as blue,
        SUM(CASE WHEN status = 'red' THEN 1 ELSE 0 END) as red,
        SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending
        FROM block_rewards WHERE wallet_address = ?`
    row := db.db.QueryRow(query, wallet)
    err := row.Scan(&blue, &red, &pending)
    if err != nil {
        // Handle error (log or return 0s)
        return 0, 0, 0
    }
    return
}

func (db *MiningDB) GetTotalAtomsByWallet(wallet string) (uint64, error) {
    var total uint64
    err := db.db.QueryRow(`SELECT COALESCE(SUM(reward_atoms), 0) FROM block_rewards WHERE wallet_address = ?`, wallet).Scan(&total)
    return total, err
}


// GetBlock returns a single BlockRecord by its block hash.
// Returns (nil, nil) if no record is found.
func (d *MiningDB) GetBlock(blockHash string) (*BlockRecord, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	row := d.db.QueryRow(
		`SELECT id, timestamp, block_hash, wallet_address, worker_name, reward_atoms, status
		 FROM block_rewards WHERE block_hash = ?`,
		blockHash,
	)

	var r BlockRecord
	err := row.Scan(&r.ID, &r.Timestamp, &r.BlockHash, &r.WalletAddress, &r.WorkerName, &r.RewardAtoms, &r.Status)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Close closes the underlying database connection.
func (d *MiningDB) Close() error {
	return d.db.Close()
}
