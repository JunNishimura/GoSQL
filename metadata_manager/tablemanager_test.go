package metadatamanager

import (
	"errors"
	"slices"
	"strings"
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

// The table the create tests describe: an int and a varchar of 20 characters.
const (
	testTableName         = "test_table"
	testStringFieldLength = 20
)

// newTestSchema is the schema of testTableName. One field of each kind is what
// makes the field catalog rows worth reading: an int and a varchar are written
// down differently, since only one of the two has a length.
func newTestSchema(t *testing.T) *recordmanager.Schema {
	t.Helper()

	schema := recordmanager.NewSchema()
	mustAddIntField(t, schema, "id")
	mustAddStringField(t, schema, "name", testStringFieldLength)

	return schema
}

// The widths the expected numbers below are built from, spelled out once:
//
//	an int          4 bytes
//	a varchar(n)    4 + 4n bytes
//	the in-use flag 4 bytes, at the front of every slot
//
// So a record of testTableName takes 4 + 4 + (4 + 80) = 92 bytes, with "id" at
// offset 4 and "name" at offset 8.
//
// They are written as literals rather than asked of a layout, so that a change
// to any of those widths shows up here as a failing test. Asking a layout would
// only check that the catalog agrees with whatever the layout said, which it
// would go on doing after the format changed underneath both.
func TestTableManagerCreateTable(t *testing.T) {
	t.Run("given a table of an int and a varchar field, when it is created, then the table catalog holds its name and the size of its slots", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}

		want := []tableCatalogRow{
			{
				tableName: testTableName,
				slotSize:  92,
			},
		}
		if got := readTableCatalog(t, tx, tm); !slices.Equal(got, want) {
			t.Errorf("the table catalog holds %+v, want %+v", got, want)
		}
	})

	t.Run("given a table of an int and a varchar field, when it is created, then the field catalog holds a row for each field carrying its type, length and offset", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}

		want := []fieldCatalogRow{
			{
				tableName:   testTableName,
				fieldName:   "id",
				fieldType:   int32(recordmanager.FieldTypeInt),
				fieldLength: 0,
				fieldOffset: 4,
			},
			{
				tableName:   testTableName,
				fieldName:   "name",
				fieldType:   int32(recordmanager.FieldTypeVarchar),
				fieldLength: testStringFieldLength,
				fieldOffset: 8,
			},
		}
		if got := readFieldCatalog(t, tx, tm); !slices.Equal(got, want) {
			t.Errorf("the field catalog holds %+v, want %+v", got, want)
		}
	})

	t.Run("given a table whose name is exactly as long as the catalogs hold, when it is created, then it is recorded", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		tableName := strings.Repeat("a", maxNameLength)

		if err := tm.CreateTable(tx, tableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}

		rows := readTableCatalog(t, tx, tm)
		if len(rows) != 1 || rows[0].tableName != tableName {
			t.Errorf("the table catalog holds %+v, want the one table %q", rows, tableName)
		}
	})
}

// The catalogs describe themselves, so creating them writes rows about the two
// of them into the two of them.
//
//	table_catalog   4 + (4 + 64) + 4                     =  76 bytes
//	field_catalog   4 + (4 + 64) + (4 + 64) + 4 + 4 + 4  = 152 bytes
func TestTableManagerCreateCatalogTables(t *testing.T) {
	t.Run("when the catalogs are created, then the table catalog holds a row for itself and one for the field catalog", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateCatalogTables(tx); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}

		want := []tableCatalogRow{
			{
				tableName: tableCatalogName,
				slotSize:  76,
			},
			{
				tableName: fieldCatalogName,
				slotSize:  152,
			},
		}
		if got := readTableCatalog(t, tx, tm); !slices.Equal(got, want) {
			t.Errorf("the table catalog holds %+v, want %+v", got, want)
		}
	})

	t.Run("when the catalogs are created, then the field catalog holds a row for every field of both catalogs", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateCatalogTables(tx); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}

		want := []fieldCatalogRow{
			{
				tableName:   tableCatalogName,
				fieldName:   tableNameField,
				fieldType:   int32(recordmanager.FieldTypeVarchar),
				fieldLength: maxNameLength,
				fieldOffset: 4,
			},
			{
				tableName:   tableCatalogName,
				fieldName:   slotSizeField,
				fieldType:   int32(recordmanager.FieldTypeInt),
				fieldLength: 0,
				fieldOffset: 72,
			},
			{
				tableName:   fieldCatalogName,
				fieldName:   tableNameField,
				fieldType:   int32(recordmanager.FieldTypeVarchar),
				fieldLength: maxNameLength,
				fieldOffset: 4,
			},
			{
				tableName:   fieldCatalogName,
				fieldName:   fieldNameField,
				fieldType:   int32(recordmanager.FieldTypeVarchar),
				fieldLength: maxNameLength,
				fieldOffset: 72,
			},
			{
				tableName:   fieldCatalogName,
				fieldName:   fieldTypeField,
				fieldType:   int32(recordmanager.FieldTypeInt),
				fieldLength: 0,
				fieldOffset: 140,
			},
			{
				tableName:   fieldCatalogName,
				fieldName:   fieldLengthField,
				fieldType:   int32(recordmanager.FieldTypeInt),
				fieldLength: 0,
				fieldOffset: 144,
			},
			{
				tableName:   fieldCatalogName,
				fieldName:   fieldOffsetField,
				fieldType:   int32(recordmanager.FieldTypeInt),
				fieldLength: 0,
				fieldOffset: 148,
			},
		}
		if got := readFieldCatalog(t, tx, tm); !slices.Equal(got, want) {
			t.Errorf("the field catalog holds %+v, want %+v", got, want)
		}
	})
}

// A name the catalogs cannot hold is refused rather than written. The record
// page would refuse it too, but only partway through, with a message about a
// field of the field catalog rather than about the name the caller passed, and
// only after the rows ahead of it had already been written.
func TestTableManagerRejectsANameLongerThanTheCatalogsHold(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		schema    func(t *testing.T) *recordmanager.Schema
	}{
		{
			name:      "given a table name one character over what the catalogs hold, when the table is created, then it reports ErrNameTooLong",
			tableName: strings.Repeat("a", maxNameLength+1),
			schema:    newTestSchema,
		},
		{
			name:      "given a field name one character over what the catalogs hold, when the table is created, then it reports ErrNameTooLong",
			tableName: testTableName,
			schema: func(t *testing.T) *recordmanager.Schema {
				t.Helper()

				schema := recordmanager.NewSchema()
				mustAddIntField(t, schema, "id")
				mustAddIntField(t, schema, strings.Repeat("a", maxNameLength+1))

				return schema
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			tm := mustNewTableManager(t)

			if err := tm.CreateTable(tx, tt.tableName, tt.schema(t)); !errors.Is(err, ErrNameTooLong) {
				t.Errorf("error = %v, want %v", err, ErrNameTooLong)
			}

			// The refusal is worth little if it comes after some of the rows
			// have been written: what is left behind is a table the catalogs
			// half know about, which no later call has any reason to clean up.
			if rows := readTableCatalog(t, tx, tm); len(rows) != 0 {
				t.Errorf("the table catalog holds %+v, want nothing: the refused table was written anyway", rows)
			}
			if rows := readFieldCatalog(t, tx, tm); len(rows) != 0 {
				t.Errorf("the field catalog holds %+v, want nothing: the refused table was written anyway", rows)
			}
		})
	}
}
