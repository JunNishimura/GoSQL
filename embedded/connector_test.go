package embedded

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewConnector(t *testing.T) {
	t.Run("given a database directory that does not exist, it opens a database there with the block size it was given", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")

		c, err := NewConnector(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("NewConnector() error = %v", err)
		}

		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Stat(%q) error = %v, want the directory to exist", dir, err)
		}
		if c == nil || c.db == nil {
			t.Fatal("NewConnector() gave no database, want the one opened in the directory")
		}
		if got := c.db.FileManager().BlockSize(); got != testBlockSize {
			t.Errorf("the database has a block size of %d, want %d", got, testBlockSize)
		}
	})
}

func TestConnectorConnect(t *testing.T) {
	// database/sql keeps a pool of connections from one connector, so every one
	// of them has to share the database the connector opened: two would each
	// keep a buffer pool and a lock table of their own over the same files.
	t.Run("given a connector, when two connections are made, then both are to the database the connector opened", func(t *testing.T) {
		c := newTestConnector(t)

		for i := range 2 {
			conn, err := c.Connect(context.Background())
			if err != nil {
				t.Fatalf("Connect() error = %v", err)
			}
			got, ok := conn.(*Conn)
			if !ok {
				t.Fatalf("connection %d is a %T, want a *Conn", i, conn)
			}
			if got.db == nil || got.db != c.db {
				t.Errorf("connection %d is to %p, want the connector's database %p", i, got.db, c.db)
			}
		}
	})
}

func TestConnectorDriver(t *testing.T) {
	t.Run("it is the driver of this package", func(t *testing.T) {
		c := newTestConnector(t)

		if _, ok := c.Driver().(Driver); !ok {
			t.Errorf("Driver() = %T, want a Driver", c.Driver())
		}
	})
}

func TestConnectorClose(t *testing.T) {
	t.Run("given a connector, when it is closed, then every file its database opened is closed", func(t *testing.T) {
		before := openFDs(t)
		c := newTestConnector(t)
		// Without files open to start with, the count after Close would match
		// whether or not Close closed anything.
		if opened := openFDs(t); opened <= before {
			t.Fatalf("the process has %d files open before Close, want more than the %d before the database was opened", opened, before)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if after := openFDs(t); after != before {
			t.Errorf("the process has %d files open, want %d as before the database was opened", after, before)
		}
	})
}
