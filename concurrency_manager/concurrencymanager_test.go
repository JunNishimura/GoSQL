package concurrencymanager

import (
	"errors"
	"testing"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

func TestNewConcurrencyManager(t *testing.T) {
	t.Run("it holds the lock table it was given and starts with no locks recorded", func(t *testing.T) {
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
	})

	// Every transaction has its own concurrency manager, and all managers
	// coordinate through one shared lock table. Each manager must record only
	// the locks it took. With a shared record, a transaction could release a
	// lock that another one holds.
	t.Run("given two managers over the same lock table, when one records a lock, then the other does not see it", func(t *testing.T) {
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
	})
}

func TestLockType(t *testing.T) {
	// The zero value of lockType must mean "not held". A lookup for a block
	// that this transaction never locked then returns the right answer without
	// a presence check.
	t.Run("its zero value is noLock, and the three kinds are distinct", func(t *testing.T) {
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
	})
}

func TestConcurrencyManagerSLock(t *testing.T) {
	tests := []struct {
		name string
		// held is the lock that this transaction has already recorded for the block.
		held lockType
		// tableHeld is the count already in the lock table; 0 means no entry.
		tableHeld int
		wantHeld  lockType
		wantTable int
	}{
		{
			name:      "given a block it holds nothing on, it takes a shared lock and records it",
			held:      noLock,
			tableHeld: 0,
			wantHeld:  sharedLock,
			wantTable: 1,
		},
		{
			// A second request to the table would count the same transaction
			// twice. An exclusive request would then wait for a lock that nobody
			// holds.
			name:      "given a block it already holds a shared lock on, it does not ask the table a second time",
			held:      sharedLock,
			tableHeld: 1,
			wantHeld:  sharedLock,
			wantTable: 1,
		},
		{
			// An exclusive lock already allows everything a shared one does.
			name:      "given a block it already holds exclusively, it leaves that lock as it is",
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

	t.Run("when two blocks are locked, then each is recorded on its own rather than one standing for the other", func(t *testing.T) {
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
	})

	// A lock that was not granted must not be recorded. Otherwise the
	// transaction would assume that it may read the block, and would later
	// release a lock that it never held.
	t.Run("given a block another transaction holds exclusively, it reports ErrLockAbort and records nothing", func(t *testing.T) {
		lt := NewLockTable()
		lt.maxWaitTime = 50 * time.Millisecond
		cm := NewConcurrencyManager(lt)
		blk := filemanager.NewBlockId(testDataFile, 0)
		// Another transaction holds the block exclusively and never releases it.
		lt.locks[*blk] = -1

		err := cm.SLock(blk)

		if !errors.Is(err, ErrLockAbort) {
			t.Errorf("SLock() error = %v, want %v", err, ErrLockAbort)
		}
		if _, present := cm.locks[*blk]; present {
			t.Errorf("locks[%v] has an entry, want none", blk)
		}
	})
}

func TestConcurrencyManagerXLock(t *testing.T) {
	tests := []struct {
		name string
		// held is the lock that this transaction has already recorded for the block.
		held lockType
		// tableHeld is the count already in the lock table; 0 means no entry.
		tableHeld int
		wantTable int
	}{
		{
			name:      "given a block it holds nothing on, it takes an exclusive lock",
			held:      noLock,
			tableHeld: 0,
			wantTable: -1,
		},
		{
			name:      "given a block it already holds a shared lock on, it upgrades that lock",
			held:      sharedLock,
			tableHeld: 1,
			wantTable: -1,
		},
		{
			name:      "given a block it already holds exclusively, it leaves that lock as it is",
			held:      exclusiveLock,
			tableHeld: -1,
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

			if err := cm.XLock(blk); err != nil {
				t.Fatalf("XLock() error = %v", err)
			}

			if got := cm.locks[*blk]; got != exclusiveLock {
				t.Errorf("locks[%v] = %d, want %d", blk, got, exclusiveLock)
			}
			if got := lt.locks[*blk]; got != tt.wantTable {
				t.Errorf("lockTable.locks[%v] = %d, want %d", blk, got, tt.wantTable)
			}
		})
	}

	// The table's exclusive request treats one shared lock as the caller's own.
	// It can therefore detect another reader only after the caller has taken
	// its own shared lock. Without it, the table would find a count of one,
	// conclude that there is no other reader, and grant the block while another
	// transaction reads it.
	t.Run("given a block another transaction is reading, it waits rather than taking the block away, and keeps the shared lock it took on the way", func(t *testing.T) {
		lt := NewLockTable()
		lt.maxWaitTime = 50 * time.Millisecond
		cm := NewConcurrencyManager(lt)
		blk := filemanager.NewBlockId(testDataFile, 0)
		// Another transaction is reading the block and never releases it.
		lt.locks[*blk] = 1

		err := cm.XLock(blk)

		if !errors.Is(err, ErrLockAbort) {
			t.Fatalf("XLock() error = %v, want %v", err, ErrLockAbort)
		}
		// The shared lock taken during the upgrade is held, so it stays recorded
		// and counted. The caller aborts and releases it with all its other
		// locks.
		if got := cm.locks[*blk]; got != sharedLock {
			t.Errorf("locks[%v] = %d, want %d", blk, got, sharedLock)
		}
		if got := lt.locks[*blk]; got != 2 {
			t.Errorf("lockTable.locks[%v] = %d, want 2", blk, got)
		}
	})

	t.Run("given a block another transaction holds exclusively, the shared lock it needs first is refused and it records nothing", func(t *testing.T) {
		lt := NewLockTable()
		lt.maxWaitTime = 50 * time.Millisecond
		cm := NewConcurrencyManager(lt)
		blk := filemanager.NewBlockId(testDataFile, 0)
		// Another transaction holds the block exclusively and never releases it.
		lt.locks[*blk] = -1

		err := cm.XLock(blk)

		if !errors.Is(err, ErrLockAbort) {
			t.Errorf("XLock() error = %v, want %v", err, ErrLockAbort)
		}
		if _, present := cm.locks[*blk]; present {
			t.Errorf("locks[%v] has an entry, want none", blk)
		}
	})
}

func TestConcurrencyManagerRelease(t *testing.T) {
	// Release releases everything the transaction took. A transaction calls it
	// when it ends. Acquiring an exclusive lock needed two calls on the table,
	// because the shared lock came first, but it needs only one unlock: the
	// exclusive lock replaced the shared count and did not add to it.
	t.Run("given a block read and a block written, it gives up both and leaves no entry behind", func(t *testing.T) {
		lt := NewLockTable()
		cm := NewConcurrencyManager(lt)
		readBlk := filemanager.NewBlockId(testDataFile, 0)
		writtenBlk := filemanager.NewBlockId(testDataFile, 1)

		if err := cm.SLock(readBlk); err != nil {
			t.Fatalf("SLock() error = %v", err)
		}
		if err := cm.XLock(writtenBlk); err != nil {
			t.Fatalf("XLock() error = %v", err)
		}

		cm.Release()

		if got := len(cm.locks); got != 0 {
			t.Errorf("len(locks) = %d, want 0", got)
		}
		if _, present := lt.locks[*readBlk]; present {
			t.Errorf("lockTable.locks[%v] still has an entry, want none", readBlk)
		}
		if _, present := lt.locks[*writtenBlk]; present {
			t.Errorf("lockTable.locks[%v] still has an entry, want none", writtenBlk)
		}
	})

	t.Run("given another transaction reading the same block, its lock is left held", func(t *testing.T) {
		lt := NewLockTable()
		first := NewConcurrencyManager(lt)
		second := NewConcurrencyManager(lt)
		blk := filemanager.NewBlockId(testDataFile, 0)

		if err := first.SLock(blk); err != nil {
			t.Fatalf("SLock() error = %v", err)
		}
		if err := second.SLock(blk); err != nil {
			t.Fatalf("SLock() error = %v", err)
		}

		first.Release()

		if got := lt.locks[*blk]; got != 1 {
			t.Errorf("lockTable.locks[%v] = %d, want 1 (the other transaction still reads it)", blk, got)
		}
		if got := second.locks[*blk]; got != sharedLock {
			t.Errorf("the other manager's locks[%v] = %d, want %d", blk, got, sharedLock)
		}
	})

	t.Run("given a transaction that took no locks at all, it does nothing rather than failing", func(t *testing.T) {
		lt := NewLockTable()
		cm := NewConcurrencyManager(lt)

		cm.Release()

		if got := len(cm.locks); got != 0 {
			t.Errorf("len(locks) = %d, want 0", got)
		}
		if got := len(lt.locks); got != 0 {
			t.Errorf("len(lockTable.locks) = %d, want 0", got)
		}
	})
}
