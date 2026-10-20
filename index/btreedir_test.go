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

const testBTreeDirFile = "test_btree_dir"

func TestBTreeDirSearch(t *testing.T) {
	// A directory at level 0, so its children are leaves.
	entries := []dirEntry{
		{value: query.NewIntConstant(10), childBlockNumber: 1},
		{value: query.NewIntConstant(20), childBlockNumber: 2},
		{value: query.NewIntConstant(30), childBlockNumber: 3},
	}

	tests := []struct {
		name      string
		searchKey query.Constant
		want      int
		wantErr   error
	}{
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 15 is searched for, then it is in leaf 1, the one filed under the greatest value below it",
			searchKey: query.NewIntConstant(15),
			want:      1,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 25 is searched for, then it is in leaf 2",
			searchKey: query.NewIntConstant(25),
			want:      2,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 20 is searched for, then it is in leaf 2, the one filed under 20 itself",
			searchKey: query.NewIntConstant(20),
			want:      2,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 10 is searched for, then it is in leaf 1, the one filed under the first value",
			searchKey: query.NewIntConstant(10),
			want:      1,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 35 is searched for, then it is in leaf 3, the one filed under the last value",
			searchKey: query.NewIntConstant(35),
			want:      3,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when 5 is searched for, which is less than every value, then it reports ErrNoChildBlock",
			searchKey: query.NewIntConstant(5),
			wantErr:   ErrNoChildBlock,
		},
		{
			name:      "given a directory filing leaves 1, 2 and 3 under 10, 20 and 30, when the varchar '20' is searched for, then it reports ErrConstantTypeMismatch",
			searchKey: query.NewStringConstant("20"),
			wantErr:   query.ErrConstantTypeMismatch,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)
			blk := newTestDirBlock(t, tx, layout, 0, entries)

			dir, err := newBTreeDir(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			defer dir.close()

			got, err := dir.search(tt.searchKey)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("search(%s) error = %v, want %v", tt.searchKey, err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("search(%s) error = %v", tt.searchKey, err)
			}
			if got != tt.want {
				t.Errorf("search(%s) = %d, want %d", tt.searchKey, got, tt.want)
			}
		})
	}
}

func TestBTreeDirSearchDown(t *testing.T) {
	// A root at level 1 over two directory blocks at level 0, whose children
	// are leaves 10 to 13:
	//
	//	block 0, level 1: 0 -> block 1, 50 -> block 2
	//	block 1, level 0: 0 -> leaf 10, 20 -> leaf 11
	//	block 2, level 0: 50 -> leaf 12, 70 -> leaf 13
	twoLevels := [][]dirEntry{
		{
			{value: query.NewIntConstant(0), childBlockNumber: 1},
			{value: query.NewIntConstant(50), childBlockNumber: 2},
		},
		{
			{value: query.NewIntConstant(0), childBlockNumber: 10},
			{value: query.NewIntConstant(20), childBlockNumber: 11},
		},
		{
			{value: query.NewIntConstant(50), childBlockNumber: 12},
			{value: query.NewIntConstant(70), childBlockNumber: 13},
		},
	}
	twoLevelsLevels := []int32{1, 0, 0}

	tests := []struct {
		name      string
		blocks    [][]dirEntry
		levels    []int32
		searchKey query.Constant
		want      int
	}{
		{
			name:      "given a root at level 1 over directory blocks 1 and 2, when 5 is searched for, then it goes down block 1 to leaf 10",
			blocks:    twoLevels,
			levels:    twoLevelsLevels,
			searchKey: query.NewIntConstant(5),
			want:      10,
		},
		{
			name:      "given a root at level 1 over directory blocks 1 and 2, when 49 is searched for, then it goes down block 1 to leaf 11",
			blocks:    twoLevels,
			levels:    twoLevelsLevels,
			searchKey: query.NewIntConstant(49),
			want:      11,
		},
		{
			name:      "given a root at level 1 over directory blocks 1 and 2, when 50 is searched for, then it goes down block 2 to leaf 12",
			blocks:    twoLevels,
			levels:    twoLevelsLevels,
			searchKey: query.NewIntConstant(50),
			want:      12,
		},
		{
			name:      "given a root at level 1 over directory blocks 1 and 2, when 80 is searched for, then it goes down block 2 to leaf 13",
			blocks:    twoLevels,
			levels:    twoLevelsLevels,
			searchKey: query.NewIntConstant(80),
			want:      13,
		},
		{
			name: "given a root at level 2 over a block at level 1 over a block at level 0, when 60 is searched for, then it goes down both to leaf 11",
			blocks: [][]dirEntry{
				{{value: query.NewIntConstant(0), childBlockNumber: 1}},
				{{value: query.NewIntConstant(0), childBlockNumber: 2}},
				{
					{value: query.NewIntConstant(0), childBlockNumber: 10},
					{value: query.NewIntConstant(50), childBlockNumber: 11},
				},
			},
			levels:    []int32{2, 1, 0},
			searchKey: query.NewIntConstant(60),
			want:      11,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)

			var root *filemanager.BlockId
			for i, entries := range tt.blocks {
				blk := newTestDirBlock(t, tx, layout, tt.levels[i], entries)
				if i == 0 {
					root = blk
				}
			}

			dir, err := newBTreeDir(tx, root, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			defer dir.close()

			got, err := dir.search(tt.searchKey)
			if err != nil {
				t.Fatalf("search(%s) error = %v", tt.searchKey, err)
			}
			if got != tt.want {
				t.Errorf("search(%s) = %d, want %d", tt.searchKey, got, tt.want)
			}
		})
	}
}

