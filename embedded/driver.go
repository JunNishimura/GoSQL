// Package embedded is a database/sql driver that runs GoSQL inside the
// calling process.
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

// ErrOpenUnsupported is returned by Driver.Open.
var ErrOpenUnsupported = errors.New("embedded: Open is not supported; open connections through OpenConnector")

func init() {
	sql.Register("gosql", Driver{})
}

// Driver opens databases by the name of the directory that stores them.
type Driver struct{}

var (
	_ driver.Driver        = Driver{}
	_ driver.DriverContext = Driver{}
)

// Open is not supported. Connections come from the connector that
// OpenConnector returns, and database/sql uses that connector instead of
// Open.
//
// A connection opened on its own would need a database of its own. That
// database would be a second buffer pool and lock table over the same files,
// and nothing would hold it to close it.
func (Driver) Open(name string) (driver.Conn, error) {
	return nil, ErrOpenUnsupported
}

// OpenConnector opens the database stored in the directory name, with
// DefaultBlockSize and DefaultNumBuffers, and returns a connector to it.
func (Driver) OpenConnector(name string) (driver.Connector, error) {
	c, err := NewConnector(name, DefaultBlockSize, DefaultNumBuffers)
	if err != nil {
		// A nil *Connector returned directly would become a non-nil
		// driver.Connector interface value.
		return nil, err
	}

	return c, nil
}
