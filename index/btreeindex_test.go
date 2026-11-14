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
		// wantRoot is the only entry of the root. It maps the minimum key of
		// the indexed field's type to the leaf.
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

// The record ids are compared without regard to order. An index does not
// define the order of the entries for one key, and a B-tree inserts a new
// entry before the existing ones.
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
		// The minimum int is also the key of the root's entry for the first
		// leaf, so this is the one search that starts from the first entry of
		// the root.
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

	// An insert searches for its value to find the leaf, and unpins the leaf
	// once the entry is inserted. It leaves no search in progress.
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

// A leaf on an int field holds 24 entries, so 100 entries fill several
// leaves. Each split must add a directory entry, or the entries moved to the
// new leaf cannot be found. The positions of the splits depend on the insert
// order, so each case inserts the values in a different order.
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
		// 37 and 100 are coprime, so this visits every value from 0 to 99 once,
		// out of order.
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

// A leaf holds at most 23 entries between inserts, because it splits as soon
// as it reaches 24. 800 entries therefore need at least 35 leaves, whatever
// the insert order. The root holds one entry per leaf and splits when it
// reaches 32, so the tree must grow a level for every leaf to be reachable.
// The test checks the root's level, so a case that did not split the root
// cannot pass.
func TestBTreeIndexInsertGrowingRoot(t *testing.T) {
	const n = 800

	tests := []struct {
		name  string
		order func(i int) int32
	}{
		{
			name:  "given an empty B-tree index, when 800 entries for 0 to 799 are inserted in ascending order, then the root grows to level 1 and each value finds only its own record id",
			order: func(i int) int32 { return int32(i) },
		},
		// 37 and 800 are coprime, so this visits every value from 0 to 799
		// once, out of order.
		{
			name:  "given an empty B-tree index, when 800 entries for 0 to 799 are inserted in no order, then the root grows to level 1 and each value finds only its own record id",
			order: func(i int) int32 { return int32(i * 37 % n) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			idx, err := NewBTreeIndex(tx, testIndexName, newTestIndexLayout(t))
			if err != nil {
				t.Fatalf("NewBTreeIndex() error = %v", err)
			}
			defer idx.Close()

			entries := make([]entry, n)
			for i := range entries {
				v := tt.order(i)
				entries[i] = entry{value: query.NewIntConstant(v), rid: recordmanager.NewRecordID(int(v)/10, int(v)%10)}
			}
			insertEntries(t, idx, entries)

			assertFlag(t, tx, filemanager.NewBlockId(testDirFile, 0), newTestDirLayout(t), 1)
			assertEachValueFinds(t, idx, entries)
		})
	}
}

// More than 24 entries for one key do not fit in a leaf, so they overflow
// into a chain of blocks. Entries for other keys must be inserted around that
// chain.
func TestBTreeIndexInsertDuplicates(t *testing.T) {
	tests := []struct {
		name    string
		entries func() []entry
	}{
		{
			name: "given an empty B-tree index, when 60 entries for 50 are inserted in among one each for 0 to 99, then each value finds only its own record ids",
			entries: func() []entry {
				var entries []entry
				for i := range 100 {
					entries = append(entries, entry{value: query.NewIntConstant(int32(i)), rid: recordmanager.NewRecordID(0, i)})
					if i < 60 {
						entries = append(entries, entry{value: query.NewIntConstant(50), rid: recordmanager.NewRecordID(1, i)})
					}
				}

				return entries
			},
		},
		// The entries for 50 are inserted first, so the leaf overflows before
		// any other key exists. The smaller keys are then inserted into the
		// overflowing leaf, which must move its entries out to accept them.
		{
			name: "given an empty B-tree index, when 60 entries for 50 are inserted and then one each for 0 to 99, then each value finds only its own record ids",
			entries: func() []entry {
				var entries []entry
				for i := range 60 {
					entries = append(entries, entry{value: query.NewIntConstant(50), rid: recordmanager.NewRecordID(1, i)})
				}
				for i := range 100 {
					entries = append(entries, entry{value: query.NewIntConstant(int32(i)), rid: recordmanager.NewRecordID(0, i)})
				}

				return entries
			},
		},
		{
			name: "given an empty B-tree index, when 40 entries each for 10, 20 and 30 are inserted taking turns, then each value finds only its own record ids",
			entries: func() []entry {
				var entries []entry
				for i := range 40 {
					for _, v := range []int32{10, 20, 30} {
						entries = append(entries, entry{value: query.NewIntConstant(v), rid: recordmanager.NewRecordID(int(v), i)})
					}
				}

				return entries
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := newTestBTreeIndex(t)
			defer idx.Close()

			entries := tt.entries()
			insertEntries(t, idx, entries)

			assertEachValueFinds(t, idx, entries)
		})
	}
}

