package index

import (
	"errors"
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

// A leaf whose entries are all filed under one value cannot be split without
// parting two of them, so a 24th entry for that value keeps the leaf's first
// entry, the one just inserted, and moves the rest into an overflow block put
// at the head of the leaf's chain.
func TestBTreeLeafInsertOverflow(t *testing.T) {
	tests := []struct {
		name string
		// chain is the leaf the entry goes into, then the blocks it already
		// overflows into.
		chain [][]entry
		// wantFlag is the block the leaf overflows into afterwards, the one
		// just appended.
		wantFlag int32
		// wantOverflowFlag is the block the appended one overflows into: the
		// one the leaf overflowed into before, or none.
		wantOverflowFlag int32
	}{
		{
			name:             "given a leaf of 23 entries for 20 with no overflow, when another entry for 20 fills it, then the leaf keeps the new entry and overflows into a new block holding the other 23, which overflows into none",
			chain:            [][]entry{entriesFor(20, 0, 23)},
			wantFlag:         1,
			wantOverflowFlag: -1,
		},
		{
			name:             "given a leaf of 23 entries for 20 that overflows into a block of 2 more, when another entry for 20 fills it, then the leaf keeps the new entry and overflows into a new block holding the other 23, which overflows into the old one",
			chain:            [][]entry{entriesFor(20, 0, 23), entriesFor(20, 2, 2)},
			wantFlag:         2,
			wantOverflowFlag: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafChain(t, tx, layout, tt.chain)

			searchKey := query.NewIntConstant(20)
			inserted := recordmanager.NewRecordID(1, 0)

			leaf, err := newBTreeLeaf(tx, blk, layout, searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", searchKey, err)
			}
			got, err := leaf.insert(inserted)
			leaf.close()
			if err != nil {
				t.Fatalf("insert(%s) error = %v", inserted, err)
			}
			if got != nil {
				t.Errorf("insert(%s) = %+v, want nil", inserted, got)
			}

			assertHeader(t, tx, blk, layout, tt.wantFlag, 1)
			overflowBlk := filemanager.NewBlockId(testBTreeFile, int(tt.wantFlag))
			assertHeader(t, tx, overflowBlk, layout, tt.wantOverflowFlag, 23)

			// Every entry is still found, the new one first, then the leaf's
			// old ones, then those of the blocks it already overflowed into.
			want := []string{inserted.String()}
			for _, entries := range tt.chain {
				for _, e := range entries {
					want = append(want, e.rid.String())
				}
			}

			leaf, err = newBTreeLeaf(tx, blk, layout, searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", searchKey, err)
			}
			defer leaf.close()

			if got := leafRecordIDs(t, leaf); !slices.Equal(got, want) {
				t.Errorf("record ids filed under %s = %v, want %v", searchKey, got, want)
			}
		})
	}
}

// The entries a leaf overflows into are all for the value of its first entry,
// so another value inserted there has to go somewhere the chain does not cover.
func TestBTreeLeafInsertIntoOverflowing(t *testing.T) {
	t.Run("given a leaf of 2 entries for 20 that overflows into a block of 2 more, when an entry for 10 is inserted, then the leaf keeps only it, and the entries for 20 move with their chain to a new block that comes back filed under 20", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)
		blk := newTestLeafChain(t, tx, layout, [][]entry{entriesFor(20, 0, 2), entriesFor(20, 2, 2)})

		inserted := recordmanager.NewRecordID(1, 0)
		leaf, err := newBTreeLeaf(tx, blk, layout, query.NewIntConstant(10))
		if err != nil {
			t.Fatalf("newBTreeLeaf(10) error = %v", err)
		}
		got, err := leaf.insert(inserted)
		leaf.close()
		if err != nil {
			t.Fatalf("insert(%s) error = %v", inserted, err)
		}

		wantEntry := dirEntry{value: query.NewIntConstant(20), childBlockNumber: 2}
		if got == nil || *got != wantEntry {
			t.Errorf("insert(%s) = %+v, want %+v", inserted, got, wantEntry)
		}

		assertHeader(t, tx, blk, layout, -1, 1)
		newBlk := filemanager.NewBlockId(testBTreeFile, 2)
		assertHeader(t, tx, newBlk, layout, 1, 2)

		assertLeafRecordIDs(t, tx, blk, layout, query.NewIntConstant(10), []string{inserted.String()})
		assertLeafRecordIDs(t, tx, newBlk, layout, query.NewIntConstant(20), []string{
			recordmanager.NewRecordID(0, 0).String(),
			recordmanager.NewRecordID(0, 1).String(),
			recordmanager.NewRecordID(2, 0).String(),
			recordmanager.NewRecordID(2, 1).String(),
		})
	})

	t.Run("given a leaf of 2 entries for 20 that overflows into a block of 2 more, when an entry for 25 is inserted, then nothing comes back for the directory, 20 still reads all four entries of its chain, and 25 reads its own", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestIndexLayout(t)
		blk := newTestLeafChain(t, tx, layout, [][]entry{entriesFor(20, 0, 2), entriesFor(20, 2, 2)})

		inserted := recordmanager.NewRecordID(1, 0)
		leaf, err := newBTreeLeaf(tx, blk, layout, query.NewIntConstant(25))
		if err != nil {
			t.Fatalf("newBTreeLeaf(25) error = %v", err)
		}
		got, err := leaf.insert(inserted)
		leaf.close()
		if err != nil {
			t.Fatalf("insert(%s) error = %v", inserted, err)
		}
		if got != nil {
			t.Errorf("insert(%s) = %+v, want nil", inserted, got)
		}

		assertLeafRecordIDs(t, tx, blk, layout, query.NewIntConstant(20), []string{
			recordmanager.NewRecordID(0, 0).String(),
			recordmanager.NewRecordID(0, 1).String(),
			recordmanager.NewRecordID(2, 0).String(),
			recordmanager.NewRecordID(2, 1).String(),
		})
		assertLeafRecordIDs(t, tx, blk, layout, query.NewIntConstant(25), []string{inserted.String()})
	})
}

