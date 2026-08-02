package buffermanager

// replacementStrategy decides which unpinned buffer to reuse when no buffer
// already holds the requested block. It returns nil when every buffer is pinned.
type replacementStrategy interface {
	chooseUnpinnedBuffer(pool []*Buffer) *Buffer
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

// fifoStrategy takes the unpinned buffer whose block was read in the longest
// time ago, regardless of how recently that buffer was used.
type fifoStrategy struct{}

func (s *fifoStrategy) chooseUnpinnedBuffer(pool []*Buffer) *Buffer {
	var chosen *Buffer
	for _, buf := range pool {
		if buf.isPinned() {
			continue
		}
		if chosen == nil || buf.readTime < chosen.readTime {
			chosen = buf
		}
	}
	return chosen
}
