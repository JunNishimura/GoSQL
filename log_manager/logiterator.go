package logmanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

type LogIterator struct {
	fileManager *filemanager.FileManager
	blockId     *filemanager.BlockId
	page        *filemanager.Page
	currentPos  int
	boundary    int
}

func NewLogIterator(fm *filemanager.FileManager, blk *filemanager.BlockId) (*LogIterator, error) {
	it := &LogIterator{
		fileManager: fm,
		page:        filemanager.NewPageByBytes(make([]byte, fm.BlockSize())),
	}

	if err := it.moveToBlock(blk); err != nil {
		return nil, err
	}

	return it, nil
}

func (it *LogIterator) moveToBlock(blk *filemanager.BlockId) error {
	if err := it.fileManager.Read(blk, it.page); err != nil {
		return err
	}

	it.blockId = blk
	it.boundary = int(it.page.GetInt(0))
	it.currentPos = it.boundary

	return nil
}

func (it *LogIterator) HasNext() bool {
	return it.currentPos < it.fileManager.BlockSize() || it.blockId.Number() > 0
}

func (it *LogIterator) Next() ([]byte, error) {
	if it.currentPos == it.fileManager.BlockSize() {
		blk := filemanager.NewBlockId(it.blockId.FileName(), it.blockId.Number()-1)
		if err := it.moveToBlock(blk); err != nil {
			return nil, err
		}
	}

	record := it.page.GetBytes(it.currentPos)
	it.currentPos += filemanager.IntBytes + len(record)

	return record, nil
}