func TestBTreeLeafDelete(t *testing.T) {
	entries := []entry{
		{value: query.NewIntConstant(10), rid: recordmanager.NewRecordID(0, 1)},
		{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 2)},
		{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 3)},
		{value: query.NewIntConstant(30), rid: recordmanager.NewRecordID(0, 4)},
	}
	all := []string{
		"10 " + recordmanager.NewRecordID(0, 1).String(),
		"20 " + recordmanager.NewRecordID(0, 2).String(),
		"20 " + recordmanager.NewRecordID(0, 3).String(),
		"30 " + recordmanager.NewRecordID(0, 4).String(),
	}

	tests := []struct {
		name    string
		deleted entry
		want    []string
		wantErr error
	}{
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when the second entry for 20 is deleted, then the other three are left",
			deleted: entries[2],
			want:    []string{all[0], all[1], all[3]},
		},
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when the first entry for 20 is deleted, then the other three are left",
			deleted: entries[1],
			want:    []string{all[0], all[2], all[3]},
		},
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when the entry for 10 is deleted, then the other three are left",
			deleted: entries[0],
			want:    []string{all[1], all[2], all[3]},
		},
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when an entry for 20 pointing to a record no entry points to is deleted, then it reports ErrEntryNotFound and all four are left",
			deleted: entry{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(9, 9)},
			want:    all,
			wantErr: ErrEntryNotFound,
		},
		// The record id is one an entry points to, but filed under 10, so an
		// entry for 20 with it is not there.
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when an entry for 20 pointing to the record the entry for 10 points to is deleted, then it reports ErrEntryNotFound and all four are left",
			deleted: entry{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)},
			want:    all,
			wantErr: ErrEntryNotFound,
		},
		{
			name:    "given a leaf holding entries for 10, 20, 20 and 30, when an entry for 25, which none is filed under, is deleted, then it reports ErrEntryNotFound and all four are left",
			deleted: entry{value: query.NewIntConstant(25), rid: recordmanager.NewRecordID(0, 2)},
			want:    all,
			wantErr: ErrEntryNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafBlock(t, tx, layout, entries)

			leaf, err := newBTreeLeaf(tx, blk, layout, tt.deleted.value)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", tt.deleted.value, err)
			}
			err = leaf.delete(tt.deleted.rid)
			leaf.close()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("delete(%s) error = %v, want %v", tt.deleted.rid, err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("delete(%s) error = %v", tt.deleted.rid, err)
			}

			page, err := newBTreePage(tx, blk, layout)
			if err != nil {
				t.Fatalf("newBTreePage() error = %v", err)
			}
			defer page.close()

			if got := leafEntries(t, page); !slices.Equal(got, tt.want) {
				t.Errorf("entries = %v, want %v", got, tt.want)
			}
		})
	}
}

