package embedded

import (
	"database/sql/driver"

	"github.com/JunNishimura/GoSQL/server"
)

// Conn is a connection to a database.
type Conn struct {
	db *server.DB
}

var _ driver.Conn = (*Conn)(nil)

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return nil, nil
}

func (c *Conn) Close() error {
	return nil
}

func (c *Conn) Begin() (driver.Tx, error) {
	return nil, nil
}
