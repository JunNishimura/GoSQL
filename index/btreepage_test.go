package index

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
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

func TestBTreePageInsertLeaf(t *testing.T) {
	tests := []struct {
		name     string
		layout   func(t *testing.T) *recordmanager.Layout
		existing []entry
		slot     int
		inserted entry
		want     []string
	}{
		{
			name:     "given an empty leaf page, when an entry is inserted at slot 0, then the page holds that one entry",
			layout:   newTestIndexLayout,
			slot:     0,
			inserted: entry{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(1, 2)},
			want: []string{
				"10 " + recordmanager.NewRecordID(1, 2).String(),
			},
		},
		{
			name:   "given a leaf page holding entries for 20 and 30, when an entry for 10 is inserted at slot 0, then the entries read 10, 20, 30",
			layout: newTestIndexLayout,
			existing: []entry{
				{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
				{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 3)},
			},
			slot:     0,
			inserted: entry{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:   "given a leaf page holding entries for 10 and 30, when an entry for 20 is inserted at slot 1, then the entries read 10, 20, 30",
			layout: newTestIndexLayout,
			existing: []entry{
				{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 3)},
			},
			slot:     1,
			inserted: entry{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:   "given a leaf page holding entries for 10 and 20, when an entry for 30 is inserted at slot 2, then the entries read 10, 20, 30",
			layout: newTestIndexLayout,
			existing: []entry{
				{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
			},
			slot:     2,
			inserted: entry{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 3)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:   "given a leaf page on a varchar field holding an entry for 'bob', when an entry for 'alice' is inserted at slot 0, then the entries read 'alice', 'bob'",
			layout: newTestVarcharIndexLayout,
			existing: []entry{
				{value: query.NewStringConstant("bob"), rid: recordmanager.NewRecordID(0, 2)},
			},
			slot:     0,
			inserted: entry{value: query.NewStringConstant("alice"), rid: recordmanager.NewRecordID(0, 1)},
			want: []string{
				"'alice' " + recordmanager.NewRecordID(0, 1).String(),
				"'bob' " + recordmanager.NewRecordID(0, 2).String(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := tt.layout(t)

			blk, err := appendBTreeBlock(tx, testBTreeFile, layout, -1)
			if err != nil {
				t.Fatalf("appendBTreeBlock() error = %v", err)
			}
			page, err := newBTreePage(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreePage() error = %v", err)
			}
			defer page.close()

			for i, e := range tt.existing {
				if err := page.insertLeaf(i, e.value, e.rid); err != nil {
					t.Fatalf("insertLeaf(%d, %s, %s) error = %v", i, e.value, e.rid, err)
				}
			}

			if err := page.insertLeaf(tt.slot, tt.inserted.value, tt.inserted.rid); err != nil {
				t.Fatalf("insertLeaf(%d, %s, %s) error = %v", tt.slot, tt.inserted.value, tt.inserted.rid, err)
			}

			if got := leafEntries(t, page); !slices.Equal(got, tt.want) {
				t.Errorf("entries = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("given a leaf page on an int field holding an entry for 10, when an entry filed under the varchar '20' is inserted, then it reports ErrFieldTypeMismatch and the page still holds only the entry for 10", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)

		blk, err := appendBTreeBlock(tx, testBTreeFile, layout, -1)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}
		page, err := newBTreePage(tx, blk, layout)
		if err != nil {
			t.Fatalf("newBTreePage() error = %v", err)
		}
		defer page.close()

		if err := page.insertLeaf(0, query.NewIntConstant(10), recordmanager.NewRecordID(0, 1)); err != nil {
			t.Fatalf("insertLeaf(0, 10) error = %v", err)
		}

		err = page.insertLeaf(0, query.NewStringConstant("20"), recordmanager.NewRecordID(0, 2))
		if !errors.Is(err, recordmanager.ErrFieldTypeMismatch) {
			t.Errorf("insertLeaf(0, '20') error = %v, want %v", err, recordmanager.ErrFieldTypeMismatch)
		}

		want := []string{
			"10 " + recordmanager.NewRecordID(0, 1).String(),
		}
		if got := leafEntries(t, page); !slices.Equal(got, want) {
			t.Errorf("entries = %v, want %v", got, want)
		}
	})
}

// dirEntry is one directory record as a test inserts it: a value and the child
// block the entries from that value on are reached through.
type dirEntry struct {
	value            query.Constant
	childBlockNumber int
}

func TestBTreePageInsertDir(t *testing.T) {
	tests := []struct {
		name     string
		existing []dirEntry
		slot     int
		inserted dirEntry
		want     []string
	}{
		{
			name:     "given an empty directory page, when a record is inserted at slot 0, then the page holds that one record",
			slot:     0,
			inserted: dirEntry{value: query.NewIntConstant(10), childBlockNumber: 3},
			want: []string{
				"10 -> 3",
			},
		},
		{
			name: "given a directory page holding records for 20 and 30, when a record for 10 is inserted at slot 0, then the records read 10, 20, 30 with their child blocks",
			existing: []dirEntry{
				{value: query.NewIntConstant(20), childBlockNumber: 2},
				{value: query.NewIntConstant(30), childBlockNumber: 3},
			},
			slot:     0,
			inserted: dirEntry{value: query.NewIntConstant(10), childBlockNumber: 1},
			want: []string{
				"10 -> 1",
				"20 -> 2",
				"30 -> 3",
			},
		},
		{
			name: "given a directory page holding records for 10 and 30, when a record for 20 is inserted at slot 1, then the records read 10, 20, 30 with their child blocks",
			existing: []dirEntry{
				{value: query.NewIntConstant(10), childBlockNumber: 1},
				{value: query.NewIntConstant(30), childBlockNumber: 3},
			},
			slot:     1,
			inserted: dirEntry{value: query.NewIntConstant(20), childBlockNumber: 2},
			want: []string{
				"10 -> 1",
				"20 -> 2",
				"30 -> 3",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)

			blk, err := appendBTreeBlock(tx, testBTreeFile, layout, 0)
			if err != nil {
				t.Fatalf("appendBTreeBlock() error = %v", err)
			}
			page, err := newBTreePage(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreePage() error = %v", err)
			}
			defer page.close()

			for i, e := range tt.existing {
				if err := page.insertDir(i, e.value, e.childBlockNumber); err != nil {
					t.Fatalf("insertDir(%d, %s, %d) error = %v", i, e.value, e.childBlockNumber, err)
				}
			}

			if err := page.insertDir(tt.slot, tt.inserted.value, tt.inserted.childBlockNumber); err != nil {
				t.Fatalf("insertDir(%d, %s, %d) error = %v", tt.slot, tt.inserted.value, tt.inserted.childBlockNumber, err)
			}

			if got := dirEntries(t, page); !slices.Equal(got, tt.want) {
				t.Errorf("records = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("given a directory page on an int field holding a record for 10, when a record for the varchar '20' is inserted, then it reports ErrFieldTypeMismatch and the page still holds only the record for 10", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestDirLayout(t)

		blk, err := appendBTreeBlock(tx, testBTreeFile, layout, 0)
		if err != nil {
			t.Fatalf("appendBTreeBlock() error = %v", err)
		}
		page, err := newBTreePage(tx, blk, layout)
		if err != nil {
			t.Fatalf("newBTreePage() error = %v", err)
		}
		defer page.close()

		if err := page.insertDir(0, query.NewIntConstant(10), 1); err != nil {
			t.Fatalf("insertDir(0, 10, 1) error = %v", err)
		}

		err = page.insertDir(0, query.NewStringConstant("20"), 2)
		if !errors.Is(err, recordmanager.ErrFieldTypeMismatch) {
			t.Errorf("insertDir(0, '20', 2) error = %v, want %v", err, recordmanager.ErrFieldTypeMismatch)
		}

		want := []string{
			"10 -> 1",
		}
		if got := dirEntries(t, page); !slices.Equal(got, want) {
			t.Errorf("records = %v, want %v", got, want)
		}
	})
}

// newTestDirLayout builds the layout of the directory records of a B-tree on an
// int field: the child block, and the value the entries under it start from.
func newTestDirLayout(t *testing.T) *recordmanager.Layout {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddIntField(BlockNumberField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", BlockNumberField, err)
	}
	if err := s.AddIntField(IndexedValueField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", IndexedValueField, err)
	}

	return recordmanager.NewLayout(s)
}

// dirEntries reads every record of a directory page in slot order, each
// written out as its value and its child block so that they compare with ==.
func dirEntries(t *testing.T, page *btreePage) []string {
	t.Helper()

	n, err := page.numRecords()
	if err != nil {
		t.Fatalf("numRecords() error = %v", err)
	}

	got := make([]string, 0, n)
	for slot := range n {
		value, err := page.dataValue(slot)
		if err != nil {
			t.Fatalf("dataValue(%d) error = %v", slot, err)
		}
		child, err := page.childBlockNumber(slot)
		if err != nil {
			t.Fatalf("childBlockNumber(%d) error = %v", slot, err)
		}
		got = append(got, fmt.Sprintf("%s -> %d", value, child))
	}

	return got
}

// newTestVarcharIndexLayout builds the layout of an index on a varchar field
// of 10 characters, laid out the way the catalog lays one out.
func newTestVarcharIndexLayout(t *testing.T) *recordmanager.Layout {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddIntField(BlockNumberField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", BlockNumberField, err)
	}
	if err := s.AddIntField(SlotField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", SlotField, err)
	}
	if err := s.AddStringField(IndexedValueField, 10); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", IndexedValueField, err)
	}

	return recordmanager.NewLayout(s)
}

// leafEntries reads every record of a leaf page in slot order, each written
// out as its value and its record id so that they compare with ==.
func leafEntries(t *testing.T, page *btreePage) []string {
	t.Helper()

	n, err := page.numRecords()
	if err != nil {
		t.Fatalf("numRecords() error = %v", err)
	}

	got := make([]string, 0, n)
	for slot := range n {
		value, err := page.dataValue(slot)
		if err != nil {
			t.Fatalf("dataValue(%d) error = %v", slot, err)
		}
		rid, err := page.dataRecordID(slot)
		if err != nil {
			t.Fatalf("dataRecordID(%d) error = %v", slot, err)
		}
		got = append(got, value.String()+" "+rid.String())
	}

	return got
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
