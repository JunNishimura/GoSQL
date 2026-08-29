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

func (lm *LogManager) Flush(lsn int) error {
	if lsn > lm.lastSavedLSN {
		return lm.flush()
	}
	return nil
}

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

// Archive moves the log to destPath and starts an empty one in its place. It is
// meant for after recovery, when nothing in the log is needed any more except
// to look back at.
//
// Whatever is still only in the page is written out first, so that the archived
// file holds every record and can be read as a log on its own.
//
// The LSN carries on rather than starting over. Buffers still hold the numbers
// they were given, and those records are on disk in the archived file, so
// nothing is waiting to be written: starting over would make every one of them
// look newer than the log and force a pointless flush.
//
// The manager itself is reused, which is what keeps the buffer pool working:
// every buffer holds the manager it was built with, so a replacement would
// leave them writing to the log that was archived.
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

func (lm *LogManager) Iterator() (*LogIterator, error) {
	if err := lm.flush(); err != nil {
		return nil, err
	}
	return NewLogIterator(lm.fileManager, lm.currentBlock)
}
