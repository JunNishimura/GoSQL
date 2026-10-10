package embedded

import (
	"context"
	"database/sql/driver"
	"errors"
	"slices"
	"testing"
)

func TestConnBegin(t *testing.T) {
	t.Run("given a connection with no transaction, when one is begun, then the connection holds it", func(t *testing.T) {
		c := &Conn{db: newTestConnector(t).db}

		tx, err := c.Begin()
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}

		if tx == nil {
			t.Error("Begin() = nil, want a transaction")
		}
		if c.tx == nil {
			t.Error("the connection holds no transaction after Begin")
		}
	})

	// database/sql never begins a second transaction on a connection before the
	// first ends, so one that does is a mistake to report rather than one to
	// quietly leave the first transaction behind for.
	t.Run("given a connection with a transaction begun, when another is begun, then it reports an error", func(t *testing.T) {
		c := newTestConnWithTable(t)

		if _, err := c.Begin(); err == nil {
			t.Error("Begin() error = nil, want an error")
		}
	})
}

func TestTxCommit(t *testing.T) {
	t.Run("given a record put in through a transaction begun on a connection, when the transaction commits, then the connection is left without one and a later transaction reads the record", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx, "insert into test (id) values (7)")

		if err := (&Tx{conn: c}).Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		// Checked first, since a transaction left on the connection would hold
		// the locks the read below has to wait for.
		if c.tx != nil {
			t.Fatal("the connection still holds a transaction after Commit")
		}
		if got := committedIDs(t, c.db); !slices.Equal(got, []int32{7}) {
			t.Errorf("a later transaction reads ids %v, want [7]", got)
		}
	})
}

func TestTxRollback(t *testing.T) {
	t.Run("given a record put in through a transaction begun on a connection, when the transaction rolls back, then the connection is left without one and a later transaction does not read the record", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx, "insert into test (id) values (7)")

		if err := (&Tx{conn: c}).Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		if c.tx != nil {
			t.Fatal("the connection still holds a transaction after Rollback")
		}
		if got := committedIDs(t, c.db); len(got) != 0 {
			t.Errorf("a later transaction reads ids %v, want none", got)
		}
	})
}

func TestConnClose(t *testing.T) {
	// database/sql closes a connection without ending its transaction when the
	// connection goes bad or the pool shrinks, and nothing else would end it.
	t.Run("given a connection with a record put in through a transaction begun on it, when the connection is closed, then the transaction is rolled back", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx, "insert into test (id) values (7)")

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if c.tx != nil {
			t.Fatal("the connection still holds a transaction after Close")
		}
		if got := committedIDs(t, c.db); len(got) != 0 {
			t.Errorf("a later transaction reads ids %v, want none", got)
		}
	})
}

// newTestConnWithTableOutsideTx returns a connection to a database of its own
// that holds the committed, empty table test, with no transaction begun on it.
func newTestConnWithTableOutsideTx(t *testing.T) *Conn {
	t.Helper()

	c := newTestConnWithTable(t)
	if err := c.rollback(); err != nil {
		t.Fatalf("rollback() error = %v", err)
	}

	return c
}

func TestConnExecContext(t *testing.T) {
	t.Run("given a connection with no transaction, when an insert is executed, then it reports one row affected and a later transaction reads the record", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)

		res, err := c.ExecContext(context.Background(), "insert into test (id) values (7)", nil)
		if err != nil {
			t.Fatalf("ExecContext() error = %v", err)
		}

		if res == nil {
			t.Fatal("ExecContext() = nil, want a result")
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			t.Errorf("RowsAffected() = %d, %v, want 1, nil", n, err)
		}
		// Checked before the read below, which would wait on the locks of a
		// transaction left behind.
		if c.tx != nil {
			t.Fatal("the connection holds a transaction after ExecContext, want none")
		}
		if got := committedIDs(t, c.db); !slices.Equal(got, []int32{7}) {
			t.Errorf("a later transaction reads ids %v, want [7]", got)
		}
	})

	// The insert fails on the value after taking a slot for the record, so what
	// it leaves behind has to be rolled back rather than committed with it.
	t.Run("given a connection with no transaction, when an insert that fails partway is executed, then it reports the error and a later transaction reads no record", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)

		_, err := c.ExecContext(context.Background(), "insert into test (id) values ('x')", nil)

		if err == nil {
			t.Fatal("ExecContext() error = nil, want an error")
		}
		if c.tx != nil {
			t.Fatal("the connection holds a transaction after ExecContext, want none")
		}
		if got := committedIDs(t, c.db); len(got) != 0 {
			t.Errorf("a later transaction reads ids %v, want none", got)
		}
	})

	t.Run("given a connection with a transaction begun, when an insert is executed, then the record is in that transaction and goes when it rolls back", func(t *testing.T) {
		c := newTestConnWithTable(t)

		if _, err := c.ExecContext(context.Background(), "insert into test (id) values (7)", nil); err != nil {
			t.Fatalf("ExecContext() error = %v", err)
		}

		if got := idsIn(t, c.db, c.tx); !slices.Equal(got, []int32{7}) {
			t.Errorf("the transaction reads ids %v, want [7]", got)
		}
		if err := c.rollback(); err != nil {
			t.Fatalf("rollback() error = %v", err)
		}
		if got := committedIDs(t, c.db); len(got) != 0 {
			t.Errorf("a later transaction reads ids %v, want none", got)
		}
	})

	t.Run("given a connection with no transaction, when a statement is executed with arguments, then it reports ErrArgsUnsupported", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)
		args := []driver.NamedValue{
			{
				Ordinal: 1,
				Value:   int64(7),
			},
		}

		_, err := c.ExecContext(context.Background(), "insert into test (id) values (7)", args)

		if !errors.Is(err, ErrArgsUnsupported) {
			t.Errorf("ExecContext() error = %v, want %v", err, ErrArgsUnsupported)
		}
	})
}

func TestResultLastInsertId(t *testing.T) {
	t.Run("it reports an error", func(t *testing.T) {
		if _, err := (Result{}).LastInsertId(); err == nil {
			t.Error("LastInsertId() error = nil, want an error")
		}
	})
}
