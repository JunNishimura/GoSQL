package query

import (
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
	testDataFile   = "test.tbl"
	testBlockSize  = 400
	testNumBuffers = 3
	// testStringFieldLength is the character limit of the varchar field the
	// tests share. It is named rather than written out so that a case about the
	// limit reads as being about the limit, whatever the number happens to be.
	testStringFieldLength = 20
)

// The numbers the slot tests rely on, spelled out once:
//
//	a block         400 bytes
//	a slot           92 bytes, the in-use flag (4) plus id (4) plus name (84)
//	slots per block   4, so 0 through 3 fit and 4 does not
const (
	testSlotSize     = 92
	testSlotsInBlock = testBlockSize / testSlotSize
)

// overwideFieldLength is a varchar wide enough that a slot of one such field,
// 4 + (4 + 396) = 404 bytes, does not fit in a block at all.
const overwideFieldLength = 99

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

// newTestLayout builds a layout over the schema the scan tests share: "id" is
// an int and "name" a varchar of 20 characters. One field of each kind is what
// lets a test reach for the wrong one on purpose.
func newTestLayout(t *testing.T) *recordmanager.Layout {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddIntField("id"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "id", err)
	}
	if err := s.AddStringField("name", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "name", err)
	}

	return recordmanager.NewLayout(s)
}

// newLayoutOfOneStringField builds a layout of a single varchar, which is what
// lets a test put a slot at a width of its choosing.
func newLayoutOfOneStringField(t *testing.T, length int) *recordmanager.Layout {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddStringField("definition", length); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "definition", err)
	}

	return recordmanager.NewLayout(s)
}
