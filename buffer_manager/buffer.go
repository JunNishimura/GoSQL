package buffermanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

type Buffer struct {
	fileManager *filemanager.FileManager
	logManager  *logmanager.LogManager
	contents    *filemanager.Page
	blk         *filemanager.BlockId
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

func (b *Buffer) flush() error {
	if b.txNum < 0 {
		return nil
	}

	if err := b.logManager.Flush(b.lsn); err != nil {
		return err
	}
	if err := b.fileManager.Write(b.blk, b.contents); err != nil {
		return err
	}
	b.txNum = -1

	return nil
}

func (b *Buffer) assignToBlock(blk *filemanager.BlockId) error {
	if err := b.flush(); err != nil {
		return err
	}

	b.blk = blk
	if err := b.fileManager.Read(b.blk, b.contents); err != nil {
		return err
	}
	b.pins = 0

	return nil
}