// An overflow block left with no entries is taken out of its chain, so that no
// block in a chain is empty and every one of them says by its first entry
// which value it holds.
func TestBTreeLeafDeleteFromOverflow(t *testing.T) {
	tests := []struct {
		name string
		// chain is the leaf, then the blocks it overflows into, in order.
		chain   [][]entry
		deleted *recordmanager.RecordID
		// wantFlags is the flag of each block of chain afterwards. Those of a
		// block taken out of the chain are not checked, and are left as -2.
		wantFlags []int32
		want      []string
	}{
		{
			name:      "given a leaf of 2 entries for 20 that overflows into a block of 2 more, when one in the overflow block is deleted, then the chain is left as it was and the other three are read",
			chain:     [][]entry{entriesFor(20, 0, 2), entriesFor(20, 1, 2)},
			deleted:   recordmanager.NewRecordID(1, 0),
			wantFlags: []int32{1, -1},
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(1, 1).String(),
			},
		},
		{
			name:      "given a leaf of entries for 20 that overflows into a block of one more, which overflows into a block of 2 more, when the one in the middle block is deleted, then the leaf overflows into the last block and the other four are read",
			chain:     [][]entry{entriesFor(20, 0, 2), entriesFor(20, 1, 1), entriesFor(20, 2, 2)},
			deleted:   recordmanager.NewRecordID(1, 0),
			wantFlags: []int32{2, -2, -1},
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(2, 0).String(),
				recordmanager.NewRecordID(2, 1).String(),
			},
		},
		{
			name:      "given a leaf of entries for 20 that overflows into a block of one more, which overflows into a block of one more, when the one in the last block is deleted, then the middle block overflows into none and the other three are read",
			chain:     [][]entry{entriesFor(20, 0, 2), entriesFor(20, 1, 1), entriesFor(20, 2, 1)},
			deleted:   recordmanager.NewRecordID(2, 0),
			wantFlags: []int32{1, -1, -2},
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(1, 0).String(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafChain(t, tx, layout, tt.chain)
			searchKey := query.NewIntConstant(20)

			leaf, err := newBTreeLeaf(tx, blk, layout, searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", searchKey, err)
			}
			err = leaf.delete(tt.deleted)
			leaf.close()
			if err != nil {
				t.Fatalf("delete(%s) error = %v", tt.deleted, err)
			}

			for i, wantFlag := range tt.wantFlags {
				if wantFlag == -2 {
					continue
				}
				assertFlag(t, tx, filemanager.NewBlockId(testBTreeFile, i), layout, wantFlag)
			}

			assertLeafRecordIDs(t, tx, blk, layout, searchKey, tt.want)
		})
	}
}

