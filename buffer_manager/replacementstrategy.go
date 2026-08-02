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
)

// replacementStrategy decides which unpinned buffer to reuse when no buffer
// already holds the requested block. It returns nil when every buffer is pinned.
type replacementStrategy interface {
	chooseUnpinnedBuffer(pool []*Buffer) *Buffer
}

func newReplacementStrategy(policy ReplacementPolicy) (replacementStrategy, error) {
	switch policy {
	case NaivePolicy:
		return &naiveStrategy{}, nil
	case FIFOPolicy:
		return &fifoStrategy{}, nil
	case LRUPolicy:
		return &lruStrategy{}, nil
	case ClockPolicy:
		return &clockStrategy{}, nil
	case UnmodifiedFirstPolicy:
		return &unmodifiedFirstStrategy{}, nil
	default:
		return nil, fmt.Errorf("unknown replacement policy: %d", policy)
	}
}

// naiveStrategy scans the pool from the beginning and takes the first unpinned
// buffer it finds.
type naiveStrategy struct{}

func (s *naiveStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	for _, buf := range pool {
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
type fifoStrategy struct{}

func (s *fifoStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	return oldestUnpinnedBuffer(pool, func(buf *Buffer) int { return buf.readTime })
}

// lruStrategy takes the unpinned buffer that was unpinned the longest time ago,
// on the assumption that it is the least likely to be needed again.
type lruStrategy struct{}

func (s *lruStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	return oldestUnpinnedBuffer(pool, func(buf *Buffer) int { return buf.unpinTime })
}

// clockStrategy scans the pool as if it were a circle, starting at the buffer
// following the one it replaced last.
type clockStrategy struct {
	hand int
}

func (s *clockStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	for i := 0; i < len(pool); i++ {
		index := (s.hand + i) % len(pool)
		if buf := pool[index]; !buf.isPinned() {
			s.hand = (index + 1) % len(pool)
			return buf
		}
	}
	return nil
}

// unmodifiedFirstStrategy takes a buffer that needs no write-back whenever one
// is available, so that replacing it costs a single disk read instead of a read
// plus the write of the page and of the log records protecting it.
type unmodifiedFirstStrategy struct {
	fallback naiveStrategy
}

func (s *unmodifiedFirstStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	for _, buf := range pool {
		if !buf.isPinned() && !buf.isModified() {
			return buf
		}
	}
	return s.fallback.chooseUnpinnedBuffer(pool)
}
