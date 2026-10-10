// Package embedded is a database/sql driver that runs GoSQL in the calling
// process.
package embedded

import (
	"database/sql"
	"database/sql/driver"
	"errors"
)

const (
	// DefaultBlockSize is the block size of a database opened by name.
	DefaultBlockSize = 4096
	// DefaultNumBuffers is the size of the buffer pool of a database opened by
	// name.
	DefaultNumBuffers = 32
)

// ErrOpenUnsupported is reported by Driver.Open.
var ErrOpenUnsupported = errors.New("embedded: Open is not supported; open connections through OpenConnector")

func init() {
	sql.Register("gosql", Driver{})
}

// Driver opens databases by the name of the directory they are kept in.
type Driver struct{}

var (
	_ driver.Driver        = Driver{}
	_ driver.DriverContext = Driver{}
)

// Open is not supported; connections come from the connector OpenConnector
// returns, which database/sql uses in place of Open.
//
// A connection opened on its own would need a database of its own: a second
// buffer pool and lock table over the same files as any other, and nothing
// left holding the database to close it.
func (Driver) Open(name string) (driver.Conn, error) {
	return nil, ErrOpenUnsupported
}

// OpenConnector opens the database kept in the directory name, with
// DefaultBlockSize and DefaultNumBuffers, and returns a connector to it.
func (Driver) OpenConnector(name string) (driver.Connector, error) {
	c, err := NewConnector(name, DefaultBlockSize, DefaultNumBuffers)
	if err != nil {
		// Returned as it is, the nil *Connector would make a driver.Connector
		// that is not nil.
		return nil, err
	}

	return c, nil
}
