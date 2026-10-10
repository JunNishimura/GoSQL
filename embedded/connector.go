package embedded

import (
	"context"
	"database/sql/driver"
	"io"

	"github.com/JunNishimura/GoSQL/server"
)

// Connector hands out connections to one database, opened when the connector
// is made and closed when it is.
type Connector struct {
	db *server.DB
}

// database/sql closes a connector that is an io.Closer when the sql.DB made
// from it is closed, which is the only point at which the database can be.
var (
	_ driver.Connector = (*Connector)(nil)
	_ io.Closer        = (*Connector)(nil)
)

// NewConnector opens the database kept in dir, with blocks of blockSize bytes
// and a pool of numBuffers buffers, and returns a connector to it.
func NewConnector(dir string, blockSize, numBuffers int) (*Connector, error) {
	db, err := server.Open(dir, blockSize, numBuffers)
	if err != nil {
		return nil, err
	}

	return &Connector{db: db}, nil
}

// Connect returns a connection to the connector's database.
func (c *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	return &Conn{db: c.db}, nil
}

// Driver returns the driver the connector belongs to.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the connector's database.
func (c *Connector) Close() error {
	return c.db.Close()
}
