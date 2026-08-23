package concurrencymanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const testDataFile = "test.tbl"

func TestNewLockTable(t *testing.T) {
	lt := NewLockTable()

	if lt == nil {
		t.Fatal("NewLockTable() = nil, want non-nil")
	}
	if lt.locks == nil {
		t.Fatal("locks is nil, want an initialized map")
	}
	if got := len(lt.locks); got != 0 {
		t.Errorf("len(locks) = %d, want 0", got)
	}
}

// Each lock table owns its own map. A table shared between callers would let
// one of them see, and release, locks it never took.
func TestNewLockTableDoesNotShareLocks(t *testing.T) {
	first := NewLockTable()
	second := NewLockTable()

	first.locks[*filemanager.NewBlockId(testDataFile, 0)] = 1

	if got := len(second.locks); got != 0 {
		t.Errorf("len(locks) of the second table = %d, want 0", got)
	}
}
