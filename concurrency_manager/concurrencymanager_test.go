package concurrencymanager

import (
	"errors"
	"testing"
	"time"

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

func TestConcurrencyManagerSLock(t *testing.T) {
	tests := []struct {
		name string
		// held is what this transaction has already recorded for the block.
		held lockType
		// tableHeld is the count already in the lock table; 0 means no entry.
		tableHeld int
		wantHeld  lockType
		wantTable int
	}{
		{
			name:      "takes a shared lock on a block it holds nothing on",
			held:      noLock,
			tableHeld: 0,
			wantHeld:  sharedLock,
			wantTable: 1,
		},
		{
			// Going to the table again would count the same transaction twice,
			// which would make an exclusive request wait for a lock nobody holds.
			name:      "does not take a second shared lock on a block it already holds",
			held:      sharedLock,
			tableHeld: 1,
			wantHeld:  sharedLock,
			wantTable: 1,
		},
		{
			// An exclusive lock already allows everything a shared one does.
			name:      "leaves an exclusive lock it already holds alone",
			held:      exclusiveLock,
			tableHeld: -1,
			wantHeld:  exclusiveLock,
			wantTable: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt := NewLockTable()
			cm := NewConcurrencyManager(lt)
			blk := filemanager.NewBlockId(testDataFile, 0)
			if tt.held != noLock {
				cm.locks[*blk] = tt.held
			}
			if tt.tableHeld != 0 {
				lt.locks[*blk] = tt.tableHeld
			}

			if err := cm.SLock(blk); err != nil {
				t.Fatalf("SLock() error = %v", err)
			}

			if got := cm.locks[*blk]; got != tt.wantHeld {
				t.Errorf("locks[%v] = %d, want %d", blk, got, tt.wantHeld)
			}
			if got := lt.locks[*blk]; got != tt.wantTable {
				t.Errorf("lockTable.locks[%v] = %d, want %d", blk, got, tt.wantTable)
			}
		})
	}
}

func TestConcurrencyManagerSLockIsPerBlock(t *testing.T) {
	lt := NewLockTable()
	cm := NewConcurrencyManager(lt)
	first := filemanager.NewBlockId(testDataFile, 0)
	second := filemanager.NewBlockId(testDataFile, 1)

	if err := cm.SLock(first); err != nil {
		t.Fatalf("SLock() error = %v", err)
	}
	if err := cm.SLock(second); err != nil {
		t.Fatalf("SLock() error = %v", err)
	}

	if got := cm.locks[*first]; got != sharedLock {
		t.Errorf("locks[%v] = %d, want %d", first, got, sharedLock)
	}
	if got := cm.locks[*second]; got != sharedLock {
		t.Errorf("locks[%v] = %d, want %d", second, got, sharedLock)
	}
}

// A lock that could not be taken must not be recorded, or the transaction would
// later release a lock it never held and believe it may read the block.
func TestConcurrencyManagerSLockRecordsNothingWhenTheLockIsRefused(t *testing.T) {
	lt := NewLockTable()
	lt.maxWaitTime = 50 * time.Millisecond
	cm := NewConcurrencyManager(lt)
	blk := filemanager.NewBlockId(testDataFile, 0)
	// Another transaction holds the block exclusively and never lets go.
	lt.locks[*blk] = -1

	err := cm.SLock(blk)

	if !errors.Is(err, ErrLockAbort) {
		t.Errorf("SLock() error = %v, want %v", err, ErrLockAbort)
	}
	if _, present := cm.locks[*blk]; present {
		t.Errorf("locks[%v] has an entry, want none", blk)
	}
}