func TestBTreeDirInsert(t *testing.T) {
	// A directory at level 0, so the records go in this block itself.
	entries := []dirEntry{
		{value: query.NewIntConstant(10), childBlockNumber: 1},
		{value: query.NewIntConstant(30), childBlockNumber: 3},
	}

	tests := []struct {
		name     string
		inserted dirEntry
		want     []string
	}{
		{
			name:     "given a directory at level 0 filing blocks under 10 and 30, when a record for 20 is inserted, then it goes between them and nothing comes back for the level above",
			inserted: dirEntry{value: query.NewIntConstant(20), childBlockNumber: 2},
			want:     []string{"10 -> 1", "20 -> 2", "30 -> 3"},
		},
		{
			name:     "given a directory at level 0 filing blocks under 10 and 30, when a record for 5 is inserted, then it goes first and nothing comes back for the level above",
			inserted: dirEntry{value: query.NewIntConstant(5), childBlockNumber: 9},
			want:     []string{"5 -> 9", "10 -> 1", "30 -> 3"},
		},
		{
			name:     "given a directory at level 0 filing blocks under 10 and 30, when a record for 40 is inserted, then it goes last and nothing comes back for the level above",
			inserted: dirEntry{value: query.NewIntConstant(40), childBlockNumber: 4},
			want:     []string{"10 -> 1", "30 -> 3", "40 -> 4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)
			blk := newTestDirBlock(t, tx, layout, 0, entries)

			dir, err := newBTreeDir(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			got, err := dir.insert(tt.inserted)
			dir.close()
			if err != nil {
				t.Fatalf("insert(%+v) error = %v", tt.inserted, err)
			}
			if got != nil {
				t.Errorf("insert(%+v) = %+v, want nil", tt.inserted, got)
			}

			page, err := newBTreePage(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreePage() error = %v", err)
			}
			defer page.close()

			if records := dirEntries(t, page); !slices.Equal(records, tt.want) {
				t.Errorf("records = %v, want %v", records, tt.want)
			}
		})
	}
}

// A directory record on an int field is 12 bytes, so 32 of them fit in the 392
// bytes a block has past its header. Each case fills a directory block with 31
// and inserts a 32nd, which fills it and splits it in the middle, at slot 16.
func TestBTreeDirInsertSplit(t *testing.T) {
	// The records for 0, 10, ..., 310, filing child blocks 100, 101, ..., 131.
	all := dirEntriesStepping(0, 310)

	tests := []struct {
		name     string
		existing []dirEntry
		inserted dirEntry
	}{
		{
			name:     "given a directory block at level 0 of 31 records for 0 to 300, when a record for 310 fills it, then it keeps 0 to 150, a new block at level 0 takes 160 to 310, and the new block comes back filed under 160",
			existing: all[:31],
			inserted: all[31],
		},
		{
			name:     "given a directory block at level 0 of 31 records for 10 to 310, when a record for 0 fills it, then it keeps 0 to 150, a new block at level 0 takes 160 to 310, and the new block comes back filed under 160",
			existing: all[1:],
			inserted: all[0],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)
			blk := newTestDirBlock(t, tx, layout, 0, tt.existing)

			dir, err := newBTreeDir(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			got, err := dir.insert(tt.inserted)
			dir.close()
			if err != nil {
				t.Fatalf("insert(%+v) error = %v", tt.inserted, err)
			}

			newBlk := filemanager.NewBlockId(testBTreeDirFile, 1)
			wantEntry := dirEntry{value: query.NewIntConstant(160), childBlockNumber: newBlk.Number()}
			if got == nil || *got != wantEntry {
				t.Errorf("insert(%+v) = %+v, want %+v", tt.inserted, got, wantEntry)
			}

			assertDirRecords(t, tx, blk, layout, dirStrings(all[:16]))
			assertDirRecords(t, tx, newBlk, layout, dirStrings(all[16:]))
			assertHeader(t, tx, blk, layout, 0, 16)
			assertHeader(t, tx, newBlk, layout, 0, 16)
		})
	}
}

