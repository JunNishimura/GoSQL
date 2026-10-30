package materialize

import (
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
	testLogFile           = "test.log"
	testBlockSize         = 400
	testNumBuffers        = 8
	testStringFieldLength = 10
)

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

// fakeScan is a scan standing on one record, whose fields hold the values the
// test put in the map. Only reading that record is supported: it has no other
// record to move to.
type fakeScan map[string]query.Constant

var _ query.Scan = fakeScan{}

func (fs fakeScan) MoveBeforeFirstRecord() error    { return nil }
func (fs fakeScan) MoveToNextRecord() (bool, error) { return false, nil }
func (fs fakeScan) GetInt(fieldName string) (int32, error) {
	return fs[fieldName].AsInt()
}
func (fs fakeScan) GetString(fieldName string) (string, error) {
	return fs[fieldName].AsString()
}
func (fs fakeScan) GetValue(fieldName string) (query.Constant, error) {
	return fs[fieldName], nil
}
func (fs fakeScan) HasField(fieldName string) bool {
	_, ok := fs[fieldName]
	return ok
}
func (fs fakeScan) Close() {}

// newTestSchema builds a schema of an int "id" and a varchar "name", one field
// of each kind.
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
