package multibuffer

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// fourBlocks is a file of four blocks of two records each, the ids counting up
// from block 0, so that which blocks a chunk read shows in the ids it gives.
var fourBlocks = [][]int32{{1, 2}, {3, 4}, {5, 6}, {7, 8}}

// newTestChunkScan writes blocks to the test file and opens a chunk scan of
// startBlock to endBlock of it, closed when the test ends.
func newTestChunkScan(t *testing.T, tx *transaction.Transaction, blocks [][]int32, startBlock, endBlock int) *ChunkScan {
	t.Helper()

	layout := newTestLayout(t)
	writeBlocks(t, tx, testFileName, layout, blocks)

	cs, err := NewChunkScan(tx, testFileName, layout, startBlock, endBlock)
	if err != nil {
		t.Fatalf("NewChunkScan() error = %v", err)
	}
	t.Cleanup(cs.Close)

	return cs
}

func TestNewChunkScan(t *testing.T) {
	t.Run("given a file of 4 blocks, when a chunk of blocks 1 to 2 is made, then 2 more buffers are pinned", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)
		writeBlocks(t, tx, testFileName, layout, fourBlocks)
		before := tx.AvailableBuffers()

		cs, err := NewChunkScan(tx, testFileName, layout, 1, 2)
		if err != nil {
			t.Fatalf("NewChunkScan() error = %v", err)
		}
		defer cs.Close()

		if got, want := tx.AvailableBuffers(), before-2; got != want {
			t.Errorf("AvailableBuffers() = %d, want %d", got, want)
		}
	})

	t.Run("given a file of 4 blocks, when a chunk whose last block comes before its first is made, then it is refused", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)
		writeBlocks(t, tx, testFileName, layout, fourBlocks)

		cs, err := NewChunkScan(tx, testFileName, layout, 2, 1)
		if err == nil {
			cs.Close()
			t.Fatal("NewChunkScan() error = nil, want error")
		}
	})
}

func TestChunkScanMoveToNextRecord(t *testing.T) {
	tests := []struct {
		name       string
		blocks     [][]int32
		startBlock int
		endBlock   int
		want       []int32
	}{
		{
			name:       "given a file of 4 blocks, when a chunk of blocks 1 to 2 is read through, then it gives the records of those two blocks in order and none of the others",
			blocks:     fourBlocks,
			startBlock: 1,
			endBlock:   2,
			want:       []int32{3, 4, 5, 6},
		},
		{
			name:       "given a chunk with a block of no records in the middle, when it is read through, then that block is passed over",
			blocks:     [][]int32{{1}, {}, {3}},
			startBlock: 0,
			endBlock:   2,
			want:       []int32{1, 3},
		},
		{
			name:       "given a chunk of one block with no records, when it is read through, then it gives none",
			blocks:     [][]int32{{}},
			startBlock: 0,
			endBlock:   0,
			want:       nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			cs := newTestChunkScan(t, tx, tt.blocks, tt.startBlock, tt.endBlock)

			if got := readIDs(t, cs); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ids = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChunkScanMoveBeforeFirstRecord(t *testing.T) {
	t.Run("given a chunk of blocks 1 to 2 read to the end, when it is moved before the first record, then it gives the same records again", func(t *testing.T) {
		tx := newTestTransaction(t)
		cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)
		readIDs(t, cs)

		if err := cs.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got, want := readIDs(t, cs), []int32{3, 4, 5, 6}; !reflect.DeepEqual(got, want) {
			t.Errorf("ids = %v, want %v", got, want)
		}
	})
}

func TestChunkScanClose(t *testing.T) {
	t.Run("given a chunk of 2 blocks, when it is closed, then the 2 buffers it pinned are available again", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)
		writeBlocks(t, tx, testFileName, layout, fourBlocks)
		cs, err := NewChunkScan(tx, testFileName, layout, 1, 2)
		if err != nil {
			t.Fatalf("NewChunkScan() error = %v", err)
		}
		open := tx.AvailableBuffers()

		cs.Close()

		if got, want := tx.AvailableBuffers(), open+2; got != want {
			t.Errorf("AvailableBuffers() = %d, want %d", got, want)
		}
	})
}

func TestChunkScanGetInt(t *testing.T) {
	t.Run("given a chunk on its first record, when its int field is read, then it is that record's", func(t *testing.T) {
		tx := newTestTransaction(t)
		cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)
		if _, err := cs.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}

		got, err := cs.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 3 {
			t.Errorf("GetInt() = %d, want 3", got)
		}
	})

	t.Run("given a chunk before its first record, when its int field is read, then it reports ErrNoCurrentRecord", func(t *testing.T) {
		tx := newTestTransaction(t)
		cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)

		if _, err := cs.GetInt("id"); !errors.Is(err, query.ErrNoCurrentRecord) {
			t.Errorf("GetInt() error = %v, want %v", err, query.ErrNoCurrentRecord)
		}
	})
}

func TestChunkScanGetString(t *testing.T) {
	t.Run("given a chunk on its first record, when its varchar field is read, then it is that record's", func(t *testing.T) {
		tx := newTestTransaction(t)
		cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)
		if _, err := cs.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}

		got, err := cs.GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}
		if want := nameOf(3); got != want {
			t.Errorf("GetString() = %q, want %q", got, want)
		}
	})
}

func TestChunkScanGetValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      query.Constant
	}{
		{
			name:      "given a chunk on its first record, when its int field is read as a constant, then it is an int constant of that record's value",
			fieldName: "id",
			want:      query.NewIntConstant(3),
		},
		{
			name:      "given a chunk on its first record, when its varchar field is read as a constant, then it is a string constant of that record's value",
			fieldName: "name",
			want:      query.NewStringConstant(nameOf(3)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)
			if _, err := cs.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}

			got, err := cs.GetValue(tt.fieldName)
			if err != nil {
				t.Fatalf("GetValue() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChunkScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a chunk of records with an id field, when asked for id, then it has it",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a chunk of records with no age field, when asked for age, then it does not have it",
			fieldName: "age",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			cs := newTestChunkScan(t, tx, fourBlocks, 1, 2)

			if got := cs.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}