func TestBTreeDirInsertDown(t *testing.T) {
	// A root at level 1 over two directory blocks at level 0:
	//
	//	block 0, level 1: 0 -> block 1, 50 -> block 2
	//	block 1, level 0: 0 -> 10, 20 -> 11
	//	block 2, level 0: 50 -> 12, 70 -> 13
	twoLevels := [][]dirEntry{
		{
			{value: query.NewIntConstant(0), childBlockNumber: 1},
			{value: query.NewIntConstant(50), childBlockNumber: 2},
		},
		{
			{value: query.NewIntConstant(0), childBlockNumber: 10},
			{value: query.NewIntConstant(20), childBlockNumber: 11},
		},
		{
			{value: query.NewIntConstant(50), childBlockNumber: 12},
			{value: query.NewIntConstant(70), childBlockNumber: 13},
		},
	}

	tests := []struct {
		name     string
		inserted dirEntry
		// want is the records of blocks 0, 1 and 2 afterwards.
		want [][]string
	}{
		{
			name:     "given a root at level 1 over directory blocks 1 and 2, when a record for 30 is inserted, then it goes in block 1, the root is left as it was, and nothing comes back",
			inserted: dirEntry{value: query.NewIntConstant(30), childBlockNumber: 14},
			want: [][]string{
				{"0 -> 1", "50 -> 2"},
				{"0 -> 10", "20 -> 11", "30 -> 14"},
				{"50 -> 12", "70 -> 13"},
			},
		},
		{
			name:     "given a root at level 1 over directory blocks 1 and 2, when a record for 60 is inserted, then it goes in block 2, the root is left as it was, and nothing comes back",
			inserted: dirEntry{value: query.NewIntConstant(60), childBlockNumber: 14},
			want: [][]string{
				{"0 -> 1", "50 -> 2"},
				{"0 -> 10", "20 -> 11"},
				{"50 -> 12", "60 -> 14", "70 -> 13"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)
			root := newTestDirBlocks(t, tx, layout, []int32{1, 0, 0}, twoLevels)

			dir, err := newBTreeDir(tx, root, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			got, err := dir.insert(tt.inserted)
			dir.close()
			if err != nil {
				t.Fatalf("insert(%+v) error = %v", tt.inserted, err)
			}
			if got != nil {
				t.Errorf("insert(%+v) = %+v, want nil", tt.inserted, got)
			}

			for i, want := range tt.want {
				assertDirRecords(t, tx, filemanager.NewBlockId(testBTreeDirFile, i), layout, want)
			}
		})
	}

	t.Run("given a root at level 1 over a full directory block 1, when a record that fills block 1 is inserted, then block 1 splits into a new block 2 at level 0, the root files it, and nothing comes back", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestDirLayout(t)

		// The records for 0, 10, ..., 310, filing child blocks 100 to 131.
		all := dirEntriesStepping(0, 310)
		root := newTestDirBlocks(t, tx, layout, []int32{1, 0}, [][]dirEntry{
			{{value: query.NewIntConstant(0), childBlockNumber: 1}},
			all[:31],
		})

		dir, err := newBTreeDir(tx, root, layout)
		if err != nil {
			t.Fatalf("newBTreeDir() error = %v", err)
		}
		got, err := dir.insert(all[31])
		dir.close()
		if err != nil {
			t.Fatalf("insert(%+v) error = %v", all[31], err)
		}
		if got != nil {
			t.Errorf("insert(%+v) = %+v, want nil", all[31], got)
		}

		assertDirRecords(t, tx, root, layout, []string{"0 -> 1", "160 -> 2"})
		assertDirRecords(t, tx, filemanager.NewBlockId(testBTreeDirFile, 1), layout, dirStrings(all[:16]))
		assertDirRecords(t, tx, filemanager.NewBlockId(testBTreeDirFile, 2), layout, dirStrings(all[16:]))
		assertHeader(t, tx, root, layout, 1, 2)
		assertHeader(t, tx, filemanager.NewBlockId(testBTreeDirFile, 2), layout, 0, 16)
	})

	// The root's records other than the one for 300 file child blocks that do
	// not exist, which is all right: the insert only goes down the one for 300.
	t.Run("given a full root at level 1 whose last record files a full directory block 1, when a record that fills block 1 is inserted, then block 1 splits into a new block 2, the root splits into a new block 3 at level 1, and the record for block 3 comes back", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestDirLayout(t)

		rootEntries := append(dirEntriesStepping(0, 290), dirEntry{value: query.NewIntConstant(300), childBlockNumber: 1})
		childEntries := dirEntriesStepping(300, 600)
		root := newTestDirBlocks(t, tx, layout, []int32{1, 0}, [][]dirEntry{rootEntries, childEntries})

		inserted := dirEntry{value: query.NewIntConstant(610), childBlockNumber: 999}
		dir, err := newBTreeDir(tx, root, layout)
		if err != nil {
			t.Fatalf("newBTreeDir() error = %v", err)
		}
		got, err := dir.insert(inserted)
		dir.close()
		if err != nil {
			t.Fatalf("insert(%+v) error = %v", inserted, err)
		}

		wantEntry := dirEntry{value: query.NewIntConstant(160), childBlockNumber: 3}
		if got == nil || *got != wantEntry {
			t.Errorf("insert(%+v) = %+v, want %+v", inserted, got, wantEntry)
		}

		// Block 1 keeps 300 to 450 and block 2 takes 460 to 610; the root
		// files block 2 under 460, then keeps its first 16 records and moves
		// the other 16 to block 3.
		wantBlock3 := append(dirStrings(rootEntries[16:]), "460 -> 2")
		assertDirRecords(t, tx, root, layout, dirStrings(rootEntries[:16]))
		assertDirRecords(t, tx, filemanager.NewBlockId(testBTreeDirFile, 3), layout, wantBlock3)
		assertHeader(t, tx, filemanager.NewBlockId(testBTreeDirFile, 2), layout, 0, 16)
		assertHeader(t, tx, filemanager.NewBlockId(testBTreeDirFile, 3), layout, 1, 16)
	})
}

