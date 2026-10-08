package server

import (
	"os"
	"path/filepath"
	"testing"

	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	// testBlockSize has to hold a slot of the widest catalog, which is the view
	// catalog at 476 bytes, since a record page refuses a layout whose slot
	// does not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
	// tableCatalogName is the catalog that lists every table, the catalogs
	// among them. It is the metadata manager's name, written out again here
	// since the metadata manager does not export it.
	tableCatalogName = "table_catalog"
	// testTxNum numbers the transactions the tests start on their own. It is
	// far above any number the database hands out, so that the log never shows
	// a commit of one of the database's own under the same number.
	testTxNum = 1000
)

// newTestTransaction starts a transaction on db outside the numbering db does
// itself.
func newTestTransaction(t *testing.T, db *DB, txNum int) *transaction.Transaction {
	t.Helper()

	tx, err := transaction.NewTransaction(
		db.FileManager(),
		db.LogManager(),
		db.BufferManager(),
		concurrencymanager.NewLockTable(),
		txNum,
	)
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	return tx
}

// countTables reports how many tables the table catalog lists.
func countTables(t *testing.T, db *DB, tx *transaction.Transaction) int {
	t.Helper()

	layout, err := db.MetadataManager().GetLayout(tx, tableCatalogName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", tableCatalogName, err)
	}
	ts, err := query.NewTableScan(tx, tableCatalogName, layout)
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}
	defer ts.Close()

	count := 0
	for {
		ok, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return count
		}
		count++
	}
}

func TestOpen(t *testing.T) {
	t.Run("given a database directory that does not exist, when it is opened, then the directory is created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "newdb")

		if _, err := Open(dir, testBlockSize, testNumBuffers); err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Stat(%q) error = %v, want the directory to exist", dir, err)
		}
	})

	t.Run("when a database is opened with a block size, then its file manager uses that block size", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if got := db.FileManager().BlockSize(); got != testBlockSize {
			t.Errorf("FileManager().BlockSize() = %d, want %d", got, testBlockSize)
		}
	})

	t.Run("given a database directory that does not exist, when it is opened, then the catalogs are made and list themselves", func(t *testing.T) {
		db, err := Open(filepath.Join(t.TempDir(), "newdb"), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		tx := newTestTransaction(t, db, testTxNum)
		if got := countTables(t, db, tx); got != 4 {
			t.Errorf("the table catalog lists %d tables, want 4 (itself and the field, view and index catalogs)", got)
		}
	})

	// Making the catalogs again would list every catalog a second time, which
	// is how a database opened as new when it was not would show.
	t.Run("given a database that holds a table, when it is opened again, then the catalogs are left as they were", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx := newTestTransaction(t, db, testTxNum)
		schema := recordmanager.NewSchema()
		if err := schema.AddIntField("id"); err != nil {
			t.Fatalf("AddIntField() error = %v", err)
		}
		if err := db.MetadataManager().CreateTable(tx, "test", schema); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		reopened, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		tx = newTestTransaction(t, reopened, testTxNum+1)
		if got := countTables(t, reopened, tx); got != 5 {
			t.Errorf("the table catalog lists %d tables, want 5 (the four catalogs and the table)", got)
		}
	})

	// The state a crash leaves is set down directly: a value on disk written by
	// a transaction the log shows no end of, with its pre-image on the log.
	t.Run("given a database that a transaction left unfinished, when it is opened again, then what the transaction wrote is undone", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		fm := db.FileManager()
		blk, err := fm.Append("data.tbl")
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		page := filemanager.NewPageByBlockSize(testBlockSize)
		if err := page.SetInt(0, 200); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := fm.Write(blk, page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		lsn, err := recoverymanager.WriteSetIntRecordToLog(db.LogManager(), testTxNum, blk, 0, 100)
		if err != nil {
			t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
		}
		if err := db.LogManager().Flush(lsn); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}

		reopened, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		got := filemanager.NewPageByBlockSize(testBlockSize)
		if err := reopened.FileManager().Read(blk, got); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if v := got.GetInt(0); v != 100 {
			t.Errorf("the block holds %d, want 100 (the value from before the unfinished write)", v)
		}
	})
}
