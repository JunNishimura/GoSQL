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
