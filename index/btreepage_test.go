package index

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const testBTreeFile = "test_btree"

func TestAppendBTreeBlock(t *testing.T) {
	t.Run("given an empty file, when a block is appended with flag 3, then it is block 0 and a page on it reads flag 3 and no records", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)

		blk, err := appendBTreeBlock(tx, testBTreeFile, layout, 3)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}

		if want := filemanager.NewBlockId(testBTreeFile, 0); blk == nil || !blk.Equals(want) {
			t.Fatalf("appendBTreeBlock() = %v, want %v", blk, want)
		}
		assertHeader(t, tx, blk, layout, 3, 0)
	})

	t.Run("given a file of one block flagged 3, when another is appended with flag 5, then it is block 1 flagged 5 and the first keeps flag 3", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)

		first, err := appendBTreeBlock(tx, testBTreeFile, layout, 3)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}
		second, err := appendBTreeBlock(tx, testBTreeFile, layout, 5)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}

		if want := filemanager.NewBlockId(testBTreeFile, 1); second == nil || !second.Equals(want) {
			t.Fatalf("appendBTreeBlock() = %v, want %v", second, want)
		}
		assertHeader(t, tx, first, layout, 3, 0)
		assertHeader(t, tx, second, layout, 5, 0)
	})
}

func TestBTreePageSetFlag(t *testing.T) {
	t.Run("given a page whose block was appended with flag 3, when its flag is set to 5, then it reads 5 and still no records", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)

		blk, err := appendBTreeBlock(tx, testBTreeFile, layout, 3)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}

		page, err := newBTreePage(tx, blk, layout)
		if err != nil {
			t.Fatalf("newBTreePage() error = %v", err)
		}
		if err := page.setFlag(5); err != nil {
			t.Fatalf("setFlag(5) error = %v", err)
		}
		page.close()

		assertHeader(t, tx, blk, layout, 5, 0)
	})
}

// assertHeader opens a page on blk afresh and checks the flag and the number of
// records its header holds.
func assertHeader(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	wantFlag int32,
	wantNumRecords int,
) {
	t.Helper()

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage(%v) error = %v", blk, err)
	}
	defer page.close()

	flag, err := page.flag()
	if err != nil {
		t.Fatalf("flag() error = %v", err)
	}
	if flag != wantFlag {
		t.Errorf("flag() of %v = %d, want %d", blk, flag, wantFlag)
	}

	numRecords, err := page.numRecords()
	if err != nil {
		t.Fatalf("numRecords() error = %v", err)
	}
	if numRecords != wantNumRecords {
		t.Errorf("numRecords() of %v = %d, want %d", blk, numRecords, wantNumRecords)
	}
}
