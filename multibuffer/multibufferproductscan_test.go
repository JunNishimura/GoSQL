package multibuffer

import (
	"reflect"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// sevenBlocks is a right side of seven blocks with one record each, ids 1 to
// 7.
//
// The test pool has 8 buffers, and the left side pins one of them, so 7 are
// free when the product is created. Without the reserved buffers, 5 remain. 7
// blocks do not fit in 5 buffers, but half of 7 rounded up, 4, does. The
// product therefore reads the table as one chunk of blocks 0 to 3 and one
// chunk of blocks 4 to 6.
var sevenBlocks = [][]int32{{1}, {2}, {3}, {4}, {5}, {6}, {7}}

// sevenBlocksPairs are the pairs of a left side with lids 10 and 20 and
// sevenBlocks: the first chunk with the whole left side, then the second.
var sevenBlocksPairs = []pair{
	{10, 1}, {10, 2}, {10, 3}, {10, 4},
	{20, 1}, {20, 2}, {20, 3}, {20, 4},
	{10, 5}, {10, 6}, {10, 7},
	{20, 5}, {20, 6}, {20, 7},
}

// newTestMultibufferProductScan writes blocks to the test table and opens the
// product of a left side with lids and that table. The scan is closed when
// the test ends.
func newTestMultibufferProductScan(t *testing.T, tx *transaction.Transaction, lids []int32, blocks [][]int32) *MultibufferProductScan {
	t.Helper()

	layout := newTestLayout(t)
	writeBlocks(t, tx, testFileName, layout, blocks)
	lhs := openLeftScan(t, tx, lids)

	mps, err := NewMultibufferProductScan(tx, lhs, testTableName, layout)
	if err != nil {
		lhs.Close()
		t.Fatalf("NewMultibufferProductScan() error = %v", err)
	}
	t.Cleanup(mps.Close)

	return mps
}

func TestNewMultibufferProductScan(t *testing.T) {
	t.Run("given 7 buffers free and a table of 7 blocks, when the product is made, then a first chunk of 4 blocks is pinned", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)
		writeBlocks(t, tx, testFileName, layout, sevenBlocks)
		lhs := openLeftScan(t, tx, []int32{10, 20})
		before := tx.AvailableBuffers()

		mps, err := NewMultibufferProductScan(tx, lhs, testTableName, layout)
		if err != nil {
			lhs.Close()
			t.Fatalf("NewMultibufferProductScan() error = %v", err)
		}
		defer mps.Close()

		if got, want := tx.AvailableBuffers(), before-4; got != want {
			t.Errorf("AvailableBuffers() = %d, want %d", got, want)
		}
	})
}

func TestMultibufferProductScanMoveToNextRecord(t *testing.T) {
	tests := []struct {
		name   string
		lids   []int32
		blocks [][]int32
		want   []pair
	}{
		{
			name:   "given a table that fits in one chunk, when the product is read through, then every record of the left is put beside every record of the table",
			lids:   []int32{10, 20},
			blocks: [][]int32{{1}, {2}},
			want:   []pair{{10, 1}, {10, 2}, {20, 1}, {20, 2}},
		},
		{
			name:   "given a table read in a chunk of 4 blocks and a shorter one of 3, when the product is read through, then each chunk is put beside the whole left before the next",
			lids:   []int32{10, 20},
			blocks: sevenBlocks,
			want:   sevenBlocksPairs,
		},
		{
			name:   "given a left side with no records, when the product is read through, then it gives none",
			lids:   nil,
			blocks: sevenBlocks,
			want:   nil,
		},
		{
			name:   "given a table of one block with no records, when the product is read through, then it gives none",
			lids:   []int32{10, 20},
			blocks: [][]int32{{}},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mps := newTestMultibufferProductScan(t, tx, tt.lids, tt.blocks)

			if got := readPairs(t, mps); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("pairs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMultibufferProductScanMoveBeforeFirstRecord(t *testing.T) {
	t.Run("given a product read into its second chunk, when it is moved before the first record, then it gives every pair again from the first chunk", func(t *testing.T) {
		tx := newTestTransaction(t)
		mps := newTestMultibufferProductScan(t, tx, []int32{10, 20}, sevenBlocks)
		for range 10 {
			if _, err := mps.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}
		}

		if err := mps.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := readPairs(t, mps); !reflect.DeepEqual(got, sevenBlocksPairs) {
			t.Errorf("pairs = %v, want %v", got, sevenBlocksPairs)
		}
	})
}

func TestMultibufferProductScanClose(t *testing.T) {
	t.Run("given a product on its first chunk, when it is closed, then the buffers of the left side and of the chunk are all available again", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)
		writeBlocks(t, tx, testFileName, layout, sevenBlocks)
		lhs := openLeftScan(t, tx, []int32{10, 20})
		mps, err := NewMultibufferProductScan(tx, lhs, testTableName, layout)
		if err != nil {
			lhs.Close()
			t.Fatalf("NewMultibufferProductScan() error = %v", err)
		}

		mps.Close()

		if got := tx.AvailableBuffers(); got != testNumBuffers {
			t.Errorf("AvailableBuffers() = %d, want %d", got, testNumBuffers)
		}
	})
}

func TestMultibufferProductScanGetInt(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      int32
	}{
		{
			name:      "given a product on its first pair, when a field of the left side is read, then it is the left record's",
			fieldName: "lid",
			want:      10,
		},
		{
			name:      "given a product on its first pair, when a field of the table is read, then it is the table record's",
			fieldName: "id",
			want:      1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mps := newTestMultibufferProductScan(t, tx, []int32{10, 20}, sevenBlocks)
			if _, err := mps.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}

			got, err := mps.GetInt(tt.fieldName)
			if err != nil {
				t.Fatalf("GetInt() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("GetInt(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMultibufferProductScanGetString(t *testing.T) {
	t.Run("given a product on its first pair, when a varchar field of the table is read, then it is the table record's", func(t *testing.T) {
		tx := newTestTransaction(t)
		mps := newTestMultibufferProductScan(t, tx, []int32{10, 20}, sevenBlocks)
		if _, err := mps.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}

		got, err := mps.GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}
		if want := nameOf(1); got != want {
			t.Errorf("GetString() = %q, want %q", got, want)
		}
	})
}

func TestMultibufferProductScanGetValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      query.Constant
	}{
		{
			name:      "given a product on its first pair, when a field of the left side is read as a constant, then it is the left record's",
			fieldName: "lid",
			want:      query.NewIntConstant(10),
		},
		{
			name:      "given a product on its first pair, when a varchar field of the table is read as a constant, then it is the table record's",
			fieldName: "name",
			want:      query.NewStringConstant(nameOf(1)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mps := newTestMultibufferProductScan(t, tx, []int32{10, 20}, sevenBlocks)
			if _, err := mps.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}

			got, err := mps.GetValue(tt.fieldName)
			if err != nil {
				t.Fatalf("GetValue() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetValue(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMultibufferProductScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a product of a left side with lid and a table with id, when asked for lid, then it has it",
			fieldName: "lid",
			want:      true,
		},
		{
			name:      "given a product of a left side with lid and a table with id, when asked for id, then it has it",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a product of a left side with lid and a table with id, when asked for age, which neither has, then it does not have it",
			fieldName: "age",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mps := newTestMultibufferProductScan(t, tx, []int32{10, 20}, sevenBlocks)

			if got := mps.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}
