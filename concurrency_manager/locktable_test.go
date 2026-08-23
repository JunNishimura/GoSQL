package concurrencymanager

import (
	"errors"
	"testing"
	"time"

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
	// Waiting on a nil condition variable panics, so the constructor has to
	// build it rather than leave it to the first caller that has to wait.
	if lt.cond == nil {
		t.Error("cond is nil, want non-nil")
	}
	if lt.maxWaitTime != defaultMaxWaitTime {
		t.Errorf("maxWaitTime = %v, want %v", lt.maxWaitTime, defaultMaxWaitTime)
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

func TestSLock(t *testing.T) {
	tests := []struct {
		name string
		// held is the count already in the table; 0 means the block has no entry.
		held int
		want int
	}{
		{
			name: "takes a shared lock on a block nobody holds",
			held: 0,
			want: 1,
		},
		{
			name: "joins a shared lock another transaction already holds",
			held: 1,
			want: 2,
		},
		{
			name: "joins several shared locks already held",
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
}

// Locks are per block, so one block being locked says nothing about another.
func TestSLockIsPerBlock(t *testing.T) {
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
}

// releaseExclusiveLock stands in for Unlock, which does not exist yet. It drops
// the entry and wakes the waiters, which is what a real release has to do.
func releaseExclusiveLock(lt *LockTable, blk *filemanager.BlockId) {
	lt.mu.Lock()
	delete(lt.locks, *blk)
	lt.mu.Unlock()
	lt.cond.Broadcast()
}

func TestSLockWaitsForAnExclusiveLockToBeReleased(t *testing.T) {
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

	releaseExclusiveLock(lt, blk)

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
}

func TestSLockTimesOutWhileAnExclusiveLockIsHeld(t *testing.T) {
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
}
