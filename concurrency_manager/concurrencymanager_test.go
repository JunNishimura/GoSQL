package concurrencymanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

func TestNewConcurrencyManager(t *testing.T) {
	lt := NewLockTable()

	cm := NewConcurrencyManager(lt)

	if cm == nil {
		t.Fatal("NewConcurrencyManager() = nil, want non-nil")
	}
	if cm.lockTable != lt {
		t.Errorf("lockTable = %v, want %v", cm.lockTable, lt)
	}
	if cm.locks == nil {
		t.Fatal("locks is nil, want an initialized map")
	}
	if got := len(cm.locks); got != 0 {
		t.Errorf("len(locks) = %d, want 0", got)
	}
}

// Every transaction gets its own concurrency manager, and they coordinate
// through one shared lock table. Each has to remember only the locks it took:
// a shared record would let a transaction release a lock another one holds.
func TestNewConcurrencyManagerKeepsItsOwnRecords(t *testing.T) {
	lt := NewLockTable()

	first := NewConcurrencyManager(lt)
	second := NewConcurrencyManager(lt)

	if first.lockTable != second.lockTable {
		t.Error("the two managers point at different lock tables, want the same one")
	}

	first.locks[*filemanager.NewBlockId(testDataFile, 0)] = sharedLock

	if got := len(second.locks); got != 0 {
		t.Errorf("len(locks) of the second manager = %d, want 0", got)
	}
}

// The zero value of lockType has to mean "not held", so that a lookup for a
// block this transaction never locked reads correctly without a presence check.
func TestNoLockIsTheZeroValue(t *testing.T) {
	var held lockType

	if held != noLock {
		t.Errorf("the zero value of lockType = %d, want %d (noLock)", held, noLock)
	}
	if noLock == sharedLock || noLock == exclusiveLock {
		t.Error("noLock must be distinct from the locks a transaction can hold")
	}
	if sharedLock == exclusiveLock {
		t.Error("sharedLock and exclusiveLock must be distinct")
	}
}
