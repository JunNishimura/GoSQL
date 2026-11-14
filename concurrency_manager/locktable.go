package concurrencymanager

import (
	"errors"
	"fmt"
	"sync"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const defaultMaxWaitTime = 10 * time.Second

// exclusive is the count stored for a block that one transaction holds
// exclusively. Every other transaction must wait until it is released.
const exclusive = -1

// ErrLockAbort is returned when a lock does not become available within
// maxWaitTime. A wait that long most likely means a deadlock, so the caller
// is expected to roll back and retry.
var ErrLockAbort = errors.New("lock not available")

// LockTable holds the locks on each block for the whole database, not for one
// transaction. All transactions coordinate through a single instance, so the
// lock table decides whether a request must wait.
type LockTable struct {
	// locks holds the lock count of each block. A positive value is the number
	// of shared locks, and -1 is one exclusive lock. A block with no entry is
	// unlocked, so the count is never zero.
	locks       map[filemanager.BlockId]int
	maxWaitTime time.Duration
	mu          sync.Mutex
	cond        *sync.Cond
}

func NewLockTable() *LockTable {
	lt := &LockTable{
		locks:       make(map[filemanager.BlockId]int),
		maxWaitTime: defaultMaxWaitTime,
	}
	lt.cond = sync.NewCond(&lt.mu)

	return lt
}

// SLock takes a shared lock on blk. It waits while another transaction holds
// blk exclusively. Any number of transactions can hold a shared lock at the
// same time, so only an exclusive lock makes it wait.
func (lt *LockTable) SLock(blk *filemanager.BlockId) error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	// sync.Cond has no wait with a deadline, so a timer wakes the waiters
	// after maxWaitTime.
	timedOut := false
	timer := time.AfterFunc(lt.maxWaitTime, func() {
		lt.mu.Lock()
		timedOut = true
		lt.mu.Unlock()
		lt.cond.Broadcast()
	})
	defer timer.Stop()

	for lt.hasXLock(blk) {
		if timedOut {
			return fmt.Errorf("take a shared lock on %s: %w", blk, ErrLockAbort)
		}
		lt.cond.Wait()
	}

	lt.locks[*blk]++

	return nil
}

// XLock takes an exclusive lock on blk. It waits while any other transaction
// holds a shared lock on blk.
//
// The caller must already hold a shared lock on blk. The concurrency manager
// always calls XLock this way. This precondition makes the wait condition
// correct: one shared lock is the caller's own, so only a count above one
// means that another transaction is reading the block. The precondition also
// excludes an exclusive lock held by another transaction, because the
// caller's shared lock request would have waited for its release.
//
// XLock cannot check the precondition. The table counts locks without
// recording their owners, so it cannot tell whether a count of one is the
// caller's own lock or another transaction's.
func (lt *LockTable) XLock(blk *filemanager.BlockId) error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	timedOut := false
	timer := time.AfterFunc(lt.maxWaitTime, func() {
		lt.mu.Lock()
		timedOut = true
		lt.mu.Unlock()
		lt.cond.Broadcast()
	})
	defer timer.Stop()

	for lt.hasOtherSLocks(blk) {
		if timedOut {
			return fmt.Errorf("take an exclusive lock on %s: %w", blk, ErrLockAbort)
		}
		lt.cond.Wait()
	}

	lt.locks[*blk] = exclusive

	return nil
}

// Unlock releases one lock on blk. Releasing the last shared lock, or the
// exclusive lock, removes the entry and does not leave a count of zero. A
// missing entry is then the only state that means the block is unlocked.
//
// Every unlock wakes the waiters, not only an unlock that removes the entry.
// An exclusive request waits for the shared count to drop from two to one.
// To wake waiters selectively, Unlock would have to know the wait condition
// of every other method.
//
// Unlocking a block that is not locked does nothing. The concurrency manager
// only unlocks what it locked, so it never causes this case.
func (lt *LockTable) Unlock(blk *filemanager.BlockId) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if held := lt.locks[*blk]; held > 1 {
		lt.locks[*blk] = held - 1
	} else {
		delete(lt.locks, *blk)
	}

	lt.cond.Broadcast()
}

// hasXLock reports whether blk is held exclusively. The caller must hold mu.
func (lt *LockTable) hasXLock(blk *filemanager.BlockId) bool {
	return lt.locks[*blk] < 0
}

// hasOtherSLocks reports whether a transaction other than the caller holds a
// shared lock on blk. The caller must hold mu. It must also hold a shared
// lock on blk, which accounts for one of the counted locks.
func (lt *LockTable) hasOtherSLocks(blk *filemanager.BlockId) bool {
	return lt.locks[*blk] > 1
}
