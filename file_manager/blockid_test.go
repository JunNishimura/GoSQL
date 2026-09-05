package filemanager

import "testing"

func TestNewBlockId(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		blkNum   int
	}{
		{
			name:     "it carries the file name and the block number it was made from",
			fileName: "test.db",
			blkNum:   3,
		},
		{
			name:     "given block number zero, it keeps the zero rather than reading it as unset",
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
			name:     "it formats as its file name and block number",
			fileName: "test.db",
			blkNum:   3,
			want:     "[file test.db, block 3]",
		},
		{
			name:     "given block number zero, it shows the zero rather than leaving it out",
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
			name: "given two block ids of the same file, when their block numbers are the same, then they are equal",
			a:    NewBlockId("test.db", 1),
			b:    NewBlockId("test.db", 1),
			want: true,
		},
		{
			name: "given two block ids of the same file, when their block numbers differ, then they are not equal",
			a:    NewBlockId("test.db", 1),
			b:    NewBlockId("test.db", 2),
			want: false,
		},
		{
			name: "given two block ids of the same block number, when their file names differ, then they are not equal",
			a:    NewBlockId("a.db", 1),
			b:    NewBlockId("b.db", 1),
			want: false,
		},
		{
			name: "given two block ids, when both the file name and the block number differ, then they are not equal",
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
			name:     "given two block ids of the same file, when their block numbers are the same, then their hashes are the same",
			a:        NewBlockId("test.db", 1),
			b:        NewBlockId("test.db", 1),
			wantSame: true,
		},
		{
			name:     "given two block ids of the same file, when their block numbers differ, then their hashes differ",
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
