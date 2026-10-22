package metadatamanager

import (
	"errors"
	"testing"

	"github.com/JunNishimura/GoSQL/index"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The index the tests below describe: one on the int field of the test table.
// The name is short enough for the catalogs to hold, as any real one has to be.
const (
	testIndexName      = "id_index"
	testIndexFieldName = "id"
)

// The widths an index record is built from. Its first two fields name the
// record it points at, and the third holds the value it is filed under.
//
//	an index on an int field       4 + 4 + 4 + 4        = 16 bytes
//	an index on a varchar(20)      4 + 4 + 4 + (4 + 80) = 96 bytes
const (
	intIndexSlotSize     = 16
	varcharIndexSlotSize = 96
)

func TestNewIndexInfo(t *testing.T) {
	t.Run("it carries the transaction, the index name, the field it is on, the schema of the table it indexes and what is known about that table", func(t *testing.T) {
		tx := newTestTransaction(t)
		tableSchema := newTestSchema(t)
		tableStatistics := NewTableStatistics(7, 100)

		info, err := NewIndexInfo(tx, testIndexName, testIndexFieldName, tableSchema, tableStatistics)
		if err != nil {
			t.Fatalf("NewIndexInfo() error = %v", err)
		}

		if info.tx != tx {
			t.Errorf("tx = %p, want %p", info.tx, tx)
		}
		if info.indexName != testIndexName {
			t.Errorf("indexName = %q, want %q", info.indexName, testIndexName)
		}
		if info.fieldName != testIndexFieldName {
			t.Errorf("fieldName = %q, want %q", info.fieldName, testIndexFieldName)
		}
		if info.tableSchema != tableSchema {
			t.Errorf("tableSchema = %p, want %p", info.tableSchema, tableSchema)
		}
		if info.tableStatistics != tableStatistics {
			t.Errorf("tableStatistics = %p, want %p", info.tableStatistics, tableStatistics)
		}
		if info.indexLayout == nil {
			t.Error("indexLayout = nil, want the layout of the index's own records")
		}
	})

	// An index on a field the table does not have is refused where it is made,
	// rather than at the first call that needs to know how wide the field is.
	// There is nothing such an index could be: it has no records to hold, and
	// no shape for the records it does not hold.
	t.Run("given a field the indexed table does not have, when an index on it is described, then it reports ErrFieldNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		tableSchema := newTestSchema(t)

		_, err := NewIndexInfo(tx, testIndexName, "missing", tableSchema, NewTableStatistics(7, 100))

		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}
	})
}

// An index record names the record it points at and the value it is filed
// under, so the first two fields are a record id taken apart and the third is a
// copy of the indexed field.
//
// The third takes its type and width from the table's schema rather than being
// fixed, since what is filed is whatever the indexed field holds. That is the
// whole reason this is worked out per index rather than written down once.
func TestCreateIndexLayout(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      layoutDescription
	}{
		{
			name:      "given an index on an int field, when its records are laid out, then the value it is filed under is an int",
			fieldName: "id",
			want: layoutDescription{
				slotSize: intIndexSlotSize,
				fields: []layoutField{
					{
						name:      index.BlockNumberField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    4,
					},
					{
						name:      index.SlotField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    8,
					},
					{
						name:      index.IndexedValueField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    12,
					},
				},
			},
		},
		{
			name:      "given an index on a varchar field, when its records are laid out, then the value it is filed under is a varchar as wide as that field",
			fieldName: "name",
			want: layoutDescription{
				slotSize: varcharIndexSlotSize,
				fields: []layoutField{
					{
						name:      index.BlockNumberField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    4,
					},
					{
						name:      index.SlotField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    8,
					},
					{
						name:      index.IndexedValueField,
						fieldType: recordmanager.FieldTypeVarchar,
						length:    testStringFieldLength,
						offset:    12,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout, err := createIndexLayout(newTestSchema(t), tt.fieldName)
			if err != nil {
				t.Fatalf("createIndexLayout() error = %v", err)
			}

			assertLayout(t, layout, tt.want)
		})
	}

	t.Run("given a field the table's schema does not have, when an index on it is laid out, then it reports ErrFieldNotFound", func(t *testing.T) {
		if _, err := createIndexLayout(newTestSchema(t), "missing"); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}
	})
}

