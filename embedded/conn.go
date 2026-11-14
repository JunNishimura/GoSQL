package embedded

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/JunNishimura/GoSQL/server"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrTxInProgress is returned by Begin on a connection that already has an
// open transaction.
var ErrTxInProgress = errors.New("embedded: a transaction is already in progress on the connection")

// Conn is a connection to a database. It runs statements in the transaction
// that was begun on it, if there is one.
type Conn struct {
	db *server.DB
	tx *transaction.Transaction
}

// ErrArgsUnsupported is returned for a statement that is run with arguments.
// The parser supports no placeholders.
var ErrArgsUnsupported = errors.New("embedded: statements with arguments are not supported")

// ErrLastInsertIdUnsupported is returned by Result.LastInsertId.
var ErrLastInsertIdUnsupported = errors.New("embedded: LastInsertId is not supported")

var (
	_ driver.Conn           = (*Conn)(nil)
	_ driver.ExecerContext  = (*Conn)(nil)
	_ driver.QueryerContext = (*Conn)(nil)
)

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return nil, nil
}

// ExecContext runs a statement that changes the database. If the connection
// has an open transaction, the statement runs in it. Otherwise it runs in a
// transaction of its own, which is committed if the statement succeeds and
// rolled back if it fails.
func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) > 0 {
		return nil, ErrArgsUnsupported
	}

	if c.tx != nil {
		n, err := c.db.Planner().ExecuteUpdate(query, c.tx)
		if err != nil {
			return nil, err
		}
		return Result{rowsAffected: int64(n)}, nil
	}

	tx, err := c.db.NewTransaction()
	if err != nil {
		return nil, err
	}
	n, err := c.db.Planner().ExecuteUpdate(query, tx)
	if err != nil {
		return nil, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return Result{rowsAffected: int64(n)}, nil
}

// QueryContext runs a select statement. If the connection has an open
// transaction, the query runs in it. Otherwise it runs in a transaction of
// its own. That transaction is committed when the rows are closed, or rolled
// back if the query cannot be planned or opened.
func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		return nil, ErrArgsUnsupported
	}

	if c.tx != nil {
		rows, err := c.openRows(query, c.tx, nil)
		if err != nil {
			// A nil *Rows returned directly would become a non-nil driver.Rows
			// interface value.
			return nil, err
		}
		return rows, nil
	}

	tx, err := c.db.NewTransaction()
	if err != nil {
		return nil, err
	}
	rows, err := c.openRows(query, tx, tx)
	if err != nil {
		return nil, errors.Join(err, tx.Rollback())
	}

	return rows, nil
}

// openRows plans query in tx and opens its scan. It passes autoTx to the
// rows, which commit it when they are closed.
func (c *Conn) openRows(query string, tx, autoTx *transaction.Transaction) (*Rows, error) {
	p, err := c.db.Planner().CreateQueryPlan(query, tx)
	if err != nil {
		return nil, err
	}
	s, err := p.Open()
	if err != nil {
		return nil, err
	}

	return &Rows{
		scan:    s,
		columns: p.Schema().Fields(),
		schema:  p.Schema(),
		autoTx:  autoTx,
	}, nil
}

// Close rolls back the open transaction of the connection, if there is one.
func (c *Conn) Close() error {
	if c.tx == nil {
		return nil
	}

	return c.rollback()
}

// Begin begins a transaction on the connection.
func (c *Conn) Begin() (driver.Tx, error) {
	if c.tx != nil {
		return nil, ErrTxInProgress
	}

	tx, err := c.db.NewTransaction()
	if err != nil {
		return nil, err
	}
	c.tx = tx

	return &Tx{conn: c}, nil
}

// commit commits the open transaction of the connection and clears it. After
// a failed commit the transaction stays on the connection, because it has
// not ended.
func (c *Conn) commit() error {
	if err := c.tx.Commit(); err != nil {
		return err
	}
	c.tx = nil

	return nil
}

// rollback rolls back the open transaction of the connection and clears it.
// After a failed rollback the transaction stays on the connection, because it
// has not ended.
func (c *Conn) rollback() error {
	if err := c.tx.Rollback(); err != nil {
		return err
	}
	c.tx = nil

	return nil
}

// Tx is a transaction begun on a connection.
type Tx struct {
	conn *Conn
}

var _ driver.Tx = (*Tx)(nil)

// Commit commits the transaction and clears it from the connection.
func (tx *Tx) Commit() error {
	return tx.conn.commit()
}

// Rollback rolls back the transaction and clears it from the connection.
func (tx *Tx) Rollback() error {
	return tx.conn.rollback()
}

// Result holds the outcome of a statement that changed the database.
type Result struct {
	rowsAffected int64
}

var _ driver.Result = Result{}

// LastInsertId is not supported, because a table has no column that the
// database fills in automatically.
func (r Result) LastInsertId() (int64, error) {
	return 0, ErrLastInsertIdUnsupported
}

// RowsAffected returns the number of records that the statement changed.
func (r Result) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}
