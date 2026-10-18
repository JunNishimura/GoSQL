package index

import (
	"slices"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestBTreeLeafNext(t *testing.T) {
	entries := []entry{
		{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
		{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
		{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 3)},
		{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 4)},
	}

	tests := []struct {
		name      string
		existing  []entry
		searchKey query.Constant
		want      []string
	}{
		{
			name:      "given a leaf holding entries for 10, 20, 20 and 30, when it is read for 20, then both record ids filed under 20 come back in slot order",
			existing:  entries,
			searchKey: query.NewIntConstant(20),
			want: []string{
				recordmanager.NewRecordID(0, 2).String(),
				recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:      "given a leaf holding entries for 10, 20, 20 and 30, when it is read for 10, the value of the first, then its record id comes back",
			existing:  entries,
			searchKey: query.NewIntConstant(10),
			want: []string{
				recordmanager.NewRecordID(0, 1).String(),
			},
		},
		{
			name:      "given a leaf holding entries for 10, 20, 20 and 30, when it is read for 30, the value of the last, then its record id comes back",
			existing:  entries,
			searchKey: query.NewIntConstant(30),
			want: []string{
				recordmanager.NewRecordID(0, 4).String(),
			},
		},
		{
			name:      "given a leaf holding entries for 10, 20, 20 and 30, when it is read for 25, which none is filed under, then nothing comes back",
			existing:  entries,
			searchKey: query.NewIntConstant(25),
			want:      []string{},
		},
		{
			name:      "given a leaf holding entries for 10, 20, 20 and 30, when it is read for 40, which is greater than all of them, then nothing comes back",
			existing:  entries,
			searchKey: query.NewIntConstant(40),
			want:      []string{},
		},
		{
			name:      "given an empty leaf, when it is read for 10, then nothing comes back",
			searchKey: query.NewIntConstant(10),
			want:      []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafBlock(t, tx, layout, tt.existing)

			leaf, err := newBTreeLeaf(tx, blk, layout, tt.searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", tt.searchKey, err)
			}
			defer leaf.close()

			if got := leafRecordIDs(t, leaf); !slices.Equal(got, tt.want) {
				t.Errorf("record ids filed under %s = %v, want %v", tt.searchKey, got, tt.want)
			}
		})
	}

	// Every block of an overflow chain holds entries for one value only, which
	// is the value of the first entry of the leaf the chain starts from.
	chainTests := []struct {
		name      string
		chain     [][]entry
		searchKey query.Constant
		want      []string
	}{
		{
			name: "given a leaf of entries for 20 that overflows into one block, when it is read for 20, then the record ids of both blocks come back, the leaf's first",
			chain: [][]entry{
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)},
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
				},
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 3)},
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 4)},
				},
			},
			searchKey: query.NewIntConstant(20),
			want: []string{
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(0, 2).String(),
				recordmanager.NewRecordID(0, 3).String(),
				recordmanager.NewRecordID(0, 4).String(),
			},
		},
		{
			name: "given a leaf of entries for 20 that overflows into a block that overflows into another, when it is read for 20, then the record ids of all three blocks come back in chain order",
			chain: [][]entry{
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)},
				},
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
				},
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 3)},
				},
			},
			searchKey: query.NewIntConstant(20),
			want: []string{
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(0, 2).String(),
				recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name: "given a leaf of entries for 20 that overflows into one block, when it is read for 10, then nothing comes back",
			chain: [][]entry{
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)},
				},
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
				},
			},
			searchKey: query.NewIntConstant(10),
			want:      []string{},
		},
		{
			name: "given a leaf of entries for 20 that overflows into one block, when it is read for 30, then nothing comes back",
			chain: [][]entry{
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)},
				},
				{
					{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
				},
			},
			searchKey: query.NewIntConstant(30),
			want:      []string{},
		},
	}
	for _, tt := range chainTests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafChain(t, tx, layout, tt.chain)

			leaf, err := newBTreeLeaf(tx, blk, layout, tt.searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", tt.searchKey, err)
			}
			defer leaf.close()

			if got := leafRecordIDs(t, leaf); !slices.Equal(got, tt.want) {
				t.Errorf("record ids filed under %s = %v, want %v", tt.searchKey, got, tt.want)
			}
		})
	}
}

