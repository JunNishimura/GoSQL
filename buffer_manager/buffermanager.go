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
