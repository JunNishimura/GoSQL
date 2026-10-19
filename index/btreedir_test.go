package index

import (
	"errors"
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
