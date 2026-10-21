package index

import (
	"errors"
	"math"
	"slices"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
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

// The record ids are compared without their order: an index says nothing of
// which of the entries for one value comes first, and a B-tree files a new one
// ahead of those already there.
func TestBTreeIndexMoveToNextMatch(t *testing.T) {
	tests := []struct {
		name      string
		layout    func(t *testing.T) *recordmanager.Layout
		entries   []entry
		searchKey query.Constant
		want      []string
	}{
		{
			name:   "given entries filed under 7 and under 8, when 7 is searched for, then the record ids filed under 7 come back",
			layout: newTestIndexLayout,
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(1, 2)},
			},
			searchKey: query.NewIntConstant(7),
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(1, 2).String(),
			},
		},
		{
			name:   "given entries filed under 7 and under 8, when 8 is searched for, then the record id filed under 8 comes back",
			layout: newTestIndexLayout,
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(1, 2)},
			},
			searchKey: query.NewIntConstant(8),
			want: []string{
				recordmanager.NewRecordID(0, 1).String(),
			},
		},
		{
			name:   "given entries filed under 7 and under 8, when 9 is searched for, then nothing comes back",
			layout: newTestIndexLayout,
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 1)},
			},
			searchKey: query.NewIntConstant(9),
			want:      []string{},
		},
		// The least int is also the value the root files the first leaf
		// under, so this is the one search that starts from the root's very
		// first record.
		{
			name:   "given an entry filed under the least int, when the least int is searched for, then its record id comes back",
			layout: newTestIndexLayout,
			entries: []entry{
				{value: query.NewIntConstant(math.MinInt32), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(0), rid: recordmanager.NewRecordID(0, 1)},
			},
			searchKey: query.NewIntConstant(math.MinInt32),
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
			},
		},
		{
			name:   "given entries filed under 'alice' and under 'bob' in an index on a varchar field, when 'alice' is searched for, then the record ids filed under 'alice' come back",
			layout: newTestVarcharIndexLayout,
			entries: []entry{
				{value: query.NewStringConstant("alice"), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewStringConstant("bob"), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewStringConstant("alice"), rid: recordmanager.NewRecordID(1, 2)},
			},
			searchKey: query.NewStringConstant("alice"),
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(1, 2).String(),
			},
		},
		{
			name:      "given an empty index, when 7 is searched for, then nothing comes back",
			layout:    newTestIndexLayout,
			searchKey: query.NewIntConstant(7),
			want:      []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, err := NewBTreeIndex(newTestTransaction(t), testIndexName, tt.layout(t))
			if err != nil {
				t.Fatalf("NewBTreeIndex() error = %v", err)
			}
			defer idx.Close()

			insertEntries(t, idx, tt.entries)

			got := searchRecordIDs(t, idx, tt.searchKey)
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tt.want))
			if !slices.Equal(got, want) {
				t.Errorf("record ids filed under %s = %v, want %v in any order", tt.searchKey, got, want)
			}
		})
	}

	t.Run("given a B-tree index that has not been searched, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})

	t.Run("given a B-tree index that was searched and then closed, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		insertEntries(t, idx, []entry{
			{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
		})
		if err := idx.MoveBeforeFirstMatch(query.NewIntConstant(7)); err != nil {
			t.Fatalf("MoveBeforeFirstMatch(7) error = %v", err)
		}
		idx.Close()

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})

	// An insert searches for its value to get to the leaf, and gives the leaf
	// back once the entry is in, so it leaves no search under way.
	t.Run("given a B-tree index that was searched and then had an entry inserted, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		if err := idx.MoveBeforeFirstMatch(query.NewIntConstant(7)); err != nil {
			t.Fatalf("MoveBeforeFirstMatch(7) error = %v", err)
		}
		insertEntries(t, idx, []entry{
			{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
		})

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})
}

// A leaf on an int field holds 24 entries, so 100 of them fill one leaf after
// another, and each split has to reach the directory for the entries moved into
// the new leaf to be found. Where the splits fall turns on the order the values
// come in, so each case inserts them in a different one.
func TestBTreeIndexInsert(t *testing.T) {
	const n = 100

	tests := []struct {
		name  string
		order func(i int) int32
	}{
		{
			name:  "given an empty B-tree index, when 100 entries for 0 to 99 are inserted in ascending order, then each value finds only its own record id",
			order: func(i int) int32 { return int32(i) },
		},
		{
			name:  "given an empty B-tree index, when 100 entries for 0 to 99 are inserted in descending order, then each value finds only its own record id",
			order: func(i int) int32 { return int32(n - 1 - i) },
		},
		// 37 and 100 have no factor in common, so this goes through every
		// value from 0 to 99 once, jumping about.
		{
			name:  "given an empty B-tree index, when 100 entries for 0 to 99 are inserted in no order, then each value finds only its own record id",
			order: func(i int) int32 { return int32(i * 37 % n) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := newTestBTreeIndex(t)
			defer idx.Close()

			entries := make([]entry, n)
			for i := range entries {
				v := tt.order(i)
				entries[i] = entry{value: query.NewIntConstant(v), rid: recordmanager.NewRecordID(int(v)/10, int(v)%10)}
			}
			insertEntries(t, idx, entries)

			assertEachValueFinds(t, idx, entries)
			if got := searchRecordIDs(t, idx, query.NewIntConstant(n)); len(got) != 0 {
				t.Errorf("record ids filed under %d = %v, want none", n, got)
			}
		})
	}
}

// assertEachValueFinds checks that a search for each value of entries finds the
// record ids of the entries filed under it, and no others, in any order.
func assertEachValueFinds(t *testing.T, idx Index, entries []entry) {
	t.Helper()

	want := make(map[query.Constant][]string)
	for _, e := range entries {
		want[e.value] = append(want[e.value], e.rid.String())
	}

	for value, rids := range want {
		got := searchRecordIDs(t, idx, value)
		slices.Sort(got)
		slices.Sort(rids)
		if !slices.Equal(got, rids) {
			t.Errorf("record ids filed under %s = %v, want %v in any order", value, got, rids)
		}
	}
}

func TestBTreeIndexDataRecordID(t *testing.T) {
	t.Run("given a B-tree index that has not been searched, when the record id is asked for, then it reports ErrNoSearch", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		if _, err := idx.DataRecordID(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("DataRecordID() error = %v, want %v", err, ErrNoSearch)
		}
	})
}

// newTestBTreeIndex opens a B-tree index on an int field over a database of its
// own.
func newTestBTreeIndex(t *testing.T) *BTreeIndex {
	t.Helper()

	idx, err := NewBTreeIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
	if err != nil {
		t.Fatalf("NewBTreeIndex() error = %v", err)
	}

	return idx
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
