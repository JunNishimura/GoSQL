// Package server puts the layers of the database together into one that can be
// opened on a directory.
package server

import (
	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
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
}

// Open opens the database kept in dir, creating the directory if there is none,
// with blocks of blockSize bytes and a pool of numBuffers buffers.
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

	return &DB{
		fileManager:     fm,
		logManager:      lm,
		bufferManager:   bm,
		lockTable:       concurrencymanager.NewLockTable(),
		metadataManager: mm,
	}, nil
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
