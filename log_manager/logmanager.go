package logmanager

import (
	"sync"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

type LogManager struct {
	fileManager  *filemanager.FileManager
	logFile      string
	logPage      *filemanager.Page
	currentBlock *filemanager.BlockId
	latestLSN    int
	lastSavedLSN int
	mu           sync.Mutex
}

func NewLogManager(fm *filemanager.FileManager, logFile string) (*LogManager, error) {
	lm := &LogManager{
		fileManager: fm,
		logFile:     logFile,
		logPage:     filemanager.NewPageByBlockSize(fm.BlockSize()),
	}

	logSize, err := fm.Length(logFile)
	if err != nil {
		return nil, err
	}

	var blk *filemanager.BlockId
	if logSize == 0 {
		blk, err = lm.appendNewBlock()
	} else {
		blk = filemanager.NewBlockId(logFile, logSize-1)
		err = fm.Read(blk, lm.logPage)
	}
	if err != nil {
		return nil, err
	}

	lm.currentBlock = blk
	return lm, nil
}

func (lm *LogManager) appendNewBlock() (*filemanager.BlockId, error) {
	blk, err := lm.fileManager.Append(lm.logFile)
	if err != nil {
		return nil, err
	}

	if err := lm.logPage.SetInt(0, int32(lm.fileManager.BlockSize())); err != nil {
		return nil, err
	}

	if err := lm.fileManager.Write(blk, lm.logPage); err != nil {
		return nil, err
	}

	return blk, nil
}

// Flush writes the log to disk up to and including the record with the given
// LSN. A buffer calls it before writing its block, so that the log record of
// a change is durable before the change itself.
//
// The log file is synced after the write. A write alone only reaches the OS
// page cache, and the OS could then write the buffer's block to disk first.
func (lm *LogManager) Flush(lsn int) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lsn <= lm.lastSavedLSN {
		return nil
	}
	if err := lm.flush(); err != nil {
		return err
	}
	return lm.fileManager.Sync(lm.logFile)
}

// flush writes the page to the file unconditionally, without a sync. The
// caller must hold mu, because flush accesses the page and the saved LSN, and
// Append modifies both.
func (lm *LogManager) flush() error {
	if err := lm.fileManager.Write(lm.currentBlock, lm.logPage); err != nil {
		return err
	}
	lm.lastSavedLSN = lm.latestLSN
	return nil
}

func (lm *LogManager) Append(logRecord []byte) (int, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	boundary := int(lm.logPage.GetInt(0))
	recordSize := len(logRecord)
	bytesNeeded := recordSize + filemanager.IntBytes

	if boundary-bytesNeeded < filemanager.IntBytes {
		if err := lm.flush(); err != nil {
			return 0, err
		}
		blk, err := lm.appendNewBlock()
		if err != nil {
			return 0, err
		}
		lm.currentBlock = blk
		boundary = int(lm.logPage.GetInt(0))
	}

	recordPosition := boundary - bytesNeeded
	if err := lm.logPage.SetBytes(recordPosition, logRecord); err != nil {
		return 0, err
	}
	if err := lm.logPage.SetInt(0, int32(recordPosition)); err != nil {
		return 0, err
	}

	lm.latestLSN++
	return lm.latestLSN, nil
}

// Archive moves the log to destPath and starts an empty log in its place. It
// is meant to run after recovery, when the log is only needed for reference.
//
// Records that are still only in the page are written first, so that the
// archived file holds every record and can be read as a complete log.
//
// The LSN continues from its current value and does not restart. Buffers
// still hold the LSNs they were given, and those records are on disk in the
// archived file. If the LSN restarted, every such buffer would appear newer
// than the log and would force an unnecessary flush.
//
// The manager itself is reused, which keeps the buffer pool working. Every
// buffer holds the manager it was created with, so with a new manager the
// buffers would still write to the archived log.
func (lm *LogManager) Archive(destPath string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if err := lm.flush(); err != nil {
		return err
	}

	if err := lm.fileManager.Archive(lm.logFile, destPath); err != nil {
		return err
	}

	lm.logPage = filemanager.NewPageByBlockSize(lm.fileManager.BlockSize())
	blk, err := lm.appendNewBlock()
	if err != nil {
		return err
	}
	lm.currentBlock = blk
	lm.lastSavedLSN = lm.latestLSN

	return nil
}

// Iterator returns an iterator that reads the log backwards, newest record
// first. The page is written to the file first, because the iterator reads
// through the file manager and would otherwise miss the records in memory.
func (lm *LogManager) Iterator() (*LogIterator, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if err := lm.flush(); err != nil {
		return nil, err
	}
	return NewLogIterator(lm.fileManager, lm.currentBlock)
}