func TestBTreeDirMakeNewRoot(t *testing.T) {
	tests := []struct {
		name  string
		level int32
	}{
		{
			name:  "given a root at level 0 filing blocks under 0 and 50, when a new root is made over it and a record for 100, then the root is at level 1 over a new block at level 0 holding what it held, and the record for 100",
			level: 0,
		},
		{
			name:  "given a root at level 1 filing blocks under 0 and 50, when a new root is made over it and a record for 100, then the root is at level 2 over a new block at level 1 holding what it held, and the record for 100",
			level: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestDirLayout(t)
			root := newTestDirBlock(t, tx, layout, tt.level, []dirEntry{
				{value: query.NewIntConstant(0), childBlockNumber: 10},
				{value: query.NewIntConstant(50), childBlockNumber: 11},
			})

			dir, err := newBTreeDir(tx, root, layout)
			if err != nil {
				t.Fatalf("newBTreeDir() error = %v", err)
			}
			e := dirEntry{value: query.NewIntConstant(100), childBlockNumber: 12}
			err = dir.makeNewRoot(e)
			dir.close()
			if err != nil {
				t.Fatalf("makeNewRoot(%+v) error = %v", e, err)
			}

			moved := filemanager.NewBlockId(testBTreeDirFile, 1)
			assertDirRecords(t, tx, root, layout, []string{"0 -> 1", "100 -> 12"})
			assertDirRecords(t, tx, moved, layout, []string{"0 -> 10", "50 -> 11"})
			assertHeader(t, tx, root, layout, tt.level+1, 2)
			assertHeader(t, tx, moved, layout, tt.level, 2)
		})
	}

	// The directory of TestBTreeDirInsertDown's last case, whose root has just
	// split, made whole again by a new root over the two halves.
	t.Run("given a full root at level 1 that has just split on an insert of 610, when a new root is made over it and the record that came back, then a search for 610 goes down three levels to the child it was filed with", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestDirLayout(t)

		rootEntries := append(dirEntriesStepping(0, 290), dirEntry{value: query.NewIntConstant(300), childBlockNumber: 1})
		root := newTestDirBlocks(t, tx, layout, []int32{1, 0}, [][]dirEntry{rootEntries, dirEntriesStepping(300, 600)})

		inserted := dirEntry{value: query.NewIntConstant(610), childBlockNumber: 999}
		dir, err := newBTreeDir(tx, root, layout)
		if err != nil {
			t.Fatalf("newBTreeDir() error = %v", err)
		}
		fromRoot, err := dir.insert(inserted)
		if err != nil {
			dir.close()
			t.Fatalf("insert(%+v) error = %v", inserted, err)
		}
		if fromRoot == nil {
			dir.close()
			t.Fatalf("insert(%+v) = nil, want the record for the root's new half", inserted)
		}
		err = dir.makeNewRoot(*fromRoot)
		dir.close()
		if err != nil {
			t.Fatalf("makeNewRoot(%+v) error = %v", *fromRoot, err)
		}

		dir, err = newBTreeDir(tx, root, layout)
		if err != nil {
			t.Fatalf("newBTreeDir() error = %v", err)
		}
		defer dir.close()

		got, err := dir.search(inserted.value)
		if err != nil {
			t.Fatalf("search(%s) error = %v", inserted.value, err)
		}
		if got != inserted.childBlockNumber {
			t.Errorf("search(%s) = %d, want %d", inserted.value, got, inserted.childBlockNumber)
		}
	})
}

