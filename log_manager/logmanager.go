package logmanager

import (
	"github.com/JunNishimura/GoSQL/file_manager"
)

type LogManager struct {
	fileManager  *filemanager.FileManager
	logFile      string
	logPage      *filemanager.Page
	currentBlock *filemanager.BlockId
	latestLSN    int
	lastSavedLSN int
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
