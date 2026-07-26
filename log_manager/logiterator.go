package logmanager

import (
	"github.com/JunNishimura/GoSQL/file_manager"
)

type LogIterator struct {
	fileManager *filemanager.FileManager
	blockId     *filemanager.BlockId
	page        *filemanager.Page
}

func NewLogIterator(fm *filemanager.FileManager, blk *filemanager.BlockId) *LogIterator {
	return &LogIterator{
		fileManager: fm,
		blockId:     blk,
		page:        filemanager.NewPageByBytes(make([]byte, fm.BlockSize())),
	}
}
