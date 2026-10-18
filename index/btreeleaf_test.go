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
