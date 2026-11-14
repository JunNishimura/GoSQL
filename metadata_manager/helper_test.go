package metadatamanager

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	testLogFile = "test.log"
	// testBlockSize must fit a slot of the largest catalog, which is the view
	// catalog with 476 bytes. A record page rejects a layout whose slot does
	// not fit in a block.
	//
	// It is not larger than necessary. With a block that held every record
	// of every test, no test would cross from one block to the next, and
	// those transitions are the part of a scan that most needs testing.
	testBlockSize  = 800
	testNumBuffers = 8
)

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see the catalogs of another.
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

// tableCatalogRow is one row of the table catalog, as read back.
type tableCatalogRow struct {
	tableName string
	slotSize  int32
}

// fieldCatalogRow is one row of the field catalog, as read back.
type fieldCatalogRow struct {
	tableName   string
	fieldName   string
	fieldType   int32
	fieldLength int32
	fieldOffset int32
}

// readTableCatalog reads every row of the table catalog, in the order in
// which the rows were written.
//
// The catalogs are read through a separate scan and not through the table
// manager. A test then asserts the stored records, not the manager's report
// of them.
func readTableCatalog(t *testing.T, tx *transaction.Transaction, tm *TableManager) []tableCatalogRow {
	t.Helper()

	ts, err := query.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
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

// readFieldCatalog reads every row of the field catalog, in the order in
// which the rows were written.
func readFieldCatalog(t *testing.T, tx *transaction.Transaction, tm *TableManager) []fieldCatalogRow {
	t.Helper()

	ts, err := query.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
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

// testDatabase holds the components that several transactions share: one set
// of files, one log, one buffer pool, and one lock table.
//
// newTestTransaction gives each transaction its own database, so that one
// test cannot read the catalogs of another. A test of interacting
// transactions needs the opposite, which this type provides.
type testDatabase struct {
	fileManager   *filemanager.FileManager
	logManager    *logmanager.LogManager
	bufferManager *buffermanager.BufferManager
	lockTable     *concurrencymanager.LockTable
	nextTxNum     atomic.Int64
}

func newTestDatabase(t *testing.T) *testDatabase {
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

	return &testDatabase{
		fileManager:   fm,
		logManager:    lm,
		bufferManager: bm,
		lockTable:     concurrencymanager.NewLockTable(),
	}
}

// newTransaction starts a transaction on the shared database, with a number
// that no other transaction of the database has had.
//
// It returns an error and does not use *testing.T, so that a goroutine other
// than the test goroutine can call it. A Fatal on such a goroutine does not
// stop the test, and the test would pass although the run failed.
func (db *testDatabase) newTransaction() (*transaction.Transaction, error) {
	return transaction.NewTransaction(
		db.fileManager,
		db.logManager,
		db.bufferManager,
		db.lockTable,
		int(db.nextTxNum.Add(1)),
	)
}

func (db *testDatabase) mustNewTransaction(t *testing.T) *transaction.Transaction {
	t.Helper()

	tx, err := db.newTransaction()
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	return tx
}

// insertTestRecords writes count records to the test table, so that its
// statistics have records to count. The values differ between records only so
// that an overwritten record would be detected.
func insertTestRecords(t *testing.T, tx *transaction.Transaction, tm *TableManager, count int) {
	t.Helper()

	layout, err := tm.GetLayout(tx, testTableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", testTableName, err)
	}

	ts, err := query.NewTableScan(tx, testTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", testTableName, err)
	}
	defer ts.Close()

	for i := range count {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("id", int32(i)); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := ts.SetString("name", fmt.Sprintf("name%d", i)); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}
}

// viewCatalogRow is one row of the view catalog, as read back.
type viewCatalogRow struct {
	viewName   string
	definition string
}

// readViewCatalog reads every row of the view catalog, in the order in which
// the rows were written.
//
// Unlike the other two catalogs, the view catalog has no stored layout. It is
// an ordinary table, so its layout is read from the table catalog. The helper
// reads the layout in the same way as the view manager, so that it does not
// assert through a layout that the manager never used.
func readViewCatalog(t *testing.T, tx *transaction.Transaction, vm *ViewManager) []viewCatalogRow {
	t.Helper()

	layout, err := vm.tableManager.GetLayout(tx, viewCatalogName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", viewCatalogName, err)
	}

	ts, err := query.NewTableScan(tx, viewCatalogName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", viewCatalogName, err)
	}
	defer ts.Close()

	rows := []viewCatalogRow{}
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return rows
		}

		row := viewCatalogRow{
			viewName:   mustGetString(t, ts, viewNameField),
			definition: mustGetString(t, ts, viewDefinitionField),
		}
		rows = append(rows, row)
	}
}

// indexCatalogRow is one row of the index catalog, as read back.
type indexCatalogRow struct {
	indexName string
	tableName string
	fieldName string
}

// readIndexCatalog reads every row of the index catalog, in the order in
// which the rows were written.
//
// The helper gets the layout from the table manager, as the index manager
// does, and does not build it. A test therefore cannot pass by reading
// through a layout that the manager never wrote with.
func readIndexCatalog(t *testing.T, tx *transaction.Transaction, im *IndexManager) []indexCatalogRow {
	t.Helper()

	layout, err := im.tableManager.GetLayout(tx, indexCatalogName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", indexCatalogName, err)
	}

	ts, err := query.NewTableScan(tx, indexCatalogName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", indexCatalogName, err)
	}
	defer ts.Close()

	rows := []indexCatalogRow{}
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return rows
		}

		row := indexCatalogRow{
			indexName: mustGetString(t, ts, indexNameField),
			tableName: mustGetString(t, ts, tableNameField),
			fieldName: mustGetString(t, ts, fieldNameField),
		}
		rows = append(rows, row)
	}
}

func mustGetString(t *testing.T, ts *query.TableScan, fieldName string) string {
	t.Helper()

	val, err := ts.GetString(fieldName)
	if err != nil {
		t.Fatalf("GetString(%q) error = %v", fieldName, err)
	}

	return val
}

func mustGetInt(t *testing.T, ts *query.TableScan, fieldName string) int32 {
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

// layoutField is one field of a layout: its schema information and its offset
// in the layout.
type layoutField struct {
	name      string
	fieldType recordmanager.FieldType
	length    int
	offset    int
}

// layoutDescription holds all the data of a layout in a flat form, so that
// two layouts can be compared as values and not field by field at each call
// site.
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

// assertLayout reports the slot size and the fields separately. A failure
// then shows which of the two is wrong, and the reader does not have to
// compare two printed layouts.
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