func TestBTreeIndexDelete(t *testing.T) {
	tests := []struct {
		name    string
		entries []entry
		deleted entry
		wantErr error
		// want maps each value that is searched for afterwards to the record
		// ids the search should return, in any order.
		want map[query.Constant][]string
	}{
		{
			name: "given two record ids filed under 7, when one of them is deleted, then the other is still filed under 7",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			want: map[query.Constant][]string{
				query.NewIntConstant(7): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
		{
			name: "given one record id filed under both 7 and 8, when it is deleted from under 8, then it is filed under 7 and no longer under 8",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 0)},
			want: map[query.Constant][]string{
				query.NewIntConstant(7): {recordmanager.NewRecordID(0, 0).String()},
				query.NewIntConstant(8): {},
			},
		},
		{
			name: "given a record id filed under 7, when another record id is deleted from under 7, then it reports ErrEntryNotFound and the one filed is still there",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			wantErr: ErrEntryNotFound,
			want: map[query.Constant][]string{
				query.NewIntConstant(7): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
		{
			name: "given a record id filed under 8, when it is deleted from under 7, then it reports ErrEntryNotFound and it is still filed under 8",
			entries: []entry{
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
			wantErr: ErrEntryNotFound,
			want: map[query.Constant][]string{
				query.NewIntConstant(8): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := newTestBTreeIndex(t)
			defer idx.Close()

			insertEntries(t, idx, tt.entries)

			if err := idx.Delete(tt.deleted.value, tt.deleted.rid); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Delete(%s, %s) error = %v, want %v", tt.deleted.value, tt.deleted.rid, err, tt.wantErr)
			}

			for searchKey, want := range tt.want {
				got := searchRecordIDs(t, idx, searchKey)
				slices.Sort(got)
				if !slices.Equal(got, slices.Sorted(slices.Values(want))) {
					t.Errorf("record ids filed under %s = %v, want %v in any order", searchKey, got, want)
				}
			}
		})
	}

	// 60 entries for 50 overflow into a chain. Deleting all of them removes
	// some entries from the leaf at the head of the chain and some from its
	// overflow blocks, and empties those blocks one after another.
	t.Run("given 60 entries for 50 in among one each for 0 to 99, when every entry for 50 is deleted, then 50 finds nothing and each other value still finds its own record id", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		var others, fifties []entry
		for i := range 100 {
			others = append(others, entry{value: query.NewIntConstant(int32(i)), rid: recordmanager.NewRecordID(0, i)})
			if i < 60 {
				fifties = append(fifties, entry{value: query.NewIntConstant(50), rid: recordmanager.NewRecordID(1, i)})
			}
		}
		insertEntries(t, idx, append(slices.Clone(others), fifties...))

		for _, e := range fifties {
			if err := idx.Delete(e.value, e.rid); err != nil {
				t.Fatalf("Delete(%s, %s) error = %v", e.value, e.rid, err)
			}
		}

		// Only one entry for 50 remains, among the entries for other keys.
		assertEachValueFinds(t, idx, others)
	})

	t.Run("given 100 entries for 0 to 99, when the entries for the even values are deleted, then the even values find nothing and the odd values still find their own record ids", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		var odd, even []entry
		for i := range 100 {
			e := entry{value: query.NewIntConstant(int32(i)), rid: recordmanager.NewRecordID(0, i)}
			if i%2 == 0 {
				even = append(even, e)
			} else {
				odd = append(odd, e)
			}
		}
		insertEntries(t, idx, append(slices.Clone(even), odd...))

		for _, e := range even {
			if err := idx.Delete(e.value, e.rid); err != nil {
				t.Fatalf("Delete(%s, %s) error = %v", e.value, e.rid, err)
			}
		}

		assertEachValueFinds(t, idx, odd)
		for _, e := range even {
			if got := searchRecordIDs(t, idx, e.value); len(got) != 0 {
				t.Errorf("record ids filed under %s = %v, want none", e.value, got)
			}
		}
	})

	// A delete searches for its value to find the entry, and unpins the leaf
	// once the entry is removed. It leaves no search in progress.
	t.Run("given a B-tree index that was searched and then had an entry deleted, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := newTestBTreeIndex(t)
		defer idx.Close()

		insertEntries(t, idx, []entry{
			{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
			{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
		})
		if err := idx.MoveBeforeFirstMatch(query.NewIntConstant(7)); err != nil {
			t.Fatalf("MoveBeforeFirstMatch(7) error = %v", err)
		}
		if err := idx.Delete(query.NewIntConstant(7), recordmanager.NewRecordID(0, 0)); err != nil {
			t.Fatalf("Delete(7, %s) error = %v", recordmanager.NewRecordID(0, 0), err)
		}

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})
}

// A search reads one directory block per level and then the leaf. The cost
// is therefore the number of directory levels a root needs to reach
// numBlocks leaves, plus one. The root always exists, so there is at least
// one level. The level count rises by one each time the number of leaves
// passes a power of the fan-out of a directory block.
func TestBTreeIndexSearchCost(t *testing.T) {
	tests := []struct {
		name            string
		numBlocks       int
		recordsPerBlock int
		want            int
	}{
		{
			name:            "when an index of one leaf is costed, then a search reads the root and the leaf",
			numBlocks:       1,
			recordsPerBlock: 10,
			want:            2,
		},
		{
			name:            "when an index of as many leaves as a root has children is costed, then a search reads the root and a leaf",
			numBlocks:       10,
			recordsPerBlock: 10,
			want:            2,
		},
		{
			name:            "when an index of one leaf more than a root has children is costed, then a search reads two directory levels and a leaf",
			numBlocks:       11,
			recordsPerBlock: 10,
			want:            3,
		},
		{
			name:            "when an index of the square of how many children a directory block has is costed, then a search reads two directory levels and a leaf",
			numBlocks:       100,
			recordsPerBlock: 10,
			want:            3,
		},
		// At an exact power, a floating-point logarithm can fall slightly above
		// or below the integer. These two cases check that the cost changes at
		// exactly 1000 leaves, not one leaf earlier or later.
		{
			name:            "when an index of the cube of how many children a directory block has is costed, then a search reads three directory levels and a leaf",
			numBlocks:       1000,
			recordsPerBlock: 10,
			want:            4,
		},
		{
			name:            "when an index of one leaf more than the cube of how many children a directory block has is costed, then a search reads four directory levels and a leaf",
			numBlocks:       1001,
			recordsPerBlock: 10,
			want:            5,
		},
		// An empty index still has its root and one leaf. A search reads both
		// to find that the index holds nothing.
		{
			name:            "when an index of no blocks is costed, then a search reads the root and the leaf",
			numBlocks:       0,
			recordsPerBlock: 10,
			want:            2,
		},
		// A directory block with one record cannot branch, so its fan-out is
		// taken as 2, the minimum that can form a tree. 5 leaves then need
		// three levels.
		{
			name:            "when an index of 5 leaves holding one record per block is costed, then a search reads three directory levels and a leaf, as if each block held two",
			numBlocks:       5,
			recordsPerBlock: 1,
			want:            4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BTreeIndexSearchCost(tt.numBlocks, tt.recordsPerBlock); got != tt.want {
				t.Errorf("BTreeIndexSearchCost(%d, %d) = %d, want %d", tt.numBlocks, tt.recordsPerBlock, got, tt.want)
			}
		})
	}
}

// assertEachValueFinds checks that a search for each value in entries returns
// exactly the record ids of the entries for that value, in any order.
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

// newTestBTreeIndex opens a B-tree index on an int field, in a database of
// its own.
func newTestBTreeIndex(t *testing.T) *BTreeIndex {
	t.Helper()

	idx, err := NewBTreeIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
	if err != nil {
		t.Fatalf("NewBTreeIndex() error = %v", err)
	}

	return idx
}

// assertNewBTreeIndexFiles checks the files of the index named testIndexName
// against the state of a new index. The leaf file must hold one empty leaf
// with no overflow block. The directory file must hold one root at level 0
// whose only entry is wantRoot.
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

// assertFileSize checks that fileName has want blocks, and reports whether it
// does.
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