// newTestDirBlocks appends one directory block per element of blocks to
// testBTreeDirFile, at the level levels gives for it, and returns the first.
func newTestDirBlocks(
	t *testing.T,
	tx *transaction.Transaction,
	layout *recordmanager.Layout,
	levels []int32,
	blocks [][]dirEntry,
) *filemanager.BlockId {
	t.Helper()

	var first *filemanager.BlockId
	for i, entries := range blocks {
		blk := newTestDirBlock(t, tx, layout, levels[i], entries)
		if i == 0 {
			first = blk
		}
	}

	return first
}

// dirEntriesStepping is the directory records for from, from+10, ..., to, the
// one for value v filing child block 100+v/10.
func dirEntriesStepping(from, to int32) []dirEntry {
	var entries []dirEntry
	for v := from; v <= to; v += 10 {
		entries = append(entries, dirEntry{value: query.NewIntConstant(v), childBlockNumber: 100 + int(v/10)})
	}

	return entries
}

// dirStrings writes entries out the way dirEntries reads records back.
func dirStrings(entries []dirEntry) []string {
	s := make([]string, len(entries))
	for i, e := range entries {
		s[i] = fmt.Sprintf("%s -> %d", e.value, e.childBlockNumber)
	}

	return s
}

// assertDirRecords checks the records of the directory block blk, in slot
// order, or that there are none if the file has no such block.
func assertDirRecords(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	want []string,
) {
	t.Helper()

	size, err := tx.Size(blk.FileName())
	if err != nil {
		t.Fatalf("Size(%q) error = %v", blk.FileName(), err)
	}
	if blk.Number() >= size {
		t.Errorf("%v does not exist, want it to hold %v", blk, want)

		return
	}

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage(%v) error = %v", blk, err)
	}
	defer page.close()

	if got := dirEntries(t, page); !slices.Equal(got, want) {
		t.Errorf("records of %v = %v, want %v", blk, got, want)
	}
}

// newTestDirBlock appends a directory block at level to testBTreeDirFile and
// fills it with entries, in the order given, which the caller keeps sorted.
func newTestDirBlock(
	t *testing.T,
	tx *transaction.Transaction,
	layout *recordmanager.Layout,
	level int32,
	entries []dirEntry,
) *filemanager.BlockId {
	t.Helper()

	blk, err := appendBTreeBlock(tx, testBTreeDirFile, layout, level)
	if err != nil {
		t.Fatalf("appendBTreeBlock() error = %v", err)
	}
	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage() error = %v", err)
	}
	defer page.close()

	for i, e := range entries {
		if err := page.insertDir(i, e.value, e.childBlockNumber); err != nil {
			t.Fatalf("insertDir(%d, %s, %d) error = %v", i, e.value, e.childBlockNumber, err)
		}
	}

	return blk
}
