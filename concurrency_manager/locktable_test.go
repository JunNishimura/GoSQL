package concurrencymanager

import (
	"errors"
	"testing"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const testDataFile = "test.tbl"

func TestNewLockTable(t *testing.T) {
	t.Run("it starts with no locks, a condition variable to wait on, and the default wait time", func(t *testing.T) {
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
		// Waiting on a nil condition variable panics, so the constructor has to
		// build it rather than leave it to the first caller that has to wait.
		if lt.cond == nil {
			t.Error("cond is nil, want non-nil")
		}
		if lt.maxWaitTime != defaultMaxWaitTime {
			t.Errorf("maxWaitTime = %v, want %v", lt.maxWaitTime, defaultMaxWaitTime)
		}
	})

	// Each lock table owns its own map. A table shared between callers would let
	// one of them see, and release, locks it never took.
	t.Run("given two lock tables, when one records a lock, then the other does not see it", func(t *testing.T) {
		first := NewLockTable()
		second := NewLockTable()

		first.locks[*filemanager.NewBlockId(testDataFile, 0)] = 1

		if got := len(second.locks); got != 0 {
			t.Errorf("len(locks) of the second table = %d, want 0", got)
		}
	})
}

func TestSLock(t *testing.T) {
	tests := []struct {
		name string
		// held is the count already in the table; 0 means the block has no entry.
		held int
		want int
	}{
		{
			name: "given a block nobody holds, it becomes the one reader",
			held: 0,
			want: 1,
		},
		{
			name: "given a block one transaction is reading, it joins as a second reader",
			held: 1,
			want: 2,
		},
		{
			name: "given a block several transactions are reading, it joins them",
			held: 3,
			want: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt := NewLockTable()
			blk := filemanager.NewBlockId(testDataFile, 0)
			if tt.held != 0 {
				lt.locks[*blk] = tt.held
			}

			if err := lt.SLock(blk); err != nil {
				t.Fatalf("SLock() error = %v", err)
			}

			if got := lt.locks[*blk]; got != tt.want {
				t.Errorf("locks[%v] = %d, want %d", blk, got, tt.want)
			}
		})
	}

	// Locks are per block, so one block being locked says nothing about another.
	t.Run("when two blocks are locked, then each is counted on its own", func(t *testing.T) {
		lt := NewLockTable()
		first := filemanager.NewBlockId(testDataFile, 0)
		second := filemanager.NewBlockId(testDataFile, 1)

		if err := lt.SLock(first); err != nil {
			t.Fatalf("SLock() error = %v", err)
		}
		if err := lt.SLock(second); err != nil {
			t.Fatalf("SLock() error = %v", err)
		}

		if got := lt.locks[*first]; got != 1 {
			t.Errorf("locks[%v] = %d, want 1", first, got)
		}
		if got := lt.locks[*second]; got != 1 {
			t.Errorf("locks[%v] = %d, want 1", second, got)
		}
	})

	t.Run("given a block held exclusively, it waits, and takes the lock once that one is released", func(t *testing.T) {
		lt := NewLockTable()
		blk := filemanager.NewBlockId(testDataFile, 0)
		lt.locks[*blk] = -1

		done := make(chan error, 1)
		go func() {
			done <- lt.SLock(blk)
		}()

		select {
		case err := <-done:
			t.Fatalf("SLock() returned %v while an exclusive lock was held, want it to keep waiting", err)
		case <-time.After(50 * time.Millisecond):
			// Still waiting, which is the expected behaviour.
		}

		lt.Unlock(blk)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("SLock() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("SLock() did not return after the exclusive lock was released")
		}

		lt.mu.Lock()
		defer lt.mu.Unlock()
		if got := lt.locks[*blk]; got != 1 {
			t.Errorf("locks[%v] = %d, want 1", blk, got)
		}
	})

	t.Run("given an exclusive lock that is never released, it waits out the limit, reports ErrLockAbort and leaves the table as it found it", func(t *testing.T) {
		const maxWaitTime = 50 * time.Millisecond

		lt := NewLockTable()
		lt.maxWaitTime = maxWaitTime
		blk := filemanager.NewBlockId(testDataFile, 0)
		lt.locks[*blk] = -1

		start := time.Now()
		err := lt.SLock(blk)
		elapsed := time.Since(start)

		if !errors.Is(err, ErrLockAbort) {
			t.Errorf("SLock() error = %v, want %v", err, ErrLockAbort)
		}
		if elapsed < maxWaitTime {
			t.Errorf("SLock() gave up after %v, want it to wait at least %v", elapsed, maxWaitTime)
		}
		// A request that gave up must leave the table as it found it.
		if got := lt.locks[*blk]; got != -1 {
			t.Errorf("locks[%v] = %d, want -1", blk, got)
		}
	})
}

