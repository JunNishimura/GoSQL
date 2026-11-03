package multibuffer

import "testing"

func TestBestRoot(t *testing.T) {
	tests := []struct {
		name      string
		available int
		size      int
		want      int
	}{
		{
			name:      "given 3 buffers available, which leaves 1 after the reserve, when 100 blocks are asked for, then it takes 1",
			available: 3,
			size:      100,
			want:      1,
		},
		{
			name:      "given no buffers available, when 100 blocks are asked for, then it still takes 1",
			available: 0,
			size:      100,
			want:      1,
		},
		{
			name:      "given 12 buffers available, which leaves 10 after the reserve, when 10 blocks are asked for, then it takes all 10",
			available: 12,
			size:      10,
			want:      10,
		},
		{
			name:      "given 11 buffers available, which leaves 9 after the reserve, when 10 blocks are asked for, then it takes the square root rounded up, 4",
			available: 11,
			size:      10,
			want:      4,
		},
		{
			name:      "given 10 buffers available, which leaves 8 after the reserve, when 64 blocks are asked for, then it takes the square root, 8, which just fits",
			available: 10,
			size:      64,
			want:      8,
		},
		{
			name:      "given 12 buffers available, which leaves 10 after the reserve, when 1000 blocks are asked for, then it takes the cube root, 10",
			available: 12,
			size:      1000,
			want:      10,
		},
		{
			name:      "given 12 buffers available, which leaves 10 after the reserve, when 1001 blocks are asked for, then it takes the fourth root rounded up, 6, since the cube root rounds up to 11",
			available: 12,
			size:      1001,
			want:      6,
		},
		{
			name:      "given 7 buffers available, which leaves 5 after the reserve, when 125 blocks are asked for, then it takes the cube root, 5, which floating point would round up to 6",
			available: 7,
			size:      125,
			want:      5,
		},
		{
			name:      "given 12 buffers available, when no blocks are asked for, then it takes 1",
			available: 12,
			size:      0,
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BestRoot(tt.available, tt.size); got != tt.want {
				t.Errorf("BestRoot(%d, %d) = %d, want %d", tt.available, tt.size, got, tt.want)
			}
		})
	}
}

func TestBestFactor(t *testing.T) {
	tests := []struct {
		name      string
		available int
		size      int
		want      int
	}{
		{
			name:      "given 3 buffers available, which leaves 1 after the reserve, when 100 blocks are asked for, then it takes 1",
			available: 3,
			size:      100,
			want:      1,
		},
		{
			name:      "given no buffers available, when 100 blocks are asked for, then it still takes 1",
			available: 0,
			size:      100,
			want:      1,
		},
		{
			name:      "given 12 buffers available, which leaves 10 after the reserve, when 10 blocks are asked for, then it takes all 10",
			available: 12,
			size:      10,
			want:      10,
		},
		{
			name:      "given 11 buffers available, which leaves 9 after the reserve, when 10 blocks are asked for, then it takes half, 5",
			available: 11,
			size:      10,
			want:      5,
		},
		{
			name:      "given 7 buffers available, which leaves 5 after the reserve, when 20 blocks are asked for, then it takes a quarter, 5, which just fits",
			available: 7,
			size:      20,
			want:      5,
		},
		{
			name:      "given 6 buffers available, which leaves 4 after the reserve, when 21 blocks are asked for, then it takes a sixth rounded up, 4, since a fifth rounds up to 5",
			available: 6,
			size:      21,
			want:      4,
		},
		{
			name:      "given 12 buffers available, when no blocks are asked for, then it takes 1",
			available: 12,
			size:      0,
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BestFactor(tt.available, tt.size); got != tt.want {
				t.Errorf("BestFactor(%d, %d) = %d, want %d", tt.available, tt.size, got, tt.want)
			}
		})
	}
}
