package buffermanager

import (
	"errors"
	"fmt"
	"sync"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const defaultMaxWaitTime = 10 * time.Second

// ErrBufferAbort is returned when no buffer becomes available within maxWaitTime.
// The caller is expected to abort its transaction and retry.
var ErrBufferAbort = errors.New("no buffer available")

type BufferManager struct {
	bufferPool   []*Buffer
	numAvailable int
	maxWaitTime  time.Duration
	strategy     replacementStrategy
	mu           sync.Mutex
	cond         *sync.Cond
}

func NewBufferManager(fm *filemanager.FileManager, lm *logmanager.LogManager, numBuffers int) (*BufferManager, error) {
	if numBuffers <= 0 {
		return nil, fmt.Errorf("numBuffers must be positive: got %d", numBuffers)
	}

	bufferPool := make([]*Buffer, numBuffers)
	for i := range bufferPool {
		bufferPool[i] = NewBuffer(fm, lm)
	}

	bm := &BufferManager{
		bufferPool:   bufferPool,
		numAvailable: numBuffers,
		maxWaitTime:  defaultMaxWaitTime,
		strategy:     &naiveStrategy{},
	}
	bm.cond = sync.NewCond(&bm.mu)

	return bm, nil
}

func (bm *BufferManager) Pin(blk *filemanager.BlockId) (*Buffer, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// sync.Cond cannot wait with a deadline, so a timer wakes the waiters up once
	// maxWaitTime has passed.
	timedOut := false
	timer := time.AfterFunc(bm.maxWaitTime, func() {
		bm.mu.Lock()
		timedOut = true
		bm.mu.Unlock()
		bm.cond.Broadcast()
	})
	defer timer.Stop()

	for {
		buf, err := bm.tryToPin(blk)
		if err != nil {
			return nil, err
		}
		if buf != nil {
			return buf, nil
		}
		if timedOut {
			return nil, fmt.Errorf("pin block %s: %w", blk, ErrBufferAbort)
		}
		bm.cond.Wait()
	}
}

func (bm *BufferManager) Unpin(buf *Buffer) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	buf.unpin()
	if !buf.isPinned() {
		bm.numAvailable++
		bm.cond.Broadcast()
	}
}

func (bm *BufferManager) findExistingBuffer(blk *filemanager.BlockId) *Buffer {
	for _, buf := range bm.bufferPool {
		if buf.blk != nil && buf.blk.Equals(blk) {
			return buf
		}
	}
	return nil
}

func (bm *BufferManager) FlushAll(txNum int) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, buf := range bm.bufferPool {
		if buf.txNum == txNum {
			if err := buf.flush(); err != nil {
				return err
			}
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
	return bm.strategy.chooseUnpinnedBuffer(bm.bufferPool)
}
