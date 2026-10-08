// Package server puts the layers of the database together into one that can be
// opened on a directory.
package server

import (
	"fmt"
	"sync"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// logFile is the name of the log, kept in the database directory alongside the
// tables.
const logFile = "gosql.log"

// DB is a database opened on a directory. It owns the managers every
// transaction on it shares: one of each, since two buffer pools or two lock
// tables over the same files would each be blind to what the other holds.
type DB struct {
	fileManager     *filemanager.FileManager
	logManager      *logmanager.LogManager
	bufferManager   *buffermanager.BufferManager
	lockTable       *concurrencymanager.LockTable
	metadataManager *metadatamanager.MetadataManager

	mu        sync.Mutex
	lastTxNum int
}

// Open opens the database kept in dir, creating the directory if there is none,
// with blocks of blockSize bytes and a pool of numBuffers buffers.
//
// A new database gets its catalogs; one that already existed is recovered, since
// it may have been left by a crash with changes on disk that were never
// committed. Either is done in a transaction of its own before Open returns, so
// no other transaction sees the database before it is ready.
func Open(dir string, blockSize, numBuffers int) (*DB, error) {
	fm, err := filemanager.NewFileManager(dir, blockSize)
	if err != nil {
		return nil, err
	}
	lm, err := logmanager.NewLogManager(fm, logFile)
	if err != nil {
		return nil, err
	}
	bm, err := buffermanager.NewBufferManager(fm, lm, numBuffers)
	if err != nil {
		return nil, err
	}
	mm, err := metadatamanager.NewMetadataManager()
	if err != nil {
		return nil, err
	}

	db := &DB{
		fileManager:     fm,
		logManager:      lm,
		bufferManager:   bm,
		lockTable:       concurrencymanager.NewLockTable(),
		metadataManager: mm,
	}

	tx, err := db.newTransaction()
	if err != nil {
		return nil, err
	}
	if fm.IsNew() {
		if err := mm.CreateCatalogTables(tx); err != nil {
			return nil, fmt.Errorf("open database %s: make catalogs: %w", dir, err)
		}
	} else {
		if err := tx.Recover(); err != nil {
			return nil, fmt.Errorf("open database %s: recover: %w", dir, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("open database %s: %w", dir, err)
	}

	return db, nil
}

// newTransaction starts a transaction on the database under the next number.
func (db *DB) newTransaction() (*transaction.Transaction, error) {
	db.mu.Lock()
	db.lastTxNum++
	txNum := db.lastTxNum
	db.mu.Unlock()

	return transaction.NewTransaction(db.fileManager, db.logManager, db.bufferManager, db.lockTable, txNum)
}

func (db *DB) FileManager() *filemanager.FileManager {
	return db.fileManager
}

func (db *DB) LogManager() *logmanager.LogManager {
	return db.logManager
}

func (db *DB) BufferManager() *buffermanager.BufferManager {
	return db.bufferManager
}

func (db *DB) MetadataManager() *metadatamanager.MetadataManager {
	return db.metadataManager
}
