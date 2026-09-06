package buffermanager

import "testing"

// assertChosen checks that a strategy returned the expected buffer, where an
// index of -1 means that no buffer was expected at all.
func assertChosen(t *testing.T, got *Buffer, pool []*Buffer, wantIndex int) {
	t.Helper()

	if wantIndex == -1 {
		if got != nil {
			t.Errorf("chooseUnpinnedBuffer() = %v, want nil", got)
		}
		return
	}
	if got != pool[wantIndex] {
		t.Errorf("chooseUnpinnedBuffer() = %v, want pool[%d]", got, wantIndex)
	}
}

func TestNaiveStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name      string
		pins      []int
		wantIndex int
	}{
		{
			name:      "given no buffer pinned, it chooses the first of the pool",
			pins:      []int{0, 0, 0},
			wantIndex: 0,
		},
		{
			name:      "given the first of the pool pinned, it passes over it and chooses the next free one",
			pins:      []int{2, 0, 0},
			wantIndex: 1,
		},
		{
			name:      "given only the last buffer free, it is chosen rather than passed over",
			pins:      []int{1, 1, 0},
			wantIndex: 2,
		},
		{
			name:      "given every buffer pinned, it chooses none",
			pins:      []int{1, 1, 1},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{pins: tt.pins[i]}
			}

			got := (&naiveStrategy{newScanStrategy(pool)}).chooseUnpinnedBuffer()

			assertChosen(t, got, pool, tt.wantIndex)
		})
	}
}

func TestFifoStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name      string
		pins      []int
		readTimes []int
		wantIndex int
	}{
		{
			name:      "given buffers read in out of pool order, it chooses the one read in first rather than the first of the pool",
			pins:      []int{0, 0, 0},
			readTimes: []int{3, 1, 2},
			wantIndex: 1,
		},
		{
			name:      "given the buffer read in first is pinned, it passes over it and chooses the next oldest",
			pins:      []int{1, 0, 0},
			readTimes: []int{1, 3, 2},
			wantIndex: 2,
		},
		{
			name:      "given a buffer that has never held a block, it is chosen before any that has",
			pins:      []int{0, 0, 0},
			readTimes: []int{2, 0, 1},
			wantIndex: 1,
		},
		{
			name:      "given every buffer pinned, it chooses none",
			pins:      []int{1, 1, 1},
			readTimes: []int{1, 2, 3},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{pins: tt.pins[i], readTime: tt.readTimes[i]}
			}

			got := (&fifoStrategy{newScanStrategy(pool)}).chooseUnpinnedBuffer()

			assertChosen(t, got, pool, tt.wantIndex)
		})
	}
}

// pinChange reports that the buffer at the given pool index became pinned or
// unpinned, mirroring what the manager tells a strategy.
type pinChange struct {
	index  int
	pinned bool
}

func pinnedAt(index int) pinChange {
	return pinChange{index: index, pinned: true}
}

func unpinnedAt(index int) pinChange {
	return pinChange{index: index}
}

func TestLruStrategyChooseUnpinnedBuffer(t *testing.T) {
	const poolSize = 3

	tests := []struct {
		name      string
		changes   []pinChange
		wantIndex int
	}{
		{
			name:      "given no buffer has been used yet, it chooses the first of the pool",
			changes:   nil,
			wantIndex: 0,
		},
		{
			name: "given buffers unpinned out of pool order, it chooses the one unpinned first",
			changes: []pinChange{
				pinnedAt(0), pinnedAt(1), pinnedAt(2),
				unpinnedAt(1), unpinnedAt(2), unpinnedAt(0),
			},
			wantIndex: 1,
		},
		{
			name: "given only one buffer has been used and freed, it is chosen",
			changes: []pinChange{
				pinnedAt(0), pinnedAt(1), pinnedAt(2),
				unpinnedAt(2),
			},
			wantIndex: 2,
		},
		{
			name: "when a buffer is used again and freed, then it goes to the back and another is chosen first",
			changes: []pinChange{
				pinnedAt(0), pinnedAt(1), pinnedAt(2),
				unpinnedAt(1), unpinnedAt(2), unpinnedAt(0),
				pinnedAt(1), unpinnedAt(1),
			},
			wantIndex: 2,
		},
		{
			name: "given every buffer pinned, it chooses none",
			changes: []pinChange{
				pinnedAt(0), pinnedAt(1), pinnedAt(2),
			},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, poolSize)
			for i := range pool {
				pool[i] = &Buffer{}
			}

			strategy := newLRUStrategy(pool)
			for _, change := range tt.changes {
				buf := pool[change.index]
				if change.pinned {
					buf.pin()
					strategy.bufferPinned(buf)
					continue
				}
				buf.unpin()
				strategy.bufferUnpinned(buf)
			}

			assertChosen(t, strategy.chooseUnpinnedBuffer(), pool, tt.wantIndex)
		})
	}
}

func TestClockStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name string
		pins []int
		// wantIndexes holds the buffer expected from each successive call, so
		// that the movement of the clock hand can be observed. -1 means nil.
		wantIndexes []int
	}{
		{
			name:        "given a buffer was chosen last time, the scan carries on from the one after it",
			pins:        []int{0, 0, 0},
			wantIndexes: []int{0, 1, 2},
		},
		{
			name:        "given the scan reached the end of the pool, it wraps round to the first buffer",
			pins:        []int{0, 0, 0},
			wantIndexes: []int{0, 1, 2, 0},
		},
		{
			name:        "given a pinned buffer in the way, the scan passes over it and carries on",
			pins:        []int{0, 1, 0},
			wantIndexes: []int{0, 2},
		},
		{
			name:        "given the rest of the pool is pinned, the scan wraps round and finds the free buffer behind it",
			pins:        []int{0, 0, 1},
			wantIndexes: []int{0, 1, 0},
		},
		{
			name:        "given every buffer pinned, it chooses none",
			pins:        []int{1, 1, 1},
			wantIndexes: []int{-1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{pins: tt.pins[i]}
			}

			strategy := &clockStrategy{scanStrategy: newScanStrategy(pool)}
			for call, want := range tt.wantIndexes {
				got := strategy.chooseUnpinnedBuffer()

				if want == -1 {
					if got != nil {
						t.Errorf("call %d: chooseUnpinnedBuffer() = %v, want nil", call+1, got)
					}
					continue
				}
				if got != pool[want] {
					t.Errorf("call %d: chooseUnpinnedBuffer() = %v, want pool[%d]", call+1, got, want)
				}
			}
		})
	}
}

func TestUnmodifiedFirstStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name string
		pins []int
		// txNums holds the transaction that modified each buffer, where -1
		// marks a buffer that needs no write-back.
		txNums    []int
		wantIndex int
	}{
		{
			name:      "given a modified buffer before an unmodified one, it chooses the unmodified one, which needs no write",
			pins:      []int{0, 0, 0},
			txNums:    []int{5, -1, -1},
			wantIndex: 1,
		},
		{
			name:      "given every free buffer is modified, it takes the first of them rather than choosing none",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 2, 3},
			wantIndex: 0,
		},
		{
			name:      "given the unmodified buffer is pinned, it is passed over despite needing no write",
			pins:      []int{1, 0, 0},
			txNums:    []int{-1, 5, -1},
			wantIndex: 2,
		},
		{
			name:      "given the only unmodified buffer is pinned, it takes a modified one rather than choosing none",
			pins:      []int{1, 0, 0},
			txNums:    []int{-1, 5, 6},
			wantIndex: 1,
		},
		{
			name:      "given every buffer pinned, it chooses none, even though none would need a write",
			pins:      []int{1, 1, 1},
			txNums:    []int{-1, -1, -1},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{pins: tt.pins[i], txNum: tt.txNums[i]}
			}

			got := (&unmodifiedFirstStrategy{naiveStrategy{newScanStrategy(pool)}}).chooseUnpinnedBuffer()

			assertChosen(t, got, pool, tt.wantIndex)
		})
	}
}

func TestLeastRecentlyModifiedStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name string
		pins []int
		// txNums marks each buffer as modified, where -1 means unmodified, and
		// lsns holds the log record protecting it, where -1 means none.
		txNums    []int
		lsns      []int
		wantIndex int
	}{
		{
			name:      "given several modified buffers, it chooses the one whose changes were logged earliest",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{30, 10, 20},
			wantIndex: 1,
		},
		{
			name:      "given an unmodified buffer with the lowest lsn, it is passed over in favour of a modified one",
			pins:      []int{0, 0, 0},
			txNums:    []int{-1, 1, 1},
			lsns:      []int{5, 30, 20},
			wantIndex: 2,
		},
		{
			name:      "given a modified buffer whose changes were never logged, it is chosen before any that were",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{10, -1, 5},
			wantIndex: 1,
		},
		{
			name:      "given a pinned buffer with the lowest lsn, it is passed over",
			pins:      []int{1, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{5, 30, 20},
			wantIndex: 2,
		},
		{
			name:      "given no free buffer is modified, it takes the first of them rather than choosing none",
			pins:      []int{0, 0, 0},
			txNums:    []int{-1, -1, -1},
			lsns:      []int{30, 10, 20},
			wantIndex: 0,
		},
		{
			name:      "given every buffer pinned, it chooses none",
			pins:      []int{1, 1, 1},
			txNums:    []int{1, 1, 1},
			lsns:      []int{10, 20, 30},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{
					pins:  tt.pins[i],
					txNum: tt.txNums[i],
					lsn:   tt.lsns[i],
				}
			}

			got := (&leastRecentlyModifiedStrategy{naiveStrategy{newScanStrategy(pool)}}).chooseUnpinnedBuffer()

			assertChosen(t, got, pool, tt.wantIndex)
		})
	}
}
