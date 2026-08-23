package concurrencymanager

import (
	"errors"
	"fmt"
	"sync"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const defaultMaxWaitTime = 10 * time.Second

// ErrLockAbort is returned when a lock does not become available within
// maxWaitTime. Waiting that long means the transactions involved are most
// likely deadlocked, so the caller is expected to roll back and retry.
var ErrLockAbort = errors.New("lock not available")

// LockTable holds the locks taken on each block, for the whole database rather
// than for one transaction. Transactions coordinate through a single instance,
// so it is the lock table that decides whether a request has to wait.
type LockTable struct {
	// locks counts what is held on a block. A positive value is the number of
	// shared locks; -1 is a single exclusive lock. A block with no entry is
	// unlocked, which is why the count is never zero.
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

// SLock takes a shared lock on blk, waiting while another transaction holds it
// exclusively. Any number of transactions may hold a shared lock at once, so it
// only has to wait out an exclusive one.
func (lt *LockTable) SLock(blk *filemanager.BlockId) error {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	// sync.Cond cannot wait with a deadline, so a timer wakes the waiters up
	// once maxWaitTime has passed.
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

// hasXLock reports whether blk is held exclusively. The caller must hold mu.
func (lt *LockTable) hasXLock(blk *filemanager.BlockId) bool {
	return lt.locks[*blk] < 0
}
