package filemanager

import "testing"

func TestNewBlockId(t *testing.T) {
	b := NewBlockId("test.db", 3)
	if b.FileName() != "test.db" {
		t.Errorf("FileName() = %q, want %q", b.FileName(), "test.db")
	}
	if b.Number() != 3 {
		t.Errorf("Number() = %d, want %d", b.Number(), 3)
	}
}

func TestBlockIdString(t *testing.T) {
	b := NewBlockId("test.db", 3)
	want := "[file test.db, block 3]"
	if b.String() != want {
		t.Errorf("String() = %q, want %q", b.String(), want)
	}
}

func TestBlockIdEquals(t *testing.T) {
	tests := []struct {
		name  string
		a     *BlockId
		b     *BlockId
		want  bool
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
	t.Run("returns same hash when filename and blkNum are both equal", func(t *testing.T) {
		a := NewBlockId("test.db", 1)
		b := NewBlockId("test.db", 1)
		if a.HashCode() != b.HashCode() {
			t.Errorf("HashCode() = %d, want %d", a.HashCode(), b.HashCode())
		}
	})

	t.Run("returns different hash when blkNum differs", func(t *testing.T) {
		a := NewBlockId("test.db", 1)
		b := NewBlockId("test.db", 2)
		if a.HashCode() == b.HashCode() {
			t.Errorf("HashCode() unexpectedly equal: %d", a.HashCode())
		}
	})
}
