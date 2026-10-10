package embedded

import (
	"os"
	"testing"

	"github.com/JunNishimura/GoSQL/server"
)

const (
	// testBlockSize has to hold a slot of the widest catalog, which is the view
	// catalog at 476 bytes, since a record page refuses a layout whose slot
	// does not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
)

// openFDs reports how many files the test process has open, skipping the test
// where the system does not list them under /proc.
func openFDs(t *testing.T) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}

	return len(entries)
}

// newTestConnector returns a connector to a database of its own, opened
// without going through NewConnector, so that a test of the connector's other
// methods does not hang on that one.
func newTestConnector(t *testing.T) *Connector {
	t.Helper()

	db, err := server.Open(t.TempDir(), testBlockSize, testNumBuffers)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return &Connector{db: db}
}
