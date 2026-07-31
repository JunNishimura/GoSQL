package buffermanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

type BufferManager struct {
	bufferPool   []*Buffer
	numAvailable int
}

func NewBufferManager(fm *filemanager.FileManager, lm *logmanager.LogManager, numBuffers int) (*BufferManager, error) {
	if numBuffers <= 0 {
		return nil, fmt.Errorf("numBuffers must be positive: got %d", numBuffers)
	}

	bufferPool := make([]*Buffer, numBuffers)
	for i := range bufferPool {
		bufferPool[i] = NewBuffer(fm, lm)
	}

	return &BufferManager{
		bufferPool:   bufferPool,
		numAvailable: numBuffers,
	}, nil
}

func (bm *BufferManager) findExistingBuffer(blk *filemanager.BlockId) *Buffer {
	for _, buf := range bm.bufferPool {
		if buf.blk != nil && buf.blk.Equals(blk) {
			return buf
		}
	}
	return nil
}

func (bm *BufferManager) tryToPin(blk *filemanager.BlockId) (*Buffer, error) {
	buf := bm.findExistingBuffer(blk)
	if buf == nil {
		buf = bm.chooseUnpinnedBuffer()
		if buf == nil {
			return nil, nil
		}
		if err := buf.assignToBlock(blk); err != nil {
			return nil, err
		}
	}

	if !buf.isPinned() {
		bm.numAvailable--
	}
	buf.pin()

	return buf, nil
}

func (bm *BufferManager) chooseUnpinnedBuffer() *Buffer {
	for _, buf := range bm.bufferPool {
		if !buf.isPinned() {
			return buf
		}
	}
	return nil
}
