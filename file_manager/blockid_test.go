package filemanager

import "testing"

func TestNewBlockId(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		blkNum   int
	}{
		{
			name:     "creates BlockId with given filename and blkNum",
			fileName: "test.db",
			blkNum:   3,
		},
		{
			name:     "creates BlockId with zero blkNum",
			fileName: "test.db",
			blkNum:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBlockId(tt.fileName, tt.blkNum)
			if b.FileName() != tt.fileName {
				t.Errorf("FileName() = %q, want %q", b.FileName(), tt.fileName)
			}
			if b.Number() != tt.blkNum {
				t.Errorf("Number() = %d, want %d", b.Number(), tt.blkNum)
			}
		})
	}
}

func TestBlockIdString(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		blkNum   int
		want     string
	}{
		{
			name:     "formats filename and blkNum into string",
			fileName: "test.db",
			blkNum:   3,
			want:     "[file test.db, block 3]",
		},
		{
			name:     "formats zero blkNum into string",
			fileName: "test.db",
			blkNum:   0,
			want:     "[file test.db, block 0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBlockId(tt.fileName, tt.blkNum)
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBlockIdEquals(t *testing.T) {
	tests := []struct {
		name string
		a    *BlockId
		b    *BlockId
		want bool
	}{
		{
			name: "returns true when filename and blkNum are both equal",
			a:    NewBlockId("test.db", 1),
			b:    NewBlockId("test.db", 1),
			want: true,
		},
		{
			name: "returns false when blkNum differs",
			a:    NewBlockId("test.db", 1),
			b:    NewBlockId("test.db", 2),
			want: false,
		},
		{
			name: "returns false when filename differs",
			a:    NewBlockId("a.db", 1),
			b:    NewBlockId("b.db", 1),
			want: false,
		},
		{
			name: "returns false when both filename and blkNum differ",
			a:    NewBlockId("a.db", 1),
			b:    NewBlockId("b.db", 2),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equals(tt.b); got != tt.want {
				t.Errorf("Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBlockIdHashCode(t *testing.T) {
	tests := []struct {
		name     string
		a        *BlockId
		b        *BlockId
		wantSame bool
	}{
		{
			name:     "returns same hash when filename and blkNum are both equal",
			a:        NewBlockId("test.db", 1),
			b:        NewBlockId("test.db", 1),
			wantSame: true,
		},
		{
			name:     "returns different hash when blkNum differs",
			a:        NewBlockId("test.db", 1),
			b:        NewBlockId("test.db", 2),
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantSame && tt.a.HashCode() != tt.b.HashCode() {
				t.Errorf("HashCode() = %d, want %d", tt.a.HashCode(), tt.b.HashCode())
			}
			if !tt.wantSame && tt.a.HashCode() == tt.b.HashCode() {
				t.Errorf("HashCode() unexpectedly equal: %d", tt.a.HashCode())
			}
		})
	}
}
