package buffermanager

import (
	"container/list"
	"fmt"
)

// ReplacementPolicy selects the strategy that a BufferManager uses to choose
// the buffer to replace.
type ReplacementPolicy int

const (
	NaivePolicy ReplacementPolicy = iota
	FIFOPolicy
	LRUPolicy
	ClockPolicy
	UnmodifiedFirstPolicy
	LeastRecentlyModifiedPolicy
)

// replacementStrategy chooses the unpinned buffer to reuse when no buffer
// holds the requested block. It returns nil when every buffer is pinned.
//
// The manager notifies the strategy of every pin and unpin, so a strategy can
// keep an index of the candidates instead of scanning the pool.
type replacementStrategy interface {
	chooseUnpinnedBuffer() *Buffer
	bufferPinned(buf *Buffer)
	bufferUnpinned(buf *Buffer)
}

func newReplacementStrategy(policy ReplacementPolicy, pool []*Buffer) (replacementStrategy, error) {
	switch policy {
	case NaivePolicy:
		return &naiveStrategy{newScanStrategy(pool)}, nil
	case FIFOPolicy:
		return &fifoStrategy{newScanStrategy(pool)}, nil
	case LRUPolicy:
		return newLRUStrategy(pool), nil
	case ClockPolicy:
		return &clockStrategy{scanStrategy: newScanStrategy(pool)}, nil
	case UnmodifiedFirstPolicy:
		return &unmodifiedFirstStrategy{naiveStrategy{newScanStrategy(pool)}}, nil
	case LeastRecentlyModifiedPolicy:
		return &leastRecentlyModifiedStrategy{naiveStrategy{newScanStrategy(pool)}}, nil
	default:
		return nil, fmt.Errorf("unknown replacement policy: %d", policy)
	}
}

// scanStrategy is the base of the strategies that scan the whole pool for
// every choice. They keep no index, so they ignore the pin notifications.
type scanStrategy struct {
	pool []*Buffer
}

func newScanStrategy(pool []*Buffer) scanStrategy {
	return scanStrategy{pool: pool}
}

func (s *scanStrategy) bufferPinned(buf *Buffer) {}

func (s *scanStrategy) bufferUnpinned(buf *Buffer) {}

// naiveStrategy scans the pool from the beginning and takes the first unpinned
// buffer it finds.
type naiveStrategy struct {
	scanStrategy
}

func (s *naiveStrategy) chooseUnpinnedBuffer() *Buffer {
	for _, buf := range s.pool {
		if !buf.isPinned() {
			return buf
		}
	}
	return nil
}

// oldestUnpinnedBuffer returns the unpinned buffer with the smallest timestamp,
// or nil when every buffer is pinned.
func oldestUnpinnedBuffer(pool []*Buffer, timestamp func(*Buffer) int) *Buffer {
	var chosen *Buffer
	for _, buf := range pool {
		if buf.isPinned() {
			continue
		}
		if chosen == nil || timestamp(buf) < timestamp(chosen) {
			chosen = buf
		}
	}
	return chosen
}

// fifoStrategy chooses the unpinned buffer whose block was read earliest,
// regardless of how recently the buffer was used.
type fifoStrategy struct {
	scanStrategy
}

func (s *fifoStrategy) chooseUnpinnedBuffer() *Buffer {
	return oldestUnpinnedBuffer(s.pool, func(buf *Buffer) int { return buf.readTime })
}

// lruStrategy chooses the unpinned buffer that was unpinned earliest, on the
// assumption that it is the least likely to be needed again. It keeps the
// unpinned buffers in a queue in that order, so a choice needs no scan of the
// pool. element maps a buffer to its queue node, so removing a buffer from
// the queue needs no scan either.
type lruStrategy struct {
	queue   *list.List
	element map[*Buffer]*list.Element
}

func newLRUStrategy(pool []*Buffer) *lruStrategy {
	s := &lruStrategy{
		queue:   list.New(),
		element: make(map[*Buffer]*list.Element, len(pool)),
	}

	// Every buffer starts unpinned, so the queue starts in pool order.
	for _, buf := range pool {
		s.element[buf] = s.queue.PushBack(buf)
	}

	return s
}

func (s *lruStrategy) chooseUnpinnedBuffer() *Buffer {
	oldest := s.queue.Front()
	if oldest == nil {
		return nil
	}
	return oldest.Value.(*Buffer)
}

func (s *lruStrategy) bufferPinned(buf *Buffer) {
	element, queued := s.element[buf]
	if !queued {
		return
	}
	s.queue.Remove(element)
	delete(s.element, buf)
}

func (s *lruStrategy) bufferUnpinned(buf *Buffer) {
	if _, queued := s.element[buf]; queued {
		return
	}
	s.element[buf] = s.queue.PushBack(buf)
}

// clockStrategy scans the pool as a circle. Each scan starts at the buffer
// after the one it replaced last.
type clockStrategy struct {
	scanStrategy
	hand int
}

func (s *clockStrategy) chooseUnpinnedBuffer() *Buffer {
	for i := 0; i < len(s.pool); i++ {
		index := (s.hand + i) % len(s.pool)
		if buf := s.pool[index]; !buf.isPinned() {
			s.hand = (index + 1) % len(s.pool)
			return buf
		}
	}
	return nil
}

// unmodifiedFirstStrategy chooses a buffer that needs no write-back whenever
// one is available. Replacing it then costs one disk read, instead of a read
// plus the writes of the page and of the log records that protect it.
type unmodifiedFirstStrategy struct {
	// naiveStrategy provides the pool, and the fallback choice for when every
	// unpinned buffer has been modified.
	naiveStrategy
}

func (s *unmodifiedFirstStrategy) chooseUnpinnedBuffer() *Buffer {
	for _, buf := range s.pool {
		if !buf.isPinned() && !buf.isModified() {
			return buf
		}
	}
	return s.naiveStrategy.chooseUnpinnedBuffer()
}

// leastRecentlyModifiedStrategy chooses the modified buffer with the lowest
// LSN. The log records that protect it are the most likely to be on disk
// already, so writing the buffer needs no further log flush.
type leastRecentlyModifiedStrategy struct {
	// naiveStrategy provides the pool, and the fallback choice for when no
	// unpinned buffer has been modified.
	naiveStrategy
}

func (s *leastRecentlyModifiedStrategy) chooseUnpinnedBuffer() *Buffer {
	var chosen *Buffer
	for _, buf := range s.pool {
		if buf.isPinned() || !buf.isModified() {
			continue
		}
		if chosen == nil || buf.lsn < chosen.lsn {
			chosen = buf
		}
	}
	if chosen != nil {
		return chosen
	}
	return s.naiveStrategy.chooseUnpinnedBuffer()
}