func TestXLock(t *testing.T) {
	// XLock is only ever called by a transaction that already holds a shared
	// lock on the block, so a count of one is the caller's own lock and nothing
	// to wait for.
	t.Run("given a block only the caller is reading, it takes the block exclusively", func(t *testing.T) {
		lt := NewLockTable()
		blk := filemanager.NewBlockId(testDataFile, 0)
		lt.locks[*blk] = 1

		if err := lt.XLock(blk); err != nil {
			t.Fatalf("XLock() error = %v", err)
		}

		if got := lt.locks[*blk]; got != -1 {
			t.Errorf("locks[%v] = %d, want -1", blk, got)
		}
	})

	t.Run("when one block is taken exclusively, then the locks on another block are left alone", func(t *testing.T) {
		lt := NewLockTable()
		locked := filemanager.NewBlockId(testDataFile, 0)
		other := filemanager.NewBlockId(testDataFile, 1)
		lt.locks[*locked] = 1
		lt.locks[*other] = 3

		if err := lt.XLock(locked); err != nil {
			t.Fatalf("XLock() error = %v", err)
		}

		if got := lt.locks[*locked]; got != -1 {
			t.Errorf("locks[%v] = %d, want -1", locked, got)
		}
		if got := lt.locks[*other]; got != 3 {
			t.Errorf("locks[%v] = %d, want 3 (another block must be untouched)", other, got)
		}
	})

	t.Run("given another transaction reading the block, it waits, and takes the lock once that reader lets go", func(t *testing.T) {
		lt := NewLockTable()
		blk := filemanager.NewBlockId(testDataFile, 0)
		// Two shared locks: the caller's own, and one held by another transaction.
		lt.locks[*blk] = 2

		done := make(chan error, 1)
		go func() {
			done <- lt.XLock(blk)
		}()

		select {
		case err := <-done:
			t.Fatalf("XLock() returned %v while another shared lock was held, want it to keep waiting", err)
		case <-time.After(50 * time.Millisecond):
			// Still waiting, which is the expected behaviour.
		}

		lt.Unlock(blk)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("XLock() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("XLock() did not return after the other shared lock was released")
		}

		lt.mu.Lock()
		defer lt.mu.Unlock()
		if got := lt.locks[*blk]; got != -1 {
			t.Errorf("locks[%v] = %d, want -1", blk, got)
		}
	})

	t.Run("given another reader that never lets go, it waits out the limit, reports ErrLockAbort and leaves the caller's own shared lock counted", func(t *testing.T) {
		const maxWaitTime = 50 * time.Millisecond

		lt := NewLockTable()
		lt.maxWaitTime = maxWaitTime
		blk := filemanager.NewBlockId(testDataFile, 0)
		lt.locks[*blk] = 2

		start := time.Now()
		err := lt.XLock(blk)
		elapsed := time.Since(start)

		if !errors.Is(err, ErrLockAbort) {
			t.Errorf("XLock() error = %v, want %v", err, ErrLockAbort)
		}
		if elapsed < maxWaitTime {
			t.Errorf("XLock() gave up after %v, want it to wait at least %v", elapsed, maxWaitTime)
		}
		// A request that gave up must leave the table as it found it, so that
		// the shared lock the caller still holds is not lost.
		if got := lt.locks[*blk]; got != 2 {
			t.Errorf("locks[%v] = %d, want 2", blk, got)
		}
	})
}

func TestUnlock(t *testing.T) {
	tests := []struct {
		name string
		// held is the count already in the table; 0 means the block has no entry.
		held int
		// want is the count that must remain; 0 means the entry must be gone.
		want int
	}{
		{
			name: "given a block held exclusively, the entry is removed",
			held: -1,
			want: 0,
		},
		{
			name: "given a block with one reader, the entry is removed rather than left at zero",
			held: 1,
			want: 0,
		},
		{
			name: "given a block with two readers, the other one is left counted",
			held: 2,
			want: 1,
		},
		{
			name: "given a block with several readers, the others are left counted",
			held: 3,
			want: 2,
		},
		{
			name: "given a block nobody holds, nothing happens rather than a count going negative",
			held: 0,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt := NewLockTable()
			blk := filemanager.NewBlockId(testDataFile, 0)
			if tt.held != 0 {
				lt.locks[*blk] = tt.held
			}

			lt.Unlock(blk)

			// A released block must have no entry at all. A count of zero left
			// behind would read as a lock that nobody holds.
			got, present := lt.locks[*blk]
			if tt.want == 0 {
				if present {
					t.Errorf("locks[%v] = %d, want no entry", blk, got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("locks[%v] = %d, want %d", blk, got, tt.want)
			}
		})
	}

	t.Run("when one block is released, then the locks on another block are left alone", func(t *testing.T) {
		lt := NewLockTable()
		released := filemanager.NewBlockId(testDataFile, 0)
		other := filemanager.NewBlockId(testDataFile, 1)
		lt.locks[*released] = 1
		lt.locks[*other] = 3

		lt.Unlock(released)

		if _, present := lt.locks[*released]; present {
			t.Errorf("locks[%v] still has an entry, want none", released)
		}
		if got := lt.locks[*other]; got != 3 {
			t.Errorf("locks[%v] = %d, want 3 (another block must be untouched)", other, got)
		}
	})
}
