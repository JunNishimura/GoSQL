package buffermanager

import "fmt"

// ReplacementPolicy names the strategy a BufferManager uses to pick the buffer
// to replace.
type ReplacementPolicy int

const (
	NaivePolicy ReplacementPolicy = iota
	FIFOPolicy
	LRUPolicy
	ClockPolicy
	UnmodifiedFirstPolicy
	LeastRecentlyModifiedPolicy
)

// replacementStrategy decides which unpinned buffer to reuse when no buffer
// already holds the requested block. It returns nil when every buffer is pinned.
//
// The manager reports every buffer that becomes pinned or unpinned, so that a
// strategy may keep an index of the candidates instead of walking the pool.
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
		return &lruStrategy{newScanStrategy(pool)}, nil
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

// scanStrategy is the base of the strategies that walk the whole pool on every
// choice. They keep no index, so the pin notifications tell them nothing.
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

// fifoStrategy takes the unpinned buffer whose block was read in the longest
// time ago, regardless of how recently that buffer was used.
type fifoStrategy struct {
	scanStrategy
}

func (s *fifoStrategy) chooseUnpinnedBuffer() *Buffer {
	return oldestUnpinnedBuffer(s.pool, func(buf *Buffer) int { return buf.readTime })
}

// lruStrategy takes the unpinned buffer that was unpinned the longest time ago,
// on the assumption that it is the least likely to be needed again.
type lruStrategy struct {
	scanStrategy
}

func (s *lruStrategy) chooseUnpinnedBuffer() *Buffer {
	return oldestUnpinnedBuffer(s.pool, func(buf *Buffer) int { return buf.unpinTime })
}

// clockStrategy scans the pool as if it were a circle, starting at the buffer
// following the one it replaced last.
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

// unmodifiedFirstStrategy takes a buffer that needs no write-back whenever one
// is available, so that replacing it costs a single disk read instead of a read
// plus the write of the page and of the log records protecting it.
type unmodifiedFirstStrategy struct {
	// naiveStrategy supplies the pool and the choice to fall back on when every
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

// leastRecentlyModifiedStrategy takes the modified buffer with the lowest LSN.
// The log records protecting it are the most likely to be on disk already, so
// writing the buffer out needs no further log flush.
type leastRecentlyModifiedStrategy struct {
	// naiveStrategy supplies the pool and the choice to fall back on when no
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
