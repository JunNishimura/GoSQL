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

// Stats reports how the buffer pool has been used. Hits counts the pins that
// were served without a disk read, so comparing it against Pins shows how well
// the pool absorbs requests, while Waits shows how often the pool ran out.
type Stats struct {
	pins    int
	hits    int
	waits   int
	flushes int
}

func (s Stats) Pins() int {
	return s.pins
}

func (s Stats) Hits() int {
	return s.hits
}

func (s Stats) Waits() int {
	return s.waits
}

func (s Stats) Flushes() int {
	return s.flushes
}

type BufferManager struct {
	bufferPool   []*Buffer
	numAvailable int
	maxWaitTime  time.Duration
	strategy     replacementStrategy
	tick         int
	stats        Stats
	mu           sync.Mutex
	cond         *sync.Cond
}

func (bm *BufferManager) GetStats() Stats {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	return bm.stats
}

// nextTick returns a monotonically increasing timestamp shared by readTime and
// unpinTime, so that the two can be compared against each other.
func (bm *BufferManager) nextTick() int {
	bm.tick++
	return bm.tick
}

func NewBufferManager(fm *filemanager.FileManager, lm *logmanager.LogManager, numBuffers int) (*BufferManager, error) {
	return NewBufferManagerWithPolicy(fm, lm, numBuffers, NaivePolicy)
}

func NewBufferManagerWithPolicy(fm *filemanager.FileManager, lm *logmanager.LogManager, numBuffers int, policy ReplacementPolicy) (*BufferManager, error) {
	if numBuffers <= 0 {
		return nil, fmt.Errorf("numBuffers must be positive: got %d", numBuffers)
	}

	strategy, err := newReplacementStrategy(policy)
	if err != nil {
		return nil, err
	}

	bufferPool := make([]*Buffer, numBuffers)
	for i := range bufferPool {
		bufferPool[i] = NewBuffer(fm, lm)
	}

	bm := &BufferManager{
		bufferPool:   bufferPool,
		numAvailable: numBuffers,
		maxWaitTime:  defaultMaxWaitTime,
		strategy:     strategy,
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

	// waited keeps a pin that goes around the loop several times from being
	// counted as more than one wait.
	waited := false
	for {
		buf, err := bm.tryToPin(blk)
		if err != nil {
			return nil, err
		}
		if buf != nil {
			bm.stats.pins++
			return buf, nil
		}
		if timedOut {
			return nil, fmt.Errorf("pin block %s: %w", blk, ErrBufferAbort)
		}
		if !waited {
			waited = true
			bm.stats.waits++
		}
		bm.cond.Wait()
	}
}

func (bm *BufferManager) Unpin(buf *Buffer) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	buf.unpin()
	if !buf.isPinned() {
		buf.unpinTime = bm.nextTick()
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
			if buf.isModified() {
				bm.stats.flushes++
			}
			if err := buf.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (bm *BufferManager) tryToPin(blk *filemanager.BlockId) (*Buffer, error) {
	buf := bm.findExistingBuffer(blk)
	if buf != nil {
		bm.stats.hits++
	} else {
		buf = bm.chooseUnpinnedBuffer()
		if buf == nil {
			return nil, nil
		}
		if buf.isModified() {
			bm.stats.flushes++
		}
		if err := buf.assignToBlock(blk); err != nil {
			return nil, err
		}
		buf.readTime = bm.nextTick()
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
