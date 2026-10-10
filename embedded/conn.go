package embedded

import (
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

var _ driver.Conn = (*Conn)(nil)

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return nil, nil
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