// A chain is found by the value of its leaf's first entry, so a delete that
// takes the last entry for that value out of the leaf brings one back from the
// block the leaf overflows into. Which one comes back is left to the leaf, so
// the record ids read are compared without their order.
func TestBTreeLeafDeleteFromOverflowingLeaf(t *testing.T) {
	tests := []struct {
		name string
		// leaf is the leaf's own entries, and overflow the blocks it overflows
		// into, in order.
		leaf     []entry
		overflow [][]entry
		deleted  *recordmanager.RecordID
		wantFlag int32
		// want is what reading 20 from the leaf comes to afterwards, and
		// want25 what reading 25 does.
		want   []string
		want25 []string
	}{
		{
			name:     "given a leaf holding an entry for 20 and one for 25 that overflows under 20 into a block of 2 more, when the leaf's entry for 20 is deleted, then one comes back from the overflow block, which still holds the other, and both values are read",
			leaf:     []entry{{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 0)}, {value: query.NewIntConstant(25), rid: recordmanager.NewRecordID(0, 1)}},
			overflow: [][]entry{entriesFor(20, 1, 2)},
			deleted:  recordmanager.NewRecordID(0, 0),
			wantFlag: 1,
			want: []string{
				recordmanager.NewRecordID(1, 0).String(),
				recordmanager.NewRecordID(1, 1).String(),
			},
			want25: []string{
				recordmanager.NewRecordID(0, 1).String(),
			},
		},
		{
			name:     "given a leaf of one entry for 20 that overflows into a block of one more, when the leaf's entry is deleted, then the other comes back to the leaf, which overflows into none",
			leaf:     entriesFor(20, 0, 1),
			overflow: [][]entry{entriesFor(20, 1, 1)},
			deleted:  recordmanager.NewRecordID(0, 0),
			wantFlag: -1,
			want: []string{
				recordmanager.NewRecordID(1, 0).String(),
			},
			want25: []string{},
		},
		{
			name:     "given a leaf of one entry for 20 that overflows into a block of one more, which overflows into a block of one more, when the leaf's entry is deleted, then the middle one comes back to the leaf, which overflows into the last block",
			leaf:     entriesFor(20, 0, 1),
			overflow: [][]entry{entriesFor(20, 1, 1), entriesFor(20, 2, 1)},
			deleted:  recordmanager.NewRecordID(0, 0),
			wantFlag: 2,
			want: []string{
				recordmanager.NewRecordID(1, 0).String(),
				recordmanager.NewRecordID(2, 0).String(),
			},
			want25: []string{},
		},
		{
			name:     "given a leaf holding two entries for 20 and one for 25 that overflows under 20 into a block of one more, when the leaf's first entry for 20 is deleted, then the leaf still starts with 20, its chain is left as it was, and both values are read",
			leaf:     []entry{{value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 0)}, {value: query.NewIntConstant(20), rid: recordmanager.NewRecordID(0, 1)}, {value: query.NewIntConstant(25), rid: recordmanager.NewRecordID(0, 2)}},
			overflow: [][]entry{entriesFor(20, 1, 1)},
			deleted:  recordmanager.NewRecordID(0, 0),
			wantFlag: 1,
			want: []string{
				recordmanager.NewRecordID(0, 1).String(),
				recordmanager.NewRecordID(1, 0).String(),
			},
			want25: []string{
				recordmanager.NewRecordID(0, 2).String(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			layout := newTestIndexLayout(t)
			blk := newTestLeafChain(t, tx, layout, append([][]entry{tt.leaf}, tt.overflow...))
			searchKey := query.NewIntConstant(20)

			leaf, err := newBTreeLeaf(tx, blk, layout, searchKey)
			if err != nil {
				t.Fatalf("newBTreeLeaf(%s) error = %v", searchKey, err)
			}
			err = leaf.delete(tt.deleted)
			leaf.close()
			if err != nil {
				t.Fatalf("delete(%s) error = %v", tt.deleted, err)
			}

			assertFlag(t, tx, blk, layout, tt.wantFlag)
			assertLeafRecordIDsInAnyOrder(t, tx, blk, layout, searchKey, tt.want)
			assertLeafRecordIDsInAnyOrder(t, tx, blk, layout, query.NewIntConstant(25), tt.want25)
		})
	}
}

// assertLeafRecordIDsInAnyOrder opens the leaf in blk for searchKey and checks
// the record ids it reads, whatever order they come in.
func assertLeafRecordIDsInAnyOrder(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	searchKey query.Constant,
	want []string,
) {
	t.Helper()

	leaf, err := newBTreeLeaf(tx, blk, layout, searchKey)
	if err != nil {
		t.Fatalf("newBTreeLeaf(%v, %s) error = %v", blk, searchKey, err)
	}
	defer leaf.close()

	got := leafRecordIDs(t, leaf)
	slices.Sort(got)
	sortedWant := slices.Sorted(slices.Values(want))
	if !slices.Equal(got, sortedWant) {
		t.Errorf("record ids filed under %s from %v = %v, want %v in any order", searchKey, blk, got, sortedWant)
	}
}

// assertFlag checks the flag in the header of blk.
func assertFlag(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	want int32,
) {
	t.Helper()

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		t.Fatalf("newBTreePage(%v) error = %v", blk, err)
	}
	defer page.close()

	got, err := page.flag()
	if err != nil {
		t.Fatalf("flag() error = %v", err)
	}
	if got != want {
		t.Errorf("flag() of %v = %d, want %d", blk, got, want)
	}
}

// assertLeafRecordIDs opens the leaf in blk for searchKey and checks the
// record ids it reads.
func assertLeafRecordIDs(
	t *testing.T,
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	searchKey query.Constant,
	want []string,
) {
	t.Helper()

	leaf, err := newBTreeLeaf(tx, blk, layout, searchKey)
	if err != nil {
		t.Fatalf("newBTreeLeaf(%v, %s) error = %v", blk, searchKey, err)
	}
	defer leaf.close()

	if got := leafRecordIDs(t, leaf); !slices.Equal(got, want) {
		t.Errorf("record ids filed under %s from %v = %v, want %v", searchKey, blk, got, want)
	}
}

// entriesFor is n entries filed under v, pointing to slots 0 to n-1 of block
// blkNum of the indexed table.
func entriesFor(v int32, blkNum, n int) []entry {
	entries := make([]entry, n)
	for i := range entries {
		entries[i] = entry{value: query.NewIntConstant(v), rid: recordmanager.NewRecordID(blkNum, i)}
	}

	return entries
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
