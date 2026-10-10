package embedded

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDriverOpenConnector(t *testing.T) {
	t.Run("when a connector is opened on a name, then it is to a database in the directory of that name, with the default block size", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")

		got, err := Driver{}.OpenConnector(dir)
		if err != nil {
			t.Fatalf("OpenConnector() error = %v", err)
		}

		c, ok := got.(*Connector)
		if !ok || c == nil || c.db == nil {
			t.Fatalf("OpenConnector() = %#v, want a *Connector with a database", got)
		}
		if blockSize := c.db.FileManager().BlockSize(); blockSize != DefaultBlockSize {
			t.Errorf("the database has a block size of %d, want %d", blockSize, DefaultBlockSize)
		}
	})
}

func TestDriverOpen(t *testing.T) {
	// A connection opened on its own would need a database of its own, which
	// nothing would be left holding to close.
	t.Run("when a connection is opened on a name directly, then it reports an error", func(t *testing.T) {
		conn, err := Driver{}.Open(t.TempDir())

		if err == nil {
			t.Errorf("Open() = %v, nil, want an error", conn)
		}
	})
}

// TestRegister checks the driver through database/sql, which is how it is
// meant to be used.
func TestRegister(t *testing.T) {
	t.Run("given the driver registered as gosql, when a database is opened through database/sql and pinged, then a connection is made", func(t *testing.T) {
		db, err := sql.Open("gosql", t.TempDir())
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		defer db.Close()

		if err := db.Ping(); err != nil {
			t.Errorf("Ping() error = %v", err)
		}
	})

}