// An index on a field wide enough that its own records do not fit in a block
// is refused where it is described.
//
//	an index on a varchar(200)  4 + 4 + 4 + (4 + 800) = 816 bytes
//
// A record page would refuse the same layout, so such an index could never be
// opened. What makes refusing it here worth doing is that the cost of a search
// through it is asked before it is opened, and working that out divides by how
// many of its records go in a block, which for this one is none.
func TestNewIndexInfoRejectsAnIndexWiderThanABlock(t *testing.T) {
	t.Run("given a field whose index records would not fit in a block, when an index on it is described, then it reports ErrSlotWiderThanBlock", func(t *testing.T) {
		tableSchema := recordmanager.NewSchema()
		mustAddStringField(t, tableSchema, "essay", 200)

		_, err := NewIndexInfo(
			newTestTransaction(t),
			testIndexName,
			"essay",
			tableSchema,
			NewTableStatistics(7, testIndexedTableRecords),
		)

		if !errors.Is(err, recordmanager.ErrSlotWiderThanBlock) {
			t.Errorf("error = %v, want %v", err, recordmanager.ErrSlotWiderThanBlock)
		}
	})
}

// newTestIndexInfo describes an index on fieldName of the test table, over a
// table said to hold testIndexedTableRecords records.
func newTestIndexInfo(t *testing.T, fieldName string) *IndexInfo {
	t.Helper()

	info, err := NewIndexInfo(
		newTestTransaction(t),
		testIndexName,
		fieldName,
		newTestSchema(t),
		NewTableStatistics(7, testIndexedTableRecords),
	)
	if err != nil {
		t.Fatalf("NewIndexInfo() error = %v", err)
	}

	return info
}

// What the indexed table is said to hold, and what the guess at one field's
// distinct values comes to for it.
//
//	records          100
//	distinct values  1 + 100/3 = 34
const (
	testIndexedTableRecords  = 100
	testIndexedTableDistinct = 1 + testIndexedTableRecords/distinctValuesDivisor
)

