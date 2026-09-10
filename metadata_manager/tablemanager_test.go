package metadatamanager

import (
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// catalogField is one field a catalog's schema has to hold: what it is called,
// what type it is, and, for a varchar, how many characters it takes at most.
// length is 0 for an int, whose width the schema does not carry.
type catalogField struct {
	name      string
	fieldType recordmanager.FieldType
	length    int
}

func TestNewTableManager(t *testing.T) {
	tests := []struct {
		name   string
		layout func(tm *TableManager) *recordmanager.Layout
		want   []catalogField
	}{
		{
			name: "it builds a table catalog layout holding the name and the slot size of one table",
			layout: func(tm *TableManager) *recordmanager.Layout {
				return tm.tableCatalogLayout
			},
			want: []catalogField{
				{
					name:      tableNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
				},
				{
					name:      slotSizeField,
					fieldType: recordmanager.FieldTypeInt,
					length:    0,
				},
			},
		},
		{
			name: "it builds a field catalog layout holding the table a field belongs to and the name, type, length and offset of that field",
			layout: func(tm *TableManager) *recordmanager.Layout {
				return tm.fieldCatalogLayout
			},
			want: []catalogField{
				{
					name:      tableNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
				},
				{
					name:      fieldNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
				},
				{
					name:      fieldTypeField,
					fieldType: recordmanager.FieldTypeInt,
					length:    0,
				},
				{
					name:      fieldLengthField,
					fieldType: recordmanager.FieldTypeInt,
					length:    0,
				},
				{
					name:      fieldOffsetField,
					fieldType: recordmanager.FieldTypeInt,
					length:    0,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, err := NewTableManager()
			if err != nil {
				t.Fatalf("NewTableManager() error = %v", err)
			}

			schema := tt.layout(tm).Schema()

			gotFields := schema.Fields()
			if len(gotFields) != len(tt.want) {
				t.Fatalf("the catalog has fields %v, want %d of them", gotFields, len(tt.want))
			}

			// The order is checked along with the names: a layout assigns
			// offsets by walking the fields in order, so a catalog whose fields
			// were added in another order is another record format.
			for i, want := range tt.want {
				if gotFields[i] != want.name {
					t.Errorf("field %d is %q, want %q", i, gotFields[i], want.name)
					continue
				}

				gotType, err := schema.Type(want.name)
				if err != nil {
					t.Fatalf("Type(%q) error = %v", want.name, err)
				}
				if gotType != want.fieldType {
					t.Errorf("field %q is a %v, want a %v", want.name, gotType, want.fieldType)
				}

				gotLength, err := schema.Length(want.name)
				if err != nil {
					t.Fatalf("Length(%q) error = %v", want.name, err)
				}
				if gotLength != want.length {
					t.Errorf("field %q holds %d characters, want %d", want.name, gotLength, want.length)
				}
			}
		})
	}
}
