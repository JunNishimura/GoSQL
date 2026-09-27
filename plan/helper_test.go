package plan

import (
	"fmt"
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	testLogFile = "test.log"
	// testBlockSize has to hold a slot of the widest catalog, which is the view
	// catalog at 476 bytes, since a record page refuses a layout whose slot
	// does not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
	// testStringFieldLength is the character limit of the varchar field of the
	// test table.
	testStringFieldLength = 10
)

// testTableName is the table the plan tests read. Its schema is the one
// newTestSchema builds.
const testTableName = "test"

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see the tables another made.
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

// newTestMetadataManager builds a metadata manager over a database whose
// catalogs already exist, which is the state every plan starts from.
func newTestMetadataManager(t *testing.T, tx *transaction.Transaction) *metadatamanager.MetadataManager {
	t.Helper()

	mm, err := metadatamanager.NewMetadataManager()
	if err != nil {
		t.Fatalf("NewMetadataManager() error = %v", err)
	}
	if err := mm.CreateCatalogTables(tx); err != nil {
		t.Fatalf("CreateCatalogTables() error = %v", err)
	}

	return mm
}

// newTestSchema builds the schema of the test table: "id" is an int and "name"
// a varchar, so that a plan's schema has one field of each kind to get right.
func newTestSchema(t *testing.T) *recordmanager.Schema {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("id"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "id", err)
	}
	if err := schema.AddStringField("name", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "name", err)
	}

	return schema
}

// createTestTable makes the test table and writes count records into it. The
// id of the i-th record is i, which is what lets a test say which records a
// scan came back with.
func createTestTable(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, count int) {
	t.Helper()

	if err := mm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", testTableName, err)
	}

	layout, err := mm.GetLayout(tx, testTableName)
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

// readIDs reads the id of every record a scan gives back, in the order it gives
// them.
func readIDs(t *testing.T, s query.Scan) []int32 {
	t.Helper()

	if err := s.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	ids := []int32{}
	for {
		hasNext, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return ids
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		ids = append(ids, id)
	}
}
