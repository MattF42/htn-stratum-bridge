package db

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// BackupToReplica creates an atomic backup of the primary database to the replica
func BackupToReplica(primaryPath, replicaPath string) error {
	// Open primary database
	primaryDB, err := sql.Open("sqlite3", primaryPath)
	if err != nil {
		return err
	}
	defer primaryDB.Close()

	// Checkpoint to ensure WAL is flushed
	_, err = primaryDB.Exec("PRAGMA wal_checkpoint(RESTART)")
	if err != nil {
		log.Printf("Warning: WAL checkpoint failed: %v", err)
	}

	// Create temporary backup file
	tempPath := replicaPath + ".tmp"
	defer os.Remove(tempPath)

	// Open primary database file for copying
	srcFile, err := os.Open(primaryPath)
	if err != nil {
		return fmt.Errorf("failed to open primary database: %w", err)
	}
	defer srcFile.Close()

	// Create temporary file
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temporary backup: %w", err)
	}
	defer tempFile.Close()

	// Copy file contents
	if _, err := io.Copy(tempFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy database file: %w", err)
	}

	// Sync to disk
	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	// Close files before rename
	tempFile.Close()
	srcFile.Close()

	// Atomic rename
	if err := os.Rename(tempPath, replicaPath); err != nil {
		return fmt.Errorf("failed to rename backup file: %w", err)
	}

	log.Printf("Replica backup completed successfully at %s", time.Now().Format(time.RFC3339))
	return nil
}

// StartReplicaSync starts a background goroutine that syncs the replica periodically
func StartReplicaSync(primaryPath, replicaPath string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			if err := BackupToReplica(primaryPath, replicaPath); err != nil {
				log.Printf("Warning: Failed to backup to replica: %v", err)
				continue
			}
		}
	}()
}

// InitReplica opens a connection to the replica database for read-only operations
func InitReplica(replicaPath string) (*sql.DB, error) {
	// Open replica in read-only mode
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", replicaPath))
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
