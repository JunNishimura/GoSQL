package metadatamanager

import (
	"errors"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The index the tests below describe: one on the int field of the test table.
const (
	testIndexName      = "test_table_id_index"
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
	t.Run("it carries the index name, the field it is on, the schema of the table it indexes and what is known about that table", func(t *testing.T) {
		tableSchema := newTestSchema(t)
		tableStatistics := NewTableStatistics(7, 100)

		info, err := NewIndexInfo(testIndexName, testIndexFieldName, tableSchema, tableStatistics)
		if err != nil {
			t.Fatalf("NewIndexInfo() error = %v", err)
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
		tableSchema := newTestSchema(t)

		_, err := NewIndexInfo(testIndexName, "missing", tableSchema, NewTableStatistics(7, 100))

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
						name:      indexBlockNumberField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    4,
					},
					{
						name:      indexSlotField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    8,
					},
					{
						name:      indexedValueField,
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
						name:      indexBlockNumberField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    4,
					},
					{
						name:      indexSlotField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    8,
					},
					{
						name:      indexedValueField,
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
