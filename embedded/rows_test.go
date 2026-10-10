package embedded

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/transaction"
)

// newTestRows returns the rows of sql run on c's database in tx, made without
// going through QueryContext. autoTx is what the rows hold as the transaction
// to commit when closed.
func newTestRows(t *testing.T, c *Conn, tx *transaction.Transaction, sql string, autoTx *transaction.Transaction) *Rows {
	t.Helper()

	rows, err := c.openRows(sql, tx, autoTx)
	if err != nil {
		t.Fatalf("openRows(%q) error = %v", sql, err)
	}

	return rows
}

func TestConnQueryContext(t *testing.T) {
	// The query holds shared locks on what it read until its transaction ends,
	// so a writer after it waits on them unless closing the rows committed it.
	t.Run("given a connection with no transaction and a table holding a record, when it is queried, then the rows give the record and closing them leaves a later transaction free to write the table", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)
		executeAndCommit(t, c, "insert into test (id) values (7)")

		rows, err := c.QueryContext(context.Background(), "select id from test", nil)
		if err != nil {
			t.Fatalf("QueryContext() error = %v", err)
		}
		if rows == nil {
			t.Fatal("QueryContext() = nil, want rows")
		}
		want := [][]driver.Value{
			{int64(7)},
		}
		if got := readAll(t, rows); !reflect.DeepEqual(got, want) {
			t.Errorf("the rows give %v, want %v", got, want)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if c.tx != nil {
			t.Fatal("the connection holds a transaction after the rows were closed, want none")
		}
		executeAndCommit(t, c, "insert into test (id) values (8)")
	})

	t.Run("given a connection with a transaction begun that put in a record, when it is queried, then the rows give the record and closing them leaves the transaction on the connection", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx, "insert into test (id) values (7)")
		tx := c.tx

		rows, err := c.QueryContext(context.Background(), "select id from test", nil)
		if err != nil {
			t.Fatalf("QueryContext() error = %v", err)
		}
		if rows == nil {
			t.Fatal("QueryContext() = nil, want rows")
		}
		want := [][]driver.Value{
			{int64(7)},
		}
		if got := readAll(t, rows); !reflect.DeepEqual(got, want) {
			t.Errorf("the rows give %v, want %v", got, want)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if c.tx != tx {
			t.Error("the connection's transaction changed when the rows were closed, want it left as it was")
		}
	})

	// Planning reads the catalogs under shared locks, so a query that fails to
	// plan has to end its transaction too, or making a table afterwards waits.
	t.Run("given a connection with no transaction, when a table that does not exist is queried, then it reports an error and a later transaction can make a table", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)

		_, err := c.QueryContext(context.Background(), "select id from missing", nil)

		if err == nil {
			t.Fatal("QueryContext() error = nil, want an error")
		}
		if c.tx != nil {
			t.Fatal("the connection holds a transaction after QueryContext, want none")
		}
		executeAndCommit(t, c, "create table other (id int)")
	})

	t.Run("given a connection with no transaction, when a query is run with arguments, then it reports ErrArgsUnsupported", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)
		args := []driver.NamedValue{
			{
				Ordinal: 1,
				Value:   int64(7),
			},
		}

		_, err := c.QueryContext(context.Background(), "select id from test", args)

		if !errors.Is(err, ErrArgsUnsupported) {
			t.Errorf("QueryContext() error = %v, want %v", err, ErrArgsUnsupported)
		}
	})
}

func TestRowsColumns(t *testing.T) {
	t.Run("given the rows of a query selecting name then id, then they name the columns in that order", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx, "create table people (id int, name varchar(10))")
		rows := newTestRows(t, c, c.tx, "select name, id from people", nil)
		defer rows.Close()

		if got := rows.Columns(); !slices.Equal(got, []string{"name", "id"}) {
			t.Errorf("Columns() = %v, want [name id]", got)
		}
	})
}

func TestRowsNext(t *testing.T) {
	t.Run("given the rows of a query over two records with an int and a varchar field, then they give each record, the int as an int64 and the varchar as a string, and then io.EOF", func(t *testing.T) {
		c := newTestConnWithTable(t)
		executeUpdates(t, c.db, c.tx,
			"create table people (id int, name varchar(10))",
			"insert into people (id, name) values (7, 'ann')",
			"insert into people (id, name) values (8, 'bob')",
		)
		rows := newTestRows(t, c, c.tx, "select name, id from people", nil)
		defer rows.Close()

		want := [][]driver.Value{
			{"ann", int64(7)},
			{"bob", int64(8)},
		}
		if got := readAll(t, rows); !reflect.DeepEqual(got, want) {
			t.Errorf("the rows give %v, want %v", got, want)
		}
	})
}

func TestRowsClose(t *testing.T) {
	t.Run("given the rows of a query run in a transaction of their own, when they are closed, then they hold it no longer and a later transaction can write the table", func(t *testing.T) {
		c := newTestConnWithTableOutsideTx(t)
		executeAndCommit(t, c, "insert into test (id) values (7)")
		tx, err := c.db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		// Opening the scan takes a shared lock on the table's first block,
		// which is all the write below needs to be kept waiting.
		rows := newTestRows(t, c, tx, "select id from test", tx)

		if err := rows.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Checked first, since a transaction left uncommitted would hold the
		// locks the write below has to wait for.
		if rows.autoTx != nil {
			t.Fatal("the rows still hold their transaction after Close")
		}
		executeAndCommit(t, c, "insert into test (id) values (8)")
	})
}

func TestRowsColumnTypeDatabaseTypeName(t *testing.T) {
	tests := []struct {
		name  string
		index int
		want  string
	}{
		{
			name:  "given the rows of a query selecting a varchar then an int, when the type of the varchar column is asked for, then it is VARCHAR",
			index: 0,
			want:  "VARCHAR",
		},
		{
			name:  "given the rows of a query selecting a varchar then an int, when the type of the int column is asked for, then it is INT",
			index: 1,
			want:  "INT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestConnWithTable(t)
			executeUpdates(t, c.db, c.tx, "create table people (id int, name varchar(10))")
			rows := newTestRows(t, c, c.tx, "select name, id from people", nil)
			defer rows.Close()

			if got := rows.ColumnTypeDatabaseTypeName(tt.index); got != tt.want {
				t.Errorf("ColumnTypeDatabaseTypeName(%d) = %q, want %q", tt.index, got, tt.want)
			}
		})
	}
}

func TestRowsColumnTypeLength(t *testing.T) {
	tests := []struct {
		name       string
		index      int
		wantLength int64
		wantOK     bool
	}{
		{
			name:       "given the rows of a query selecting a varchar(10) then an int, when the length of the varchar column is asked for, then it is 10",
			index:      0,
			wantLength: 10,
			wantOK:     true,
		},
		{
			name:       "given the rows of a query selecting a varchar(10) then an int, when the length of the int column is asked for, then it has none",
			index:      1,
			wantLength: 0,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestConnWithTable(t)
			executeUpdates(t, c.db, c.tx, "create table people (id int, name varchar(10))")
			rows := newTestRows(t, c, c.tx, "select name, id from people", nil)
			defer rows.Close()

			length, ok := rows.ColumnTypeLength(tt.index)
			if length != tt.wantLength || ok != tt.wantOK {
				t.Errorf("ColumnTypeLength(%d) = %d, %v, want %d, %v", tt.index, length, ok, tt.wantLength, tt.wantOK)
			}
		})
	}
}
