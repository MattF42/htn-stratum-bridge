package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// InitDatabase initializes the primary SQLite database with schema
func InitDatabase(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?cache=shared&mode=rwc", dbPath))
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite prefers single connection
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(0)

	// Create schema
	schema := `
	CREATE TABLE IF NOT EXISTS shares (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_wallet TEXT NOT NULL,
		worker_name TEXT,
		difficulty REAL NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		block_height INTEGER,
		is_block_found BOOLEAN DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS blocks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		block_hash TEXT UNIQUE NOT NULL,
		miner_wallet TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		reward_amount REAL NOT NULL,
		pool_fee_amount REAL NOT NULL
	);

	CREATE TABLE IF NOT EXISTS pending_payouts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_wallet TEXT UNIQUE NOT NULL,
		total_reward REAL NOT NULL DEFAULT 0,
		pool_fee_deducted REAL NOT NULL DEFAULT 0,
		payout_amount REAL NOT NULL DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS payout_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		miner_wallet TEXT NOT NULL,
		payout_amount REAL NOT NULL,
		transaction_hash TEXT,
		paid_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_shares_miner ON shares(miner_wallet);
	CREATE INDEX IF NOT EXISTS idx_shares_timestamp ON shares(timestamp);
	CREATE INDEX IF NOT EXISTS idx_blocks_miner ON blocks(miner_wallet);
	CREATE INDEX IF NOT EXISTS idx_blocks_timestamp ON blocks(timestamp);
	CREATE INDEX IF NOT EXISTS idx_payouts_miner ON payout_history(miner_wallet);
	`

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	return db, nil
}

// RecordShare inserts a new share record into the database
func RecordShare(db *sql.DB, minerWallet, workerName string, difficulty float64, blockHeight uint64, isBlockFound bool) error {
	query := `INSERT INTO shares (miner_wallet, worker_name, difficulty, block_height, is_block_found, timestamp)
			  VALUES (?, ?, ?, ?, ?, ?)`
	
	_, err := db.Exec(query, minerWallet, workerName, difficulty, blockHeight, isBlockFound, time.Now())
	return err
}

// RecordBlock inserts a new block record into the database
func RecordBlock(db *sql.DB, blockHash, minerWallet string, rewardAmount, poolFeeAmount float64) error {
	query := `INSERT INTO blocks (block_hash, miner_wallet, reward_amount, pool_fee_amount, timestamp)
			  VALUES (?, ?, ?, ?, ?)`
	
	_, err := db.Exec(query, blockHash, minerWallet, rewardAmount, poolFeeAmount, time.Now())
	return err
}

// GetPendingPayouts retrieves all pending payouts
func GetPendingPayouts(db *sql.DB) (map[string]float64, error) {
	query := `SELECT miner_wallet, payout_amount FROM pending_payouts WHERE payout_amount > 0`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payouts := make(map[string]float64)
	for rows.Next() {
		var wallet string
		var amount float64
		if err := rows.Scan(&wallet, &amount); err != nil {
			return nil, err
		}
		payouts[wallet] = amount
	}

	return payouts, rows.Err()
}

// MarkPayoutPaid moves a payout from pending to history
func MarkPayoutPaid(db *sql.DB, minerWallet, txHash string) error {
	// Get the payout amount from pending
	var payoutAmount float64
	err := db.QueryRow(`SELECT payout_amount FROM pending_payouts WHERE miner_wallet = ?`, minerWallet).Scan(&payoutAmount)
	if err != nil {
		return err
	}

	// Insert into history
	_, err = db.Exec(`INSERT INTO payout_history (miner_wallet, payout_amount, transaction_hash, paid_at)
					  VALUES (?, ?, ?, ?)`, minerWallet, payoutAmount, txHash, time.Now())
	if err != nil {
		return err
	}

	// Delete from pending
	_, err = db.Exec(`DELETE FROM pending_payouts WHERE miner_wallet = ?`, minerWallet)
	return err
}

// UpdatePendingPayout updates or inserts a pending payout for a miner
func UpdatePendingPayout(db *sql.DB, minerWallet string, totalReward, poolFeeDeducted float64) error {
	payoutAmount := totalReward - poolFeeDeducted
	
	query := `INSERT INTO pending_payouts (miner_wallet, total_reward, pool_fee_deducted, payout_amount, updated_at)
			  VALUES (?, ?, ?, ?, ?)
			  ON CONFLICT(miner_wallet) DO UPDATE SET 
				total_reward = total_reward + ?,
				pool_fee_deducted = pool_fee_deducted + ?,
				payout_amount = payout_amount + ?,
				updated_at = ?`
	
	_, err := db.Exec(query, minerWallet, totalReward, poolFeeDeducted, payoutAmount, time.Now(),
		totalReward, poolFeeDeducted, payoutAmount, time.Now())
	return err
}
