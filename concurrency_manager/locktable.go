package concurrencymanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// LockTable holds the locks taken on each block, for the whole database rather
// than for one transaction. Transactions coordinate through a single instance,
// so it is the lock table that decides whether a request has to wait.
type LockTable struct {
	// locks counts what is held on a block. A positive value is the number of
	// shared locks; -1 is a single exclusive lock. A block with no entry is
	// unlocked, which is why the count is never zero.
	locks map[filemanager.BlockId]int
}

func NewLockTable() *LockTable {
	return &LockTable{
		locks: make(map[filemanager.BlockId]int),
	}
}