func TestBTreeLeafInsert(t *testing.T) {
	entries := []entry{
		{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
		{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
		{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 3)},
	}

	tests := []struct {
		name     string
		existing []entry
		inserted entry
		want     []string
	}{
		{
			name:     "given an empty leaf, when an entry for 10 is inserted, then the leaf holds it and nothing comes back for the directory",
			inserted: entry{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(1, 1)},
			want: []string{
				"10 " + recordmanager.NewRecordID(1, 1).String(),
			},
		},
		{
			name:     "given a leaf holding entries for 10, 20 and 30, when an entry for 15 is inserted, then it goes between 10 and 20 and nothing comes back for the directory",
			existing: entries,
			inserted: entry{value: query.NewIntConstant(15), rid: recordmanager.NewRecordID(1, 5)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"15 " + recordmanager.NewRecordID(1, 5).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:     "given a leaf holding entries for 10, 20 and 30, when an entry for 5 is inserted, then it goes first and nothing comes back for the directory",
			existing: entries,
			inserted: entry{value: query.NewIntConstant(5), rid: recordmanager.NewRecordID(1, 5)},
			want: []string{
				"5 " + recordmanager.NewRecordID(1, 5).String(),
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
		{
			name:     "given a leaf holding entries for 10, 20 and 30, when an entry for 40 is inserted, then it goes last and nothing comes back for the directory",
			existing: entries,
			inserted: entry{value: query.NewIntConstant(40), rid: recordmanager.NewRecordID(1, 5)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
				"40 " + recordmanager.NewRecordID(1, 5).String(),
			},
		},
		{
			name:     "given a leaf holding entries for 10, 20 and 30, when another entry for 20 is inserted, then it goes ahead of the one already there and nothing comes back for the directory",
			existing: entries,
			inserted: entry{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(1, 5)},
			want: []string{
				"10 " + recordmanager.NewRecordID(0, 1).String(),
				"20 " + recordmanager.NewRecordID(1, 5).String(),
				"20 " + recordmanager.NewRecordID(0, 2).String(),
				"30 " + recordmanager.NewRecordID(0, 3).String(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafBlock(t, tx, layout, tt.existing)

			leaf, err := newBTreeLeaf(tx, blk, layout, tt.inserted.value)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", tt.inserted.value, err)
			}

			got, err := leaf.insert(tt.inserted.rid)
			leaf.close()
			if err != nil {
				t.Fatalf("insert(%s) error = %v", tt.inserted.rid, err)
			}
			if got != nil {
				t.Errorf("insert(%s) = %+v, want nil", tt.inserted.rid, got)
			}

			page, err := newBTreePage(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreePage() error = %v", err)
			}
			defer page.close()

			if entries := leafEntries(t, page); !slices.Equal(entries, tt.want) {
				t.Errorf("entries = %v, want %v", entries, tt.want)
			}
		})
	}
}

// A leaf on an int field holds 24 entries, so each case here fills one with 23
// and inserts a 24th, the greatest, which fills it and splits it. The split
// starts from the middle, slot 12, and is moved so that it does not part two
// entries filed under the same value.
func TestBTreeLeafInsertSplit(t *testing.T) {
	tests := []struct {
		name      string
		values    []int32
		wantEntry dirEntry
		wantLeft  []int32
		wantNew   []int32
	}{
		{
			name:      "given a leaf of 23 entries for 0 to 22, when an entry for 23 fills it, then it keeps 0 to 11, a new block takes 12 to 23, and the new block comes back filed under 12",
			values:    seq(0, 23),
			wantEntry: dirEntry{value: query.NewIntConstant(12), childBlockNumber: 1},
			wantLeft:  seq(0, 11),
			wantNew:   seq(12, 23),
		},
		{
			name:      "given a leaf one entry short of full, when the entry that fills it leaves the middle slot holding the second of three entries for 50, then all three go to the new block, which comes back filed under 50",
			values:    concat(seq(0, 10), rep(50, 3), seq(60, 69)),
			wantEntry: dirEntry{value: query.NewIntConstant(50), childBlockNumber: 1},
			wantLeft:  seq(0, 10),
			wantNew:   concat(rep(50, 3), seq(60, 69)),
		},
		{
			name:      "given a leaf one entry short of full whose first 13 entries are all for 5, when the entry that fills it is inserted, then the 5s all stay, and the new block takes the rest and comes back filed under the first of them",
			values:    concat(rep(5, 13), seq(10, 20)),
			wantEntry: dirEntry{value: query.NewIntConstant(10), childBlockNumber: 1},
			wantLeft:  rep(5, 13),
			wantNew:   seq(10, 20),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)

			existing := make([]entry, 0, len(tt.values)-1)
			for i, v := range tt.values[:len(tt.values)-1] {
				existing = append(existing, entry{value: query.NewIntConstant(v), rid: recordmanager.NewRecordID(0, i)})
			}
			blk := newTestLeafBlock(t, tx, layout, existing)

			last := query.NewIntConstant(tt.values[len(tt.values)-1])
			leaf, err := newBTreeLeaf(tx, blk, layout, last)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", last, err)
			}

			got, err := leaf.insert(recordmanager.NewRecordID(1, 0))
			leaf.close()
			if err != nil {
				t.Fatalf("insert() error = %v", err)
			}
			if got == nil || *got != tt.wantEntry {
				t.Errorf("insert() = %+v, want %+v", got, tt.wantEntry)
			}

			if got := leafValues(t, tx, blk, layout); !slices.Equal(got, tt.wantLeft) {
				t.Errorf("values left in %v = %v, want %v", blk, got, tt.wantLeft)
			}
			newBlk := filemanager.NewBlockId(testBTreeFile, 1)
			if got := leafValues(t, tx, newBlk, layout); !slices.Equal(got, tt.wantNew) {
				t.Errorf("values moved to %v = %v, want %v", newBlk, got, tt.wantNew)
			}

			assertHeader(t, tx, blk, layout, -1, len(tt.wantLeft))
		})
	}
}

// leafValues reads the values of every entry in the leaf block blk, in slot
// order, or none if the file has no such block.
func leafValues(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
) []int32 {
	t.Helper()

	size, err := tx.Size(blk.FileName())
	if err != nil {
		t.Fatalf("Size(%q) error = %v", blk.FileName(), err)
	}
	if blk.Number() >= size {
		return []int32{}
	}

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage(%v) error = %v", blk, err)
	}
	defer page.close()

	n, err := page.numRecords()
	if err != nil {
		t.Fatalf("numRecords() error = %v", err)
	}

	got := make([]int32, 0, n)
	for slot := range n {
		v, err := page.dataValue(slot)
		if err != nil {
			t.Fatalf("dataValue(%d) error = %v", slot, err)
		}
		i, err := v.AsInt()
		if err != nil {
			t.Fatalf("AsInt() error = %v", err)
		}
		got = append(got, i)
	}

	return got
}

