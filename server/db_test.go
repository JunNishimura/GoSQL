package server

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	// testBlockSize has to hold a slot of the widest catalog, which is the view
	// catalog at 476 bytes, since a record page refuses a layout whose slot
	// does not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
)

func TestOpen(t *testing.T) {
	t.Run("given a database directory that does not exist, when it is opened, then the directory is created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "newdb")

		if _, err := Open(dir, testBlockSize, testNumBuffers); err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Stat(%q) error = %v, want the directory to exist", dir, err)
		}
	})

	t.Run("when a database is opened with a block size, then its file manager uses that block size", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if got := db.FileManager().BlockSize(); got != testBlockSize {
			t.Errorf("FileManager().BlockSize() = %d, want %d", got, testBlockSize)
		}
	})
}
