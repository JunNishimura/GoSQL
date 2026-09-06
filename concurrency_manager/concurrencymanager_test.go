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

	// Every transaction gets its own concurrency manager, and they coordinate
	// through one shared lock table. Each has to remember only the locks it
	// took: a shared record would let a transaction release a lock another one
	// holds.
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
	// The zero value of lockType has to mean "not held", so that a lookup for a
	// block this transaction never locked reads correctly without a presence
	// check.
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
		// held is what this transaction has already recorded for the block.
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
			// Going to the table again would count the same transaction twice,
			// which would make an exclusive request wait for a lock nobody holds.
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

	// A lock that could not be taken must not be recorded, or the transaction
	// would later release a lock it never held and believe it may read the
	// block.
	t.Run("given a block another transaction holds exclusively, it reports ErrLockAbort and records nothing", func(t *testing.T) {
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
	})
}

func TestConcurrencyManagerXLock(t *testing.T) {
	tests := []struct {
		name string
		// held is what this transaction has already recorded for the block.
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

	// The table's exclusive request treats one shared hold as the caller's own,
	// so it can only tell another reader apart once the caller has taken its own
	// shared lock. Going straight to the table would find a count of one, read
	// it as nobody else, and take the block away from the transaction reading
	// it.
	t.Run("given a block another transaction is reading, it waits rather than taking the block away, and keeps the shared lock it took on the way", func(t *testing.T) {
		lt := NewLockTable()
		lt.maxWaitTime = 50 * time.Millisecond
		cm := NewConcurrencyManager(lt)
		blk := filemanager.NewBlockId(testDataFile, 0)
		// Another transaction is reading the block and never lets go.
		lt.locks[*blk] = 1

		err := cm.XLock(blk)

		if !errors.Is(err, ErrLockAbort) {
			t.Fatalf("XLock() error = %v, want %v", err, ErrLockAbort)
		}
		// The shared lock taken on the way is really held, so it stays recorded
		// and counted. The caller aborts and releases it along with everything
		// else.
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
		// Another transaction holds the block exclusively and never lets go.
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
	// Release drops everything the transaction took, which is what ends it. A
	// block held exclusively took two calls on the table to acquire, since the
	// shared lock came first, but one release: taking the exclusive lock
	// replaced the count rather than adding to it.
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