// seq is the ints from from to to, both included.
func seq(from, to int32) []int32 {
	s := make([]int32, 0, to-from+1)
	for v := from; v <= to; v++ {
		s = append(s, v)
	}

	return s
}

// rep is v, n times over.
func rep(v int32, n int) []int32 {
	s := make([]int32, n)
	for i := range s {
		s[i] = v
	}

	return s
}

// concat is the slices one after another.
func concat(parts ...[]int32) []int32 {
	var s []int32
	for _, part := range parts {
		s = append(s, part...)
	}

	return s
}

// newTestLeafChain appends one leaf block per element of chain to
// testBTreeFile, each flagged to overflow into the next and the last into
// none, and returns the first.
func newTestLeafChain(
	t *testing.T,
	tx *transaction.Transaction,
	layout *recordmanager.Layout,
	chain [][]entry,
) *filemanager.BlockId {
	t.Helper()

	blks := make([]*filemanager.BlockId, len(chain))
	for i, entries := range chain {
		blks[i] = newTestLeafBlock(t, tx, layout, entries)
	}

	for i := range len(blks) - 1 {
		page, err := newBTreePage(tx, blks[i], layout)
		if err != nil {
			t.Fatalf("newBTreePage(%v) error = %v", blks[i], err)
		}
		if err := page.setFlag(int32(blks[i+1].Number())); err != nil {
			t.Fatalf("setFlag(%d) error = %v", blks[i+1].Number(), err)
		}
		page.close()
	}

	return blks[0]
}

// newTestLeafBlock appends a leaf block with no overflow to testBTreeFile and
// fills it with entries, in the order given, which the caller keeps sorted.
func newTestLeafBlock(
	t *testing.T,
	tx *transaction.Transaction,
	layout *recordmanager.Layout,
	entries []entry,
) *filemanager.BlockId {
	t.Helper()

	blk, err := appendBTreeBlock(tx, testBTreeFile, layout, -1)
	if err != nil {
		t.Fatalf("appendBTreeBlock() error = %v", err)
	}
	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage() error = %v", err)
	}
	defer page.close()

	for i, e := range entries {
		if err := page.insertLeaf(i, e.value, e.rid); err != nil {
			t.Fatalf("insertLeaf(%d, %s, %s) error = %v", i, e.value, e.rid, err)
		}
	}

	return blk
}

// leafRecordIDs moves leaf forward until it runs out of entries for its search
// key and returns the record ids it came across, written out so that they
// compare with ==.
func leafRecordIDs(t *testing.T, leaf *btreeLeaf) []string {
	t.Helper()

	got := []string{}
	for {
		ok, err := leaf.next()
		if err != nil {
			t.Fatalf("next() error = %v", err)
		}
		if !ok {
			return got
		}

		rid, err := leaf.dataRecordID()
		if err != nil {
			t.Fatalf("dataRecordID() error = %v", err)
		}
		got = append(got, rid.String())
	}
}
