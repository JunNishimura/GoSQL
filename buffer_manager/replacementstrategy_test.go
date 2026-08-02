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
			name:      "returns the first buffer when none of them is pinned",
			pins:      []int{0, 0, 0},
			wantIndex: 0,
		},
		{
			name:      "skips the pinned head and returns the first unpinned buffer",
			pins:      []int{2, 0, 0},
			wantIndex: 1,
		},
		{
			name:      "returns the last buffer when it is the only unpinned one",
			pins:      []int{1, 1, 0},
			wantIndex: 2,
		},
		{
			name:      "returns nil when every buffer is pinned",
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
			name:      "returns the buffer read in first rather than the first buffer in the pool",
			pins:      []int{0, 0, 0},
			readTimes: []int{3, 1, 2},
			wantIndex: 1,
		},
		{
			name:      "skips the buffer read in first when it is pinned",
			pins:      []int{1, 0, 0},
			readTimes: []int{1, 3, 2},
			wantIndex: 2,
		},
		{
			name:      "returns a buffer that has never been assigned a block before any assigned one",
			pins:      []int{0, 0, 0},
			readTimes: []int{2, 0, 1},
			wantIndex: 1,
		},
		{
			name:      "returns nil when every buffer is pinned",
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

func TestLruStrategyChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name       string
		pins       []int
		readTimes  []int
		unpinTimes []int
		wantIndex  int
	}{
		{
			name:       "returns the buffer unpinned first rather than the first buffer in the pool",
			pins:       []int{0, 0, 0},
			readTimes:  []int{1, 2, 3},
			unpinTimes: []int{6, 4, 5},
			wantIndex:  1,
		},
		{
			name:       "skips the buffer unpinned first when it is pinned",
			pins:       []int{1, 0, 0},
			readTimes:  []int{1, 2, 3},
			unpinTimes: []int{4, 6, 5},
			wantIndex:  2,
		},
		{
			name:       "returns a buffer that has never been unpinned before any unpinned one",
			pins:       []int{0, 0, 0},
			readTimes:  []int{1, 2, 3},
			unpinTimes: []int{5, 0, 4},
			wantIndex:  1,
		},
		{
			name:       "follows unpinTime when it disagrees with readTime",
			pins:       []int{0, 0, 0},
			readTimes:  []int{1, 2, 3},
			unpinTimes: []int{6, 5, 4},
			wantIndex:  2,
		},
		{
			name:       "returns nil when every buffer is pinned",
			pins:       []int{1, 1, 1},
			readTimes:  []int{1, 2, 3},
			unpinTimes: []int{4, 5, 6},
			wantIndex:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := make([]*Buffer, len(tt.pins))
			for i := range pool {
				pool[i] = &Buffer{
					pins:      tt.pins[i],
					readTime:  tt.readTimes[i],
					unpinTime: tt.unpinTimes[i],
				}
			}

			got := (&lruStrategy{newScanStrategy(pool)}).chooseUnpinnedBuffer()

			assertChosen(t, got, pool, tt.wantIndex)
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
			name:        "resumes the scan at the buffer following the one it returned",
			pins:        []int{0, 0, 0},
			wantIndexes: []int{0, 1, 2},
		},
		{
			name:        "wraps around to the head of the pool after the last buffer",
			pins:        []int{0, 0, 0},
			wantIndexes: []int{0, 1, 2, 0},
		},
		{
			name:        "skips a pinned buffer while scanning forward",
			pins:        []int{0, 1, 0},
			wantIndexes: []int{0, 2},
		},
		{
			name:        "wraps around past the pinned buffers at the end of the pool",
			pins:        []int{0, 0, 1},
			wantIndexes: []int{0, 1, 0},
		},
		{
			name:        "returns nil when every buffer is pinned",
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
			name:      "skips a modified buffer and returns the first unmodified one",
			pins:      []int{0, 0, 0},
			txNums:    []int{5, -1, -1},
			wantIndex: 1,
		},
		{
			name:      "falls back to the first unpinned buffer when every unpinned buffer is modified",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 2, 3},
			wantIndex: 0,
		},
		{
			name:      "skips an unmodified buffer that is pinned",
			pins:      []int{1, 0, 0},
			txNums:    []int{-1, 5, -1},
			wantIndex: 2,
		},
		{
			name:      "falls back to a modified buffer when the only unmodified one is pinned",
			pins:      []int{1, 0, 0},
			txNums:    []int{-1, 5, 6},
			wantIndex: 1,
		},
		{
			name:      "returns nil when every buffer is pinned even though none is modified",
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
			name:      "returns the modified buffer with the lowest LSN",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{30, 10, 20},
			wantIndex: 1,
		},
		{
			name:      "skips an unmodified buffer even when its LSN is the lowest",
			pins:      []int{0, 0, 0},
			txNums:    []int{-1, 1, 1},
			lsns:      []int{5, 30, 20},
			wantIndex: 2,
		},
		{
			name:      "prefers a modified buffer whose changes were never logged",
			pins:      []int{0, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{10, -1, 5},
			wantIndex: 1,
		},
		{
			name:      "skips a pinned buffer even when its LSN is the lowest",
			pins:      []int{1, 0, 0},
			txNums:    []int{1, 1, 1},
			lsns:      []int{5, 30, 20},
			wantIndex: 2,
		},
		{
			name:      "falls back to the first unpinned buffer when none of them is modified",
			pins:      []int{0, 0, 0},
			txNums:    []int{-1, -1, -1},
			lsns:      []int{30, 10, 20},
			wantIndex: 0,
		},
		{
			name:      "returns nil when every buffer is pinned",
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
