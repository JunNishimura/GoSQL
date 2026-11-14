package transaction

import (
	"errors"
	"fmt"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
)

// ErrBlockNotPinned is returned for a read or write of a block that the
// transaction has not pinned. The caller is responsible for pinning. Without
// a pin the transaction has no buffer for the block, and using an arbitrary
// buffer from the pool would read or overwrite another block.
var ErrBlockNotPinned = errors.New("block not pinned")

// Transaction is one unit of work against the database. It combines the three
// components that a unit of work needs: a recovery manager that logs its
// changes, a concurrency manager that locks the blocks it uses, and a list of
// the buffers it has pinned.
//
// The buffer pool and the lock table behind these components are shared by
// all transactions, and transactions coordinate through them. This type holds
// only the state that belongs to one transaction.
type Transaction struct {
	fileManager        *filemanager.FileManager
	bufferManager      *buffermanager.BufferManager
	recoveryManager    *recoverymanager.RecoveryManager
	concurrencyManager *concurrencymanager.ConcurrencyManager
	buffers            *buffermanager.BufferList
	// txNum identifies this transaction in the log and on the buffers it
	// modifies. The recovery manager keeps its own copy for the records it
	// writes. This copy is used to mark buffers.
	txNum int
}

// NewTransaction starts transaction txNum and appends its start record to the
// log.
//
// The log manager and the lock table are only needed to create the recovery
// and concurrency managers, so the transaction does not keep them. The caller
// supplies txNum, because the owner of the whole database assigns transaction
// numbers, as it owns the one lock table that all transactions share.
func NewTransaction(
	fm *filemanager.FileManager,
	lm *logmanager.LogManager,
	bm *buffermanager.BufferManager,
	lt *concurrencymanager.LockTable,
	txNum int,
) (*Transaction, error) {
	rm, err := recoverymanager.NewRecoveryManager(lm, bm, txNum)
	if err != nil {
		return nil, fmt.Errorf("start tx %d: %w", txNum, err)
	}

	return &Transaction{
		fileManager:        fm,
		bufferManager:      bm,
		recoveryManager:    rm,
		concurrencyManager: concurrencymanager.NewConcurrencyManager(lt),
		buffers:            buffermanager.NewBufferList(bm),
		txNum:              txNum,
	}, nil
}

// Pin pins blk in a buffer so that the transaction can read and write it. It
// takes a shared lock on the block first. Every Pin must be matched by an
// Unpin, or by the release at the end of the transaction.
//
// A transaction pins a block in order to read it, so the lock is taken here
// and not at each read. The lock is taken before the buffer. A transaction
// that waits for a lock therefore holds no buffer, and contention on one
// block cannot exhaust the pool for other transactions. If a transaction
// waited while it held a buffer, a conflict between two transactions would
// become a buffer shortage for all of them.
//
// The lock outlives the pin. Unpin releases the buffer, but the lock is held
// until the transaction ends. A pin that fails after the lock was taken
// therefore leaves the lock held. A single lock cannot be released on its
// own, and a lock on a block that was never read only reduces concurrency.
//
// The pin goes through the transaction's own buffer list and not directly to
// the pool. The pool records that a buffer is pinned but not by which
// transaction, so only the list makes the release at the end possible.
func (tx *Transaction) Pin(blk *filemanager.BlockId) error {
	if err := tx.concurrencyManager.SLock(blk); err != nil {
		return fmt.Errorf("lock %s to pin it: %w", blk, err)
	}

	return tx.buffers.Pin(blk)
}

// Unpin releases one pin on blk. The transaction can still hold other pins on
// it, so this does not necessarily free the buffer.
func (tx *Transaction) Unpin(blk *filemanager.BlockId) {
	tx.buffers.Unpin(blk)
}

// AvailableBuffers returns the number of unpinned buffers in the pool. The
// pool is shared, so the count reflects the pins of all transactions. Another
// transaction can take some of these buffers before this one does.
func (tx *Transaction) AvailableBuffers() int {
	return tx.bufferManager.Available()
}

