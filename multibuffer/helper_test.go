package multibuffer

import (
	"fmt"
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
	// testTableName is the table of the blocks that a test writes and reads
	// through chunks.
	testTableName = "chunk"
	// testLeftTableName is the table of the left side of a product.
	testLeftTableName = "left"
)

// testFileName is the file of testTableName.
var testFileName = query.TableFileName(testTableName)

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see the tables of another.
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

// newTestLayout builds the layout of records with an int "id" and a varchar
// "name": one field of each type.
func newTestLayout(t *testing.T) *recordmanager.Layout {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("id"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "id", err)
	}
	if err := schema.AddStringField("name", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "name", err)
	}

	return recordmanager.NewLayout(schema)
}

// nameOf returns the name written with id, so that a test can derive the
// expected string field from the id.
func nameOf(id int32) string {
	return fmt.Sprintf("name%d", id)
}

// writeBlocks appends one block to fileName for each element of blocks and
// fills it with records for those ids. A test thereby chooses the block of
// each record. An empty element creates a block with no records.
func writeBlocks(t *testing.T, tx *transaction.Transaction, fileName string, layout *recordmanager.Layout, blocks [][]int32) {
	t.Helper()

	for _, ids := range blocks {
		blk, err := tx.Append(fileName)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		rp, err := recordmanager.NewRecordPage(tx, blk, layout)
		if err != nil {
			t.Fatalf("NewRecordPage() error = %v", err)
		}
		if err := rp.InitializeNewBlock(); err != nil {
			t.Fatalf("InitializeNewBlock() error = %v", err)
		}

		slot := -1
		for _, id := range ids {
			if slot, err = rp.ClaimFreeSlotAfter(slot); err != nil {
				t.Fatalf("ClaimFreeSlotAfter() error = %v", err)
			}
			if err := rp.SetInt(slot, "id", id); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
			if err := rp.SetString(slot, "name", nameOf(id)); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}
		}

		tx.Unpin(blk)
	}
}

// openLeftScan writes one record for each of lids into a table with one int
// field "lid". It returns a scan of that table, positioned before its first
// record, for the left side of a product. The scan keeps one buffer pinned.
func openLeftScan(t *testing.T, tx *transaction.Transaction, lids []int32) *query.TableScan {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("lid"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "lid", err)
	}
	ts, err := query.NewTableScan(tx, testLeftTableName, recordmanager.NewLayout(schema))
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}
	for _, lid := range lids {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("lid", lid); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
	}
	if err := ts.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	return ts
}

// pair is one record of a product: the lid of its left side and the id of
// its right side.
type pair struct {
	lid int32
	id  int32
}

// readPairs reads the remaining records of s and returns their lids and ids.
func readPairs(t *testing.T, s query.Scan) []pair {
	t.Helper()

	var pairs []pair
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return pairs
		}

		lid, err := s.GetInt("lid")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "lid", err)
		}
		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		pairs = append(pairs, pair{lid: lid, id: id})
	}
}

// readIDs reads the remaining records of s and returns their ids.
func readIDs(t *testing.T, s query.Scan) []int32 {
	t.Helper()

	var ids []int32
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return ids
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		ids = append(ids, id)
	}
}