// Searching an index reads the index rather than the table, so what it costs
// comes from how the index's own records pack into a block, not from how the
// table's do. An index on a wide field holds fewer records to a block and so
// takes more of them.
//
//	an index on an int         16 byte slots, 800/16 = 50 to a block
//	an index on a varchar(20)  96 byte slots, 800/96 =  8 to a block
//
// A B-tree search reads one directory block per level and then a leaf. Each
// level down multiplies by how many records go in a block, so the levels are
// the fewest powers of that it takes to reach the index's blocks, and never
// fewer than one, the root:
//
//	150000 records on an int        150000/50 = 3000 blocks, 50*50*50 >= 3000 in 3 levels, so 4
//	15000 records on a varchar(20)  15000/8   = 1875 blocks, 8*8*8*8 >= 1875 in 4 levels, so 5
//	100 records on an int           100/50    =    2 blocks, 50 >= 2 in 1 level, so 2
//
// The division rounds up. Records that fill a block and start another are in
// two.
//
// None of these come to what a hash index would cost, one bucket's share of
// the blocks (30, 19, 1 and 1), so a cost still worked out as a hash index's
// does not pass for a B-tree's.
func TestIndexInfoBlocksAccessed(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		numRecords int
		want       int
	}{
		{
			name:       "given a B-tree index on an int field of 3000 blocks, when the blocks a search would touch are asked for, then it is three directory levels and a leaf",
			fieldName:  "id",
			numRecords: 150000,
			want:       4,
		},
		{
			name:       "given a B-tree index on a varchar field, whose records are wider and so fewer to a block, when the blocks a search would touch are asked for, then it is more levels than for an int",
			fieldName:  "name",
			numRecords: 15000,
			want:       5,
		},
		{
			name:       "given a B-tree index of two blocks, when the blocks a search would touch are asked for, then it is the root and a leaf",
			fieldName:  "id",
			numRecords: testIndexedTableRecords,
			want:       2,
		},
		{
			name:       "given a B-tree index with fewer records than fill a block, when the blocks a search would touch are asked for, then it is the root and the leaf",
			fieldName:  "id",
			numRecords: 10,
			want:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewIndexInfo(
				newTestTransaction(t),
				testIndexName,
				tt.fieldName,
				newTestSchema(t),
				NewTableStatistics(7, tt.numRecords),
			)
			if err != nil {
				t.Fatalf("NewIndexInfo() error = %v", err)
			}

			if got := info.BlocksAccessed(); got != tt.want {
				t.Errorf("BlocksAccessed() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIndexInfoOpen(t *testing.T) {
	t.Run("given an index described on a field, when it is opened, then it is a B-tree index", func(t *testing.T) {
		info := newTestIndexInfo(t, testIndexFieldName)

		idx, err := info.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer idx.Close()

		if _, ok := idx.(*index.BTreeIndex); !ok {
			t.Errorf("Open() = %T, want *index.BTreeIndex", idx)
		}
	})

	t.Run("given an index described on a field, when it is opened, then an entry inserted through it is found by searching it", func(t *testing.T) {
		info := newTestIndexInfo(t, testIndexFieldName)

		idx, err := info.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer idx.Close()

		value := query.NewIntConstant(7)
		rid := recordmanager.NewRecordID(3, 1)
		if err := idx.Insert(value, rid); err != nil {
			t.Fatalf("Insert(%s, %s) error = %v", value, rid, err)
		}

		if err := idx.MoveBeforeFirstMatch(value); err != nil {
			t.Fatalf("MoveBeforeFirstMatch(%s) error = %v", value, err)
		}
		ok, err := idx.MoveToNextMatch()
		if err != nil {
			t.Fatalf("MoveToNextMatch() error = %v", err)
		}
		if !ok {
			t.Fatalf("MoveToNextMatch() = false, want the entry filed under %s", value)
		}
		got, err := idx.DataRecordID()
		if err != nil {
			t.Fatalf("DataRecordID() error = %v", err)
		}
		if !got.Equals(rid) {
			t.Errorf("DataRecordID() = %s, want %s", got, rid)
		}
	})
}

// A search through an index gives back the records filed under one value, not
// every record of the table. How many that is follows from how many different
// values the field holds: the more there are, the fewer records share each one.
func TestIndexInfoRecordsOutput(t *testing.T) {
	tests := []struct {
		name       string
		numRecords int
		want       int
	}{
		{
			name:       "given a table of a hundred records, when the records a search would give back are asked for, then it is those over the values they are spread across",
			numRecords: testIndexedTableRecords,
			want:       testIndexedTableRecords / testIndexedTableDistinct,
		},
		{
			name:       "given a table with no records, when the records a search would give back are asked for, then it is none of them",
			numRecords: 0,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewIndexInfo(
				newTestTransaction(t),
				testIndexName,
				testIndexFieldName,
				newTestSchema(t),
				NewTableStatistics(7, tt.numRecords),
			)
			if err != nil {
				t.Fatalf("NewIndexInfo() error = %v", err)
			}

			if got := info.RecordsOutput(); got != tt.want {
				t.Errorf("RecordsOutput() = %d, want %d", got, tt.want)
			}
		})
	}
}

// Reading through an index is reading records that all hold one value in the
// indexed field, so that field has one value among them however many the table
// holds. Any other field is as varied among them as it is in the table, which
// is the most that can be said without looking.
func TestIndexInfoDistinctValues(t *testing.T) {
	tests := []struct {
		name     string
		askedFor string
		want     int
	}{
		{
			name:     "given an index on a field, when that same field's distinct values are asked for, then it is the one value the search was for",
			askedFor: testIndexFieldName,
			want:     1,
		},
		{
			name:     "given an index on a field, when another field's distinct values are asked for, then it is the guess the table gives for it",
			askedFor: "name",
			want:     testIndexedTableDistinct,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newTestIndexInfo(t, testIndexFieldName).DistinctValues(tt.askedFor); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.askedFor, got, tt.want)
			}
		})
	}
}
