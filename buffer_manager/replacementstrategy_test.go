package buffermanager

import "testing"

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

			got := (&fifoStrategy{}).chooseUnpinnedBuffer(pool)

			if tt.wantIndex == -1 {
				if got != nil {
					t.Errorf("chooseUnpinnedBuffer() = %v, want nil", got)
				}
				return
			}
			if got != pool[tt.wantIndex] {
				t.Errorf("chooseUnpinnedBuffer() = %v, want pool[%d]", got, tt.wantIndex)
			}
		})
	}
}
