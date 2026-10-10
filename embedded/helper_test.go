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
	// testBlockSize has to hold a slot of the widest catalog, which is the view
	// catalog at 476 bytes, since a record page refuses a layout whose slot
	// does not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
)

// openFDs reports how many files the test process has open, skipping the test
// where the system does not list them under /proc.
func openFDs(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}

	return len(entries)
}

// newTestConnector returns a connector to a database of its own, opened
// without going through NewConnector, so that a test of the connector's other
// methods does not hang on that one.
func newTestConnector(t *testing.T) *Connector {
	t.Helper()

	db, err := server.Open(t.TempDir(), testBlockSize, testNumBuffers)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return &Connector{db: db}
}

// executeUpdates runs every statement in sqls through db's planner in tx.
func executeUpdates(t *testing.T, db *server.DB, tx *transaction.Transaction, sqls ...string) {
	t.Helper()

	for _, sql := range sqls {
		if _, err := db.Planner().ExecuteUpdate(sql, tx); err != nil {
			t.Fatalf("ExecuteUpdate(%q) error = %v", sql, err)
		}
	}
}

// idsIn reports the id of every record of the table test, read in tx.
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

// committedIDs reports the id of every record of the table test, read in a
// transaction of its own that it then commits.
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

// newTestConnWithTable returns a connection to a database of its own that holds
// the committed, empty table test, with a transaction begun on the connection
// without going through Begin.
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

// maxTestRows bounds how many records readAll reads, so that rows that never
// report io.EOF fail the test rather than hang it.
const maxTestRows = 100

// readAll reads every record rows gives back.
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
