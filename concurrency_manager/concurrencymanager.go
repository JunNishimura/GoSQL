package concurrencymanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// lockType is the kind of lock that a transaction holds on a block. noLock is
// the zero value, so a map lookup for a block that the transaction never
// locked returns "not held" without a separate presence check. For the same
// reason noLock is never stored.
type lockType int

const (
	noLock lockType = iota
	sharedLock
	exclusiveLock
)

// ConcurrencyManager serves one transaction. It records the locks that the
// transaction has taken. A second request for the same block then does not
// go to the lock table again, and all locks can be released at the end.
//
// The lock table is shared by all transactions and decides which requests
// wait. The records here are private to one transaction. If they were shared,
// a transaction could release a lock that another one holds.
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

// SLock ensures that the transaction may read blk. It takes a shared lock on
// blk if the transaction holds no lock on it yet. Either kind of lock already
// allows reading, so a block that is already locked needs nothing.
//
// A second request to the table for the same block would count one
// transaction as two readers. An exclusive request would then wait for a lock
// that nobody holds.
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

// XLock ensures that the transaction may write to blk. It upgrades to an
// exclusive lock unless the transaction already holds one.
//
// The shared lock is taken first because the table's exclusive request
// assumes it. The table treats one shared lock as the caller's own, so it can
// detect another reader only after this transaction has taken its own shared
// lock. Without it, the table would find a count of one, conclude that there
// is no other reader, and grant the block while another transaction reads it.
//
// A failed upgrade keeps the shared lock recorded, because the transaction
// does hold it. The caller aborts and releases it with all its other locks.
func (cm *ConcurrencyManager) XLock(blk *filemanager.BlockId) error {
	if cm.locks[*blk] == exclusiveLock {
		return nil
	}

	if err := cm.SLock(blk); err != nil {
		return err
	}

	if err := cm.lockTable.XLock(blk); err != nil {
		return err
	}
	cm.locks[*blk] = exclusiveLock

	return nil
}

// Release releases every lock that the transaction took. A transaction calls
// it when it ends. It holds its locks until then, so that nothing it read can
// change before it finishes.
//
// One unlock per block is enough, even for a block held exclusively. Taking
// that lock needed two calls on the table, but the exclusive lock replaced
// the shared count and did not add to it.
func (cm *ConcurrencyManager) Release() {
	for blk := range cm.locks {
		cm.lockTable.Unlock(&blk)
	}

	clear(cm.locks)
}
