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
}

func NewBuffer(fm *filemanager.FileManager, lm *logmanager.LogManager) *Buffer {
	return &Buffer{
		fileManager: fm,
		logManager:  lm,
		contents:    filemanager.NewPageByBlockSize(fm.BlockSize()),
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
