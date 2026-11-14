package metadatamanager

import (
	"errors"
	"slices"
	"strings"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The sizes that every expected number in this file is based on:
//
//	an int          4 bytes
//	a varchar(n)    4 + 4n bytes
//	the in-use flag 4 bytes, at the front of every slot
//
// The catalogs describe themselves, so their sizes are asserted twice: once
// in the layouts that the manager builds in code, and once in the rows that
// are written with those layouts.
//
//	table_catalog   4 + (4 + 64) + 4                     =  76 bytes
//	field_catalog   4 + (4 + 64) + (4 + 64) + 4 + 4 + 4  = 152 bytes
//
// Every expected number below is a literal and is not read from a layout. A
// change to any of these sizes then fails a test here. Reading the numbers
// from a layout would only check that the catalogs match the layout, which
// they would still do after a format change that affects both.
const (
	tableCatalogSlotSize = 76
	fieldCatalogSlotSize = 152
)

func TestNewTableManager(t *testing.T) {
	tests := []struct {
		name   string
		layout func(tm *TableManager) *recordmanager.Layout
		want   layoutDescription
	}{
		{
			name: "it builds a table catalog layout holding the name and the slot size of one table",
			layout: func(tm *TableManager) *recordmanager.Layout {
				return tm.tableCatalogLayout
			},
			want: layoutDescription{
				slotSize: tableCatalogSlotSize,
				fields: []layoutField{
					{
						name:      tableNameField,
						fieldType: recordmanager.FieldTypeVarchar,
						length:    maxNameLength,
						offset:    4,
					},
					{
						name:      slotSizeField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    72,
					},
				},
			},
		},
		{
			name: "it builds a field catalog layout holding the table a field belongs to and the name, type, length and offset of that field",
			layout: func(tm *TableManager) *recordmanager.Layout {
				return tm.fieldCatalogLayout
			},
			want: layoutDescription{
				slotSize: fieldCatalogSlotSize,
				fields: []layoutField{
					{
						name:      tableNameField,
						fieldType: recordmanager.FieldTypeVarchar,
						length:    maxNameLength,
						offset:    4,
					},
					{
						name:      fieldNameField,
						fieldType: recordmanager.FieldTypeVarchar,
						length:    maxNameLength,
						offset:    72,
					},
					{
						name:      fieldTypeField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    140,
					},
					{
						name:      fieldLengthField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    144,
					},
					{
						name:      fieldOffsetField,
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    148,
					},
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

			// The order of the returned fields is part of the assertion. A
			// layout assigns offsets in field order, so a catalog whose fields
			// were added in another order has a different record format,
			// whatever its offsets are.
			assertLayout(t, tt.layout(tm), tt.want)
		})
	}
}

// The table that the tests below create: an int and a varchar of 20
// characters.
//
//	test_table  4 + 4 + (4 + 80) = 92 bytes, with "id" at offset 4 and "name"
//	            at offset 8
const (
	testTableName         = "test_table"
	testStringFieldLength = 20
	testTableSlotSize     = 92
)

// newTestSchema returns the schema of testTableName. It has one field of each
// type, so that the field catalog rows differ: an int and a varchar are
// stored differently, because only a varchar has a length.
func newTestSchema(t *testing.T) *recordmanager.Schema {
	t.Helper()

	schema := recordmanager.NewSchema()
	mustAddIntField(t, schema, "id")
	mustAddStringField(t, schema, "name", testStringFieldLength)

	return schema
}

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
				slotSize:  testTableSlotSize,
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

// The catalogs describe themselves, so creating them writes rows for both
// catalogs into both catalogs.
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
				slotSize:  tableCatalogSlotSize,
			},
			{
				tableName: fieldCatalogName,
				slotSize:  fieldCatalogSlotSize,
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

// A name that is too long for the catalogs is rejected and not written. The
// record page would reject it too, but only partway through the write. Its
// error would name a field of the field catalog and not the name that the
// caller passed, and the rows before it would already be written.
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

			// The error is of little use if some rows have already been
			// written. The catalogs would then partly describe a table, and no
			// later call would remove those rows.
			if rows := readTableCatalog(t, tx, tm); len(rows) != 0 {
				t.Errorf("the table catalog holds %+v, want nothing: the refused table was written anyway", rows)
			}
			if rows := readFieldCatalog(t, tx, tm); len(rows) != 0 {
				t.Errorf("the field catalog holds %+v, want nothing: the refused table was written anyway", rows)
			}
		})
	}
}

