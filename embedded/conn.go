package embedded

import (
	"context"
	"database/sql/driver"
	"errors"

	"github.com/JunNishimura/GoSQL/server"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrTxInProgress is reported by Begin on a connection that already has a
// transaction begun on it.
var ErrTxInProgress = errors.New("embedded: a transaction is already in progress on the connection")

// Conn is a connection to a database. It runs statements in the transaction
// begun on it, if there is one.
type Conn struct {
	db *server.DB
	tx *transaction.Transaction
}

// ErrArgsUnsupported is reported for a statement run with arguments, which the
// parser has no placeholders to take.
var ErrArgsUnsupported = errors.New("embedded: statements with arguments are not supported")

// ErrLastInsertIdUnsupported is reported by Result.LastInsertId.
var ErrLastInsertIdUnsupported = errors.New("embedded: LastInsertId is not supported")

var (
	_ driver.Conn          = (*Conn)(nil)
	_ driver.ExecerContext = (*Conn)(nil)
)

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return nil, nil
}

// ExecContext runs a statement that changes the database. On a connection with
// a transaction begun, it runs in that transaction; otherwise it runs in one of
// its own, committed if the statement succeeds and rolled back if it fails.
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

// Close rolls back the transaction begun on the connection, if there is one.
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

// commit commits the transaction begun on the connection and leaves the
// connection without one. A failed commit leaves the transaction on the
// connection, since it has not ended.
func (c *Conn) commit() error {
	if err := c.tx.Commit(); err != nil {
		return err
	}
	c.tx = nil

	return nil
}

// rollback rolls back the transaction begun on the connection and leaves the
// connection without one. A failed rollback leaves the transaction on the
// connection, since it has not ended.
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

// Commit commits the transaction and leaves the connection without one.
func (tx *Tx) Commit() error {
	return tx.conn.commit()
}

// Rollback rolls back the transaction and leaves the connection without one.
func (tx *Tx) Rollback() error {
	return tx.conn.rollback()
}

// Result reports what a statement that changed the database did.
type Result struct {
	rowsAffected int64
}

var _ driver.Result = Result{}

// LastInsertId is not supported, since a table has no column the database fills
// in on its own.
func (r Result) LastInsertId() (int64, error) {
	return 0, ErrLastInsertIdUnsupported
}

// RowsAffected reports how many records the statement changed.
func (r Result) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}
