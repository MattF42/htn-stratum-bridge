package db

import (
	"database/sql"
	"time"
)

// MinerStats holds statistics for a single miner
type MinerStats struct {
	Wallet          string    `json:"wallet"`
	ShareCount      int64     `json:"share_count"`
	BlocksFound     int64     `json:"blocks_found"`
	TotalReward     float64   `json:"total_reward"`
	PoolFeeDeducted float64   `json:"pool_fee_deducted"`
	PendingPayout   float64   `json:"pending_payout"`
	LastShareTime   time.Time `json:"last_share_time"`
}

// PoolStats holds overall pool statistics
type PoolStats struct {
	TotalShares      int64   `json:"total_shares"`
	TotalBlocks      int64   `json:"total_blocks"`
	TotalReward      float64 `json:"total_reward"`
	TotalPoolFees    float64 `json:"total_pool_fees"`
	PendingPayouts   float64 `json:"pending_payouts"`
	ProcessedPayouts float64 `json:"processed_payouts"`
}

// GetMinerStats retrieves statistics for a specific miner from the replica database
func GetMinerStats(replicaDB *sql.DB, minerWallet string) (*MinerStats, error) {
	stats := &MinerStats{
		Wallet: minerWallet,
	}

	// Get share count
	err := replicaDB.QueryRow(`SELECT COUNT(*) FROM shares WHERE miner_wallet = ?`, minerWallet).Scan(&stats.ShareCount)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get blocks found
	err = replicaDB.QueryRow(`SELECT COUNT(*) FROM blocks WHERE miner_wallet = ?`, minerWallet).Scan(&stats.BlocksFound)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get total reward and fees
	err = replicaDB.QueryRow(`SELECT COALESCE(SUM(reward_amount), 0), COALESCE(SUM(pool_fee_amount), 0) FROM blocks WHERE miner_wallet = ?`, minerWallet).Scan(&stats.TotalReward, &stats.PoolFeeDeducted)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get pending payout
	err = replicaDB.QueryRow(`SELECT COALESCE(payout_amount, 0) FROM pending_payouts WHERE miner_wallet = ?`, minerWallet).Scan(&stats.PendingPayout)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get last share time
	err = replicaDB.QueryRow(`SELECT MAX(timestamp) FROM shares WHERE miner_wallet = ?`, minerWallet).Scan(&stats.LastShareTime)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return stats, nil
}

// GetMinerHistory retrieves recent shares and blocks for a miner
func GetMinerHistory(replicaDB *sql.DB, minerWallet string, limit int) ([]map[string]interface{}, error) {
	query := `
	SELECT 'share' as type, timestamp, difficulty as value, worker_name FROM shares WHERE miner_wallet = ?
	UNION ALL
	SELECT 'block' as type, timestamp, reward_amount as value, block_hash FROM blocks WHERE miner_wallet = ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := replicaDB.Query(query, minerWallet, minerWallet, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var recordType, detail string
		var timestamp time.Time
		var value float64

		if err := rows.Scan(&recordType, &timestamp, &value, &detail); err != nil {
			return nil, err
		}

		record := map[string]interface{}{
			"type":      recordType,
			"timestamp": timestamp,
			"value":     value,
			"detail":    detail,
		}
		history = append(history, record)
	}

	return history, rows.Err()
}

// GetPoolStats retrieves overall pool statistics
func GetPoolStats(replicaDB *sql.DB) (*PoolStats, error) {
	stats := &PoolStats{}

	// Get total shares
	err := replicaDB.QueryRow(`SELECT COUNT(*) FROM shares`).Scan(&stats.TotalShares)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get total blocks
	err = replicaDB.QueryRow(`SELECT COUNT(*) FROM blocks`).Scan(&stats.TotalBlocks)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get total rewards and fees
	err = replicaDB.QueryRow(`SELECT COALESCE(SUM(reward_amount), 0), COALESCE(SUM(pool_fee_amount), 0) FROM blocks`).Scan(&stats.TotalReward, &stats.TotalPoolFees)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get pending payouts
	err = replicaDB.QueryRow(`SELECT COALESCE(SUM(payout_amount), 0) FROM pending_payouts`).Scan(&stats.PendingPayouts)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// Get processed payouts
	err = replicaDB.QueryRow(`SELECT COALESCE(SUM(payout_amount), 0) FROM payout_history`).Scan(&stats.ProcessedPayouts)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	return stats, nil
}

// GetTopMiners retrieves the top N miners by pending payout
func GetTopMiners(replicaDB *sql.DB, limit int) ([]MinerStats, error) {
	query := `
	SELECT COALESCE(shares.miner_wallet, pending_payouts.miner_wallet),
	       COUNT(DISTINCT CASE WHEN shares.is_block_found THEN 1 END) as blocks,
	       COUNT(shares.id) as shares, 
	       COALESCE(pending_payouts.payout_amount, 0) as pending
	FROM shares
	LEFT JOIN pending_payouts ON shares.miner_wallet = pending_payouts.miner_wallet
	GROUP BY COALESCE(shares.miner_wallet, pending_payouts.miner_wallet)
	ORDER BY pending DESC
	LIMIT ?
	`

	rows, err := replicaDB.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var miners []MinerStats
	for rows.Next() {
		var stats MinerStats
		var blocks, shares int64
		var pending float64

		if err := rows.Scan(&stats.Wallet, &blocks, &shares, &pending); err != nil {
			return nil, err
		}

		stats.BlocksFound = blocks
		stats.ShareCount = shares
		stats.PendingPayout = pending
		miners = append(miners, stats)
	}

	return miners, rows.Err()
}
