// Package server combines the layers of the database into one database that
// can be opened on a directory.
package server

import (
	"errors"
	"fmt"
	"sync"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/plan"
	"github.com/JunNishimura/GoSQL/transaction"
)

// logFile is the name of the log file. It is stored in the database directory
// together with the tables.
const logFile = "gosql.log"

// DB is a database opened on a directory. It owns the managers that all its
// transactions share. There is exactly one of each: two buffer pools or two
// lock tables over the same files would not see each other's state.
type DB struct {
	fileManager     *filemanager.FileManager
	logManager      *logmanager.LogManager
	bufferManager   *buffermanager.BufferManager
	lockTable       *concurrencymanager.LockTable
	metadataManager *metadatamanager.MetadataManager
	planner         *plan.Planner

	mu        sync.Mutex
	lastTxNum int
}

// Open opens the database stored in dir, with blocks of blockSize bytes and a
// pool of numBuffers buffers. It creates the directory if it does not exist.
//
// For a new database, Open creates the catalogs. An existing database is
// recovered, because a crash can have left uncommitted changes on disk. Both
// run in a separate transaction before Open returns, so no other transaction
// sees the database before it is ready.
//
// A failed Open closes every file it opened before it returns, because it
// returns no database that the caller could close.
func Open(dir string, blockSize, numBuffers int) (_ *DB, err error) {
	fm, err := filemanager.NewFileManager(dir, blockSize)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, fm.Close())
		}
	}()

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
		planner:         plan.NewPlanner(plan.NewHeuristicQueryPlanner(mm), plan.NewIndexUpdatePlanner(mm)),
	}

	tx, err := db.NewTransaction()
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

// NewTransaction starts a transaction on the database. The transaction gets a
// number that no other transaction has received since Open, and uses the lock
// table that all transactions share. NewTransaction is safe for concurrent
// use.
//
// The numbers restart from 1 each time the database is opened. This is safe
// because Open ends recovery with a checkpoint, and recovery never reads the
// log before the last checkpoint. The earlier numbers are therefore never
// read again.
func (db *DB) NewTransaction() (*transaction.Transaction, error) {
	db.mu.Lock()
	db.lastTxNum++
	txNum := db.lastTxNum
	db.mu.Unlock()

	return transaction.NewTransaction(db.fileManager, db.logManager, db.bufferManager, db.lockTable, txNum)
}

// Close closes every file that the database has opened.
//
// It does not end running transactions. Their owners must commit or roll them
// back first. Otherwise the recovery of the next Open undoes their changes.
func (db *DB) Close() error {
	return db.fileManager.Close()
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

// Planner returns the planner that runs SQL text against the database. Every
// caller gets the same planner, which uses the metadata manager of the
// database.
func (db *DB) Planner() *plan.Planner {
	return db.planner
}