// The second table that the read tests need. Its only field is a varchar with
// a different length, so that its layout cannot be confused with the layout
// of testTableName.
//
//	other_table  4 + (4 + 120) = 128 bytes, with "title" at offset 4
const (
	otherTableName     = "other_table"
	otherTableSlotSize = 128
)

// createTestTables adds two tables to the catalogs. Reading one of them then
// requires selecting rows, not reading every row.
func createTestTables(t *testing.T, tx *transaction.Transaction, tm *TableManager) {
	t.Helper()

	if err := tm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", testTableName, err)
	}

	other := recordmanager.NewSchema()
	mustAddStringField(t, other, "title", 30)

	if err := tm.CreateTable(tx, otherTableName, other); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", otherTableName, err)
	}
}

func TestTableManagerGetLayout(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		want      layoutDescription
	}{
		{
			name:      "given two tables in the catalogs, when the first one is asked for, then the layout holds its own fields and none of the other's",
			tableName: testTableName,
			want: layoutDescription{
				slotSize: testTableSlotSize,
				fields: []layoutField{
					{
						name:      "id",
						fieldType: recordmanager.FieldTypeInt,
						length:    0,
						offset:    4,
					},
					{
						name:      "name",
						fieldType: recordmanager.FieldTypeVarchar,
						length:    testStringFieldLength,
						offset:    8,
					},
				},
			},
		},
		{
			name:      "given two tables in the catalogs, when the second one is asked for, then the layout holds its own fields and none of the other's",
			tableName: otherTableName,
			want: layoutDescription{
				slotSize: otherTableSlotSize,
				fields: []layoutField{
					{
						name:      "title",
						fieldType: recordmanager.FieldTypeVarchar,
						length:    30,
						offset:    4,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			tm := mustNewTableManager(t)
			createTestTables(t, tx, tm)

			layout, err := tm.GetLayout(tx, tt.tableName)
			if err != nil {
				t.Fatalf("GetLayout() error = %v", err)
			}

			assertLayout(t, layout, tt.want)
		})
	}

	t.Run("given catalogs that hold another table, when a table they do not hold is asked for, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)
		createTestTables(t, tx, tm)

		if _, err := tm.GetLayout(tx, "no_such_table"); !errors.Is(err, ErrTableNotFound) {
			t.Errorf("error = %v, want %v", err, ErrTableNotFound)
		}
	})

	// The catalogs describe themselves, so the layout of a catalog that is
	// read from the catalog must equal the layout it was created with. The
	// rest of the database depends on this read: the layout of every other
	// table is read through these two catalogs. If their layouts were
	// wrong, every later read would be wrong too.
	t.Run("given the catalogs describing themselves, when the table catalog's layout is read back out of them, then it is the one it was created from", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateCatalogTables(tx); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}

		layout, err := tm.GetLayout(tx, tableCatalogName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		assertLayout(t, layout, describeLayout(t, tm.tableCatalogLayout))
	})

	t.Run("given the catalogs describing themselves, when the field catalog's layout is read back out of them, then it is the one it was created from", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)

		if err := tm.CreateCatalogTables(tx); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}

		layout, err := tm.GetLayout(tx, fieldCatalogName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		assertLayout(t, layout, describeLayout(t, tm.fieldCatalogLayout))
	})
}
