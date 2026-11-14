package embedded

import (
	"context"
	"database/sql/driver"
	"io"

	"github.com/JunNishimura/GoSQL/server"
)

// Connector creates connections to one database. The database is opened when
// the connector is created and closed when the connector is closed.
type Connector struct {
	db *server.DB
}

// database/sql closes a connector that implements io.Closer when the sql.DB
// created from it is closed. This is the only point at which the database can
// be closed.
var (
	_ driver.Connector = (*Connector)(nil)
	_ io.Closer        = (*Connector)(nil)
)

// NewConnector opens the database stored in dir, with blocks of blockSize
// bytes and a pool of numBuffers buffers, and returns a connector to it.
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

// Driver returns the driver that the connector belongs to.
func (c *Connector) Driver() driver.Driver {
	return Driver{}
}

// Close closes the connector's database.
func (c *Connector) Close() error {
	return c.db.Close()
}
