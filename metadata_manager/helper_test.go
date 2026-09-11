package metadatamanager

import (
	"slices"
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	testLogFile    = "test.log"
	testBlockSize  = 400
	testNumBuffers = 8
)

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see the catalogs another wrote.
func newTestTransaction(t *testing.T) *transaction.Transaction {
	t.Helper()

	fm, err := filemanager.NewFileManager(t.TempDir(), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	bm, err := buffermanager.NewBufferManager(fm, lm, testNumBuffers)
	if err != nil {
		t.Fatalf("NewBufferManager() error = %v", err)
	}

	tx, err := transaction.NewTransaction(fm, lm, bm, concurrencymanager.NewLockTable(), 1)
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	return tx
}

// tableCatalogRow is one row of the table catalog, read back out.
type tableCatalogRow struct {
	tableName string
	slotSize  int32
}

// fieldCatalogRow is one row of the field catalog, read back out.
type fieldCatalogRow struct {
	tableName   string
	fieldName   string
	fieldType   int32
	fieldLength int32
	fieldOffset int32
}

// readTableCatalog reads every row of the table catalog, in the order the rows
// were written.
//
// The catalogs are read through a scan of their own rather than through the
// table manager, so that what a test asserts is what reached the records, not
// what the manager reports about them.
func readTableCatalog(t *testing.T, tx *transaction.Transaction, tm *TableManager) []tableCatalogRow {
	t.Helper()

	ts, err := recordmanager.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", tableCatalogName, err)
	}
	defer ts.Close()

	rows := []tableCatalogRow{}
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return rows
		}

		row := tableCatalogRow{
			tableName: mustGetString(t, ts, tableNameField),
			slotSize:  mustGetInt(t, ts, slotSizeField),
		}
		rows = append(rows, row)
	}
}

// readFieldCatalog reads every row of the field catalog, in the order the rows
// were written.
func readFieldCatalog(t *testing.T, tx *transaction.Transaction, tm *TableManager) []fieldCatalogRow {
	t.Helper()

	ts, err := recordmanager.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", fieldCatalogName, err)
	}
	defer ts.Close()

	rows := []fieldCatalogRow{}
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return rows
		}

		row := fieldCatalogRow{
			tableName:   mustGetString(t, ts, tableNameField),
			fieldName:   mustGetString(t, ts, fieldNameField),
			fieldType:   mustGetInt(t, ts, fieldTypeField),
			fieldLength: mustGetInt(t, ts, fieldLengthField),
			fieldOffset: mustGetInt(t, ts, fieldOffsetField),
		}
		rows = append(rows, row)
	}
}

func mustGetString(t *testing.T, ts *recordmanager.TableScan, fieldName string) string {
	t.Helper()

	val, err := ts.GetString(fieldName)
	if err != nil {
		t.Fatalf("GetString(%q) error = %v", fieldName, err)
	}

	return val
}

func mustGetInt(t *testing.T, ts *recordmanager.TableScan, fieldName string) int32 {
	t.Helper()

	val, err := ts.GetInt(fieldName)
	if err != nil {
		t.Fatalf("GetInt(%q) error = %v", fieldName, err)
	}

	return val
}

func mustAddIntField(t *testing.T, schema *recordmanager.Schema, fieldName string) {
	t.Helper()

	if err := schema.AddIntField(fieldName); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", fieldName, err)
	}
}

func mustAddStringField(t *testing.T, schema *recordmanager.Schema, fieldName string, length int) {
	t.Helper()

	if err := schema.AddStringField(fieldName, length); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", fieldName, err)
	}
}

// layoutField is one placed field of a layout: what a schema says about it,
// plus where the layout put it.
type layoutField struct {
	name      string
	fieldType recordmanager.FieldType
	length    int
	offset    int
}

// layoutDescription is everything a layout says, flattened so that two layouts
// can be compared as data rather than field by field at every call site.
type layoutDescription struct {
	slotSize int
	fields   []layoutField
}

func describeLayout(t *testing.T, layout *recordmanager.Layout) layoutDescription {
	t.Helper()

	schema := layout.Schema()

	fields := []layoutField{}
	for _, fieldName := range schema.Fields() {
		fieldType, err := schema.Type(fieldName)
		if err != nil {
			t.Fatalf("Type(%q) error = %v", fieldName, err)
		}
		length, err := schema.Length(fieldName)
		if err != nil {
			t.Fatalf("Length(%q) error = %v", fieldName, err)
		}
		offset, err := layout.Offset(fieldName)
		if err != nil {
			t.Fatalf("Offset(%q) error = %v", fieldName, err)
		}

		fields = append(fields, layoutField{
			name:      fieldName,
			fieldType: fieldType,
			length:    length,
			offset:    offset,
		})
	}

	return layoutDescription{
		slotSize: layout.SlotSize(),
		fields:   fields,
	}
}

// assertLayout reports the slot size and the fields separately, so that a
// failure says which of the two is wrong rather than printing both layouts and
// leaving the reader to find the difference.
func assertLayout(t *testing.T, got *recordmanager.Layout, want layoutDescription) {
	t.Helper()

	description := describeLayout(t, got)

	if description.slotSize != want.slotSize {
		t.Errorf("the layout has slots of %d bytes, want %d", description.slotSize, want.slotSize)
	}
	if !slices.Equal(description.fields, want.fields) {
		t.Errorf("the layout holds %+v, want %+v", description.fields, want.fields)
	}
}

func mustNewTableManager(t *testing.T) *TableManager {
	t.Helper()

	tm, err := NewTableManager()
	if err != nil {
		t.Fatalf("NewTableManager() error = %v", err)
	}

	return tm
}
