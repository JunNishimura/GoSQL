package index

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
	testLogFile    = "test.log"
	testBlockSize  = 400
	testNumBuffers = 3
	testIndexName  = "test_index"
)

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see what another wrote.
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

// newTestIndexLayout builds the layout of an index on an int field, laid out
// the way the catalog lays one out.
func newTestIndexLayout(t *testing.T) *recordmanager.Layout {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddIntField(BlockNumberField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", BlockNumberField, err)
	}
	if err := s.AddIntField(SlotField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", SlotField, err)
	}
	if err := s.AddIntField(IndexedValueField); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", IndexedValueField, err)
	}

	return recordmanager.NewLayout(s)
}

// entry is one record id filed under one value, as a test inserts it.
type entry struct {
	value query.Constant
	rid   *recordmanager.RecordID
}

// insertEntries files each of entries in idx, in order.
func insertEntries(t *testing.T, idx Index, entries []entry) {
	t.Helper()

	for _, e := range entries {
		if err := idx.Insert(e.value, e.rid); err != nil {
			t.Fatalf("Insert(%s, %s) error = %v", e.value, e.rid, err)
		}
	}
}

// searchRecordIDs searches idx for searchKey and returns the record ids that
// come back, written out so that they compare with ==.
func searchRecordIDs(t *testing.T, idx Index, searchKey query.Constant) []string {
	t.Helper()

	if err := idx.MoveBeforeFirstMatch(searchKey); err != nil {
		t.Fatalf("MoveBeforeFirstMatch(%s) error = %v", searchKey, err)
	}

	var got []string
	for {
		ok, err := idx.MoveToNextMatch()
		if err != nil {
			t.Fatalf("MoveToNextMatch() error = %v", err)
		}
		if !ok {
			return got
		}

		rid, err := idx.DataRecordID()
		if err != nil {
			t.Fatalf("DataRecordID() error = %v", err)
		}
		got = append(got, rid.String())
	}
}
