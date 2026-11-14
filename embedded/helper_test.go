package embedded

import (
	"database/sql/driver"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/JunNishimura/GoSQL/server"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	// testBlockSize must fit a slot of the largest catalog, which is the view
	// catalog with 476 bytes. A record page rejects a layout whose slot does
	// not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
)

// openFDs returns the number of files that the test process has open. It
// skips the test on a system that does not list them under /proc.
func openFDs(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}

	return len(entries)
}

// newTestConnector returns a connector to a database of its own. It does not
// use NewConnector, so that a test of the other methods of the connector does
// not depend on that function.
func newTestConnector(t *testing.T) *Connector {
	t.Helper()

	db, err := server.Open(t.TempDir(), testBlockSize, testNumBuffers)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return &Connector{db: db}
}

// executeUpdates runs every statement in sqls through the planner of db, in
// tx.
func executeUpdates(t *testing.T, db *server.DB, tx *transaction.Transaction, sqls ...string) {
	t.Helper()

	for _, sql := range sqls {
		if _, err := db.Planner().ExecuteUpdate(sql, tx); err != nil {
			t.Fatalf("ExecuteUpdate(%q) error = %v", sql, err)
		}
	}
}

// idsIn returns the id of every record of the table test, read in tx.
func idsIn(t *testing.T, db *server.DB, tx *transaction.Transaction) []int32 {
	t.Helper()

	p, err := db.Planner().CreateQueryPlan("select id from test", tx)
	if err != nil {
		t.Fatalf("CreateQueryPlan() error = %v", err)
	}
	s, err := p.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

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

// committedIDs returns the id of every record of the table test. It reads
// them in a separate transaction that it then commits.
func committedIDs(t *testing.T, db *server.DB) []int32 {
	t.Helper()

	tx, err := db.NewTransaction()
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	ids := idsIn(t, db, tx)
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	return ids
}

// newTestConnWithTable returns a connection to a database of its own. The
// database holds the committed, empty table test. The connection has an open
// transaction that was set without a call to Begin.
func newTestConnWithTable(t *testing.T) *Conn {
	t.Helper()

	c := newTestConnector(t)
	setup, err := c.db.NewTransaction()
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	executeUpdates(t, c.db, setup, "create table test (id int)")
	if err := setup.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	tx, err := c.db.NewTransaction()
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	return &Conn{db: c.db, tx: tx}
}

// maxTestRows limits the number of records that readAll reads. Rows that
// never return io.EOF then fail the test instead of blocking it.
const maxTestRows = 100

// readAll reads every record that rows returns.
func readAll(t *testing.T, rows driver.Rows) [][]driver.Value {
	t.Helper()

	var records [][]driver.Value
	for range maxTestRows {
		dest := make([]driver.Value, len(rows.Columns()))
		err := rows.Next(dest)
		if errors.Is(err, io.EOF) {
			return records
		}
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		records = append(records, dest)
	}
	t.Fatalf("Next() gave more than %d records without io.EOF", maxTestRows)
	return nil
}
