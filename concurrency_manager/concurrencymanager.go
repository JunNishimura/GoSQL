package concurrencymanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// lockType is what a transaction holds on a block. noLock is the zero value so
// that looking up a block the transaction never locked reads as "not held"
// without a separate presence check, which also means it is never stored.
type lockType int

const (
	noLock lockType = iota
	sharedLock
	exclusiveLock
)

// ConcurrencyManager serves one transaction. It remembers the locks that
// transaction took, so that asking for the same block twice does not go to the
// lock table again, and so that they can all be released at the end.
//
// The lock table is shared by every transaction and is what actually decides
// who waits; the records here are private to this transaction. Sharing them
// would let a transaction release a lock another one holds.
type ConcurrencyManager struct {
	lockTable *LockTable
	locks     map[filemanager.BlockId]lockType
}

func NewConcurrencyManager(lt *LockTable) *ConcurrencyManager {
	return &ConcurrencyManager{
		lockTable: lt,
		locks:     make(map[filemanager.BlockId]lockType),
	}
}

// SLock makes sure the transaction may read blk, taking a shared lock on it if
// it holds none yet. Holding either kind of lock is already enough, so a block
// this transaction has locked before needs nothing.
//
// Asking the table twice for the same block would count one transaction as two
// readers, and an exclusive request would then wait for a lock that nobody
// holds.
func (cm *ConcurrencyManager) SLock(blk *filemanager.BlockId) error {
	if cm.locks[*blk] != noLock {
		return nil
	}

	if err := cm.lockTable.SLock(blk); err != nil {
		return err
	}
	cm.locks[*blk] = sharedLock

	return nil
}