// GetInt returns the int at offset in blk.
//
// GetInt takes no lock. The block must be pinned to reach this point, and Pin
// already took the shared lock. Other transactions can read the block at the
// same time, but none can change it until this transaction ends.
func (tx *Transaction) GetInt(blk *filemanager.BlockId, offset int) (int32, error) {
	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return 0, fmt.Errorf("read the int at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetInt(offset), nil
}

// GetString returns the string at offset in blk. Like GetInt, it takes no
// lock.
func (tx *Transaction) GetString(blk *filemanager.BlockId, offset int) (string, error) {
	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return "", fmt.Errorf("read the string at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetString(offset), nil
}

// SetInt writes val at offset in blk. It upgrades the shared lock from Pin to
// an exclusive lock, so that no other transaction can read the block until
// this transaction ends.
//
// The old value is logged before the new value is written. If the write came
// first, the log record would hold the new value. An undo of that record
// would then write the new value again and would not reverse the change.
//
// The buffer is marked with this transaction's number and the LSN of the log
// record. The LSN makes the pool flush the log before it writes the block.
func (tx *Transaction) SetInt(blk *filemanager.BlockId, offset int, val int32) error {
	if err := tx.concurrencyManager.XLock(blk); err != nil {
		return fmt.Errorf("lock %s to write an int: %w", blk, err)
	}

	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return fmt.Errorf("write the int at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	lsn, err := tx.recoveryManager.LogSetInt(buf, offset)
	if err != nil {
		return err
	}

	if err := buf.Contents().SetInt(offset, val); err != nil {
		return fmt.Errorf("write the int at offset %d of %s: %w", offset, blk, err)
	}
	buf.SetModified(tx.txNum, lsn)

	return nil
}

// SetString writes val at offset in blk. Its locking and its write order
// are the same as in SetInt.
func (tx *Transaction) SetString(blk *filemanager.BlockId, offset int, val string) error {
	if err := tx.concurrencyManager.XLock(blk); err != nil {
		return fmt.Errorf("lock %s to write a string: %w", blk, err)
	}

	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return fmt.Errorf("write the string at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	lsn, err := tx.recoveryManager.LogSetString(buf, offset)
	if err != nil {
		return err
	}

	if err := buf.Contents().SetString(offset, val); err != nil {
		return fmt.Errorf("write the string at offset %d of %s: %w", offset, blk, err)
	}
	buf.SetModified(tx.txNum, lsn)

	return nil
}

// BlockSize returns the number of bytes in a block, which is fixed for the
// whole database. Callers that lay out records in a block need it to compute
// how many fit. It takes no lock, because the block size is set when the
// database is created and no transaction can change it.
func (tx *Transaction) BlockSize() int {
	return tx.fileManager.BlockSize()
}

// endOfFile is the block number of the dummy block that represents the
// length of a file. No real block has this number. A lock on it therefore
// conflicts only with other transactions that use the same file length, and
// never with the blocks of the file. A transaction that extends a file does
// not block reads of the existing blocks.
const endOfFile = -1

// Size returns the number of blocks in fileName. It takes a shared lock on
// the file length, so that no other transaction can extend the file until
// this transaction ends.
func (tx *Transaction) Size(fileName string) (int, error) {
	if err := tx.concurrencyManager.SLock(filemanager.NewBlockId(fileName, endOfFile)); err != nil {
		return 0, fmt.Errorf("lock the length of %s to read it: %w", fileName, err)
	}

	return tx.fileManager.Length(fileName)
}

// Append adds a block to the end of fileName and returns it. This changes the
// file length, so Append takes an exclusive lock on the length. No other
// transaction can read the length until this transaction ends.
func (tx *Transaction) Append(fileName string) (*filemanager.BlockId, error) {
	if err := tx.concurrencyManager.XLock(filemanager.NewBlockId(fileName, endOfFile)); err != nil {
		return nil, fmt.Errorf("lock the length of %s to extend it: %w", fileName, err)
	}

	return tx.fileManager.Append(fileName)
}

// Commit makes the changes of the transaction permanent and ends it.
//
// The locks and the pins are released only after the commit record is on the
// log. This release lets other transactions proceed. A failed commit keeps
// the locks and the pins. Some buffers may already have been written, so
// another transaction could otherwise read uncommitted changes. The held
// locks make other transactions wait until they time out, which is less
// harmful.
func (tx *Transaction) Commit() error {
	if err := tx.recoveryManager.Commit(); err != nil {
		return err
	}

	tx.concurrencyManager.Release()
	tx.buffers.UnpinAll()

	return nil
}

// Rollback undoes every change of the transaction and ends it. As in Commit,
// the locks and the pins are released only after the rollback has succeeded.
func (tx *Transaction) Rollback() error {
	if err := tx.recoveryManager.Rollback(); err != nil {
		return err
	}

	tx.concurrencyManager.Release()
	tx.buffers.UnpinAll()

	return nil
}

// Recover undoes every transaction that the log shows as unfinished. The
// database calls it at startup after a crash. It must run before any other
// transaction, because it does not lock the blocks it restores.
//
// Recover has nothing to release at the end. It takes no locks, and it unpins
// each buffer as soon as the block is restored.
func (tx *Transaction) Recover() error {
	return tx.recoveryManager.Recover()
}
