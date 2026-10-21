package index

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	testLeafFile = testIndexName + leafFileSuffix
	testDirFile  = testIndexName + dirFileSuffix
)

func TestNewBTreeIndex(t *testing.T) {
	tests := []struct {
		name       string
		leafLayout func(t *testing.T) *recordmanager.Layout
		dirLayout  func(t *testing.T) *recordmanager.Layout
		// wantRoot is the one record of the root, filing the leaf under the
		// least value of the indexed field's type.
		wantRoot string
	}{
		{
			name:       "given no files, when a B-tree index on an int field is opened, then it has one empty leaf and a root filing it under the least int",
			leafLayout: newTestIndexLayout,
			dirLayout:  newTestDirLayout,
			wantRoot:   "-2147483648 -> 0",
		},
		{
			name:       "given no files, when a B-tree index on a varchar field is opened, then it has one empty leaf and a root filing it under the empty string",
			leafLayout: newTestVarcharIndexLayout,
			dirLayout:  newTestVarcharDirLayout,
			wantRoot:   "'' -> 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			leafLayout := tt.leafLayout(t)
			dirLayout := tt.dirLayout(t)

			idx, err := NewBTreeIndex(tx, testIndexName, leafLayout)
			if err != nil {
				t.Fatalf("NewBTreeIndex() error = %v", err)
			}
			idx.Close()

			assertNewBTreeIndexFiles(t, tx, leafLayout, dirLayout, tt.wantRoot)
		})
	}

	t.Run("given a B-tree index already opened once, when it is opened again, then it still has one empty leaf and a root of one record", func(t *testing.T) {
		tx := newTestTransaction(t)
		leafLayout := newTestIndexLayout(t)
		dirLayout := newTestDirLayout(t)

		for range 2 {
			idx, err := NewBTreeIndex(tx, testIndexName, leafLayout)
			if err != nil {
				t.Fatalf("NewBTreeIndex() error = %v", err)
			}
			idx.Close()
		}

		assertNewBTreeIndexFiles(t, tx, leafLayout, dirLayout, "-2147483648 -> 0")
	})
}

// assertNewBTreeIndexFiles checks that the files of the index named
// testIndexName are as a new index leaves them: a leaf file of one empty leaf
// with no overflow, and a directory file of one root at level 0 holding
// wantRoot alone.
func assertNewBTreeIndexFiles(
	t *testing.T,
	tx *transaction.Transaction,
	leafLayout *recordmanager.Layout,
	dirLayout *recordmanager.Layout,
	wantRoot string,
) {
	t.Helper()

	if !assertFileSize(t, tx, testLeafFile, 1) || !assertFileSize(t, tx, testDirFile, 1) {
		return
	}

	leaf := filemanager.NewBlockId(testLeafFile, 0)
	root := filemanager.NewBlockId(testDirFile, 0)
	assertHeader(t, tx, leaf, leafLayout, -1, 0)
	assertHeader(t, tx, root, dirLayout, 0, 1)
	assertDirRecords(t, tx, root, dirLayout, []string{wantRoot})
}

// assertFileSize checks that fileName is want blocks long, and reports whether
// it is.
func assertFileSize(t *testing.T, tx *transaction.Transaction, fileName string, want int) bool {
	t.Helper()

	got, err := tx.Size(fileName)
	if err != nil {
		t.Fatalf("Size(%q) error = %v", fileName, err)
	}
	if got != want {
		t.Errorf("Size(%q) = %d, want %d", fileName, got, want)

		return false
	}

	return true
}
