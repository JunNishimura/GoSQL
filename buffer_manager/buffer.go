package buffermanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

type Buffer struct {
	fileManager *filemanager.FileManager
	logManager  *logmanager.LogManager
	contents    *filemanager.Page
	pins        int
	txNum       int
	lsn         int
}

func NewBuffer(fm *filemanager.FileManager, lm *logmanager.LogManager) *Buffer {
	return &Buffer{
		fileManager: fm,
		logManager:  lm,
		contents:    filemanager.NewPageByBlockSize(fm.BlockSize()),
		txNum:       -1,
		lsn:         -1,
	}
}

func (b *Buffer) SetModified(txNum, lsn int) {
	b.txNum = txNum
	if lsn >= 0 {
		b.lsn = lsn
	}
}

func (b *Buffer) pin() {
	b.pins++
}

func (b *Buffer) unpin() {
	b.pins--
}

func (b *Buffer) isPinned() bool {
	return b.pins > 0
}
