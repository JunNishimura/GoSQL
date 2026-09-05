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

// ErrBlockNotPinned reports a read or write of a block the transaction has not
// pinned. Pinning is the caller's job, and without a pin there is no buffer to
// work through: reaching for whatever the pool happens to hold would read, or
// overwrite, some other block.
var ErrBlockNotPinned = errors.New("block not pinned")

// Transaction is one unit of work against the database. It ties together the
// three things a unit of work needs: a recovery manager to log what it changed,
// a concurrency manager to keep other transactions out of what it is using, and
// a list of the buffers it has pinned.
//
// The buffer pool and the lock table behind those are shared by every
// transaction, which is how they coordinate. What this type holds is what
// belongs to this transaction alone.
type Transaction struct {
	fileManager        *filemanager.FileManager
	bufferManager      *buffermanager.BufferManager
	recoveryManager    *recoverymanager.RecoveryManager
	concurrencyManager *concurrencymanager.ConcurrencyManager
	buffers            *buffermanager.BufferList
	// txNum identifies this transaction in the log and on the buffers it
	// modifies. The recovery manager keeps its own copy for the records it
	// writes; this one is for stamping buffers.
	txNum int
}

// NewTransaction starts transaction txNum, which puts a start record on the log.
//
// The log manager and the lock table are only needed to build the recovery and
// concurrency managers, so they are not kept. txNum comes from the caller
// because handing out numbers belongs to whatever owns the database as a whole,
// alongside the one lock table every transaction shares.
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

// Pin keeps blk in a buffer so that the transaction can read and write it,
// taking a shared lock on the block first. Every pin has to be matched by an
// Unpin, or by the release that ends the transaction.
//
// A transaction pins a block because it means to look at it, so the lock is
// taken here rather than at each read. Taking it before the buffer is what
// matters: a transaction waiting for the lock holds nothing, so contention over
// one block cannot empty the pool for everybody else. Waiting with a buffer in
// hand would turn a dispute between two transactions into a shortage for all of
// them.
//
// The lock outlives the pin. Unpin gives the buffer back, but the lock is held
// until the transaction ends, so a pin that failed after taking the lock leaves
// it held. There is no way to give up one lock on its own, and holding a lock
// on a block that was never read costs only concurrency.
//
// The pin goes through the transaction's own list rather than straight to the
// pool, which is what makes the release at the end possible: the pool records
// that a buffer is in use, but not by whom.
func (tx *Transaction) Pin(blk *filemanager.BlockId) error {
	if err := tx.concurrencyManager.SLock(blk); err != nil {
		return fmt.Errorf("lock %s to pin it: %w", blk, err)
	}

	return tx.buffers.Pin(blk)
}

// Unpin gives up one pin on blk. The transaction may still hold others, so this
// does not necessarily free the buffer.
func (tx *Transaction) Unpin(blk *filemanager.BlockId) {
	tx.buffers.Unpin(blk)
}

// GetInt returns the int at offset in blk.
//
// No lock is taken here: Pin already took the shared one, and reaching this
// point means the block is pinned. Other transactions may read the block at the
// same time, but none may change it until this transaction ends.
func (tx *Transaction) GetInt(blk *filemanager.BlockId, offset int) (int32, error) {
	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return 0, fmt.Errorf("read the int at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetInt(offset), nil
}

// GetString returns the string at offset in blk. See GetInt for why it takes no
// lock of its own.
func (tx *Transaction) GetString(blk *filemanager.BlockId, offset int) (string, error) {
	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return "", fmt.Errorf("read the string at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetString(offset), nil
}

// SetInt writes val at offset in blk, upgrading the shared lock Pin took to an
// exclusive one so that nobody may even look at the block until this
// transaction ends.
//
// The old value is logged before the new one is written. Writing first would
// leave the log holding the new value, so undoing the record would put back what
// the transaction wrote and lose the change it was meant to reverse.
//
// The buffer is stamped with this transaction's number and the LSN of that
// record, which is what makes the pool write the log out before the block.
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

// SetString writes val at offset in blk. See SetInt for the locking and the
// order the log and the block are written in.
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
// whole database. Callers that lay records out in a block need it to tell how
// many fit, and asking for it takes no lock: it is settled when the database is
// created and no transaction can change it.
func (tx *Transaction) BlockSize() int {
	return tx.fileManager.BlockSize()
}

// endOfFile is the block number of the dummy block that stands for a file's
// length. No block ever has this number, so locking it contends only with other
// transactions asking about the same length, and never with the blocks in the
// file: extending a file leaves whatever is already in it free to read.
const endOfFile = -1

// Size returns the number of blocks in fileName, taking a shared lock on its
// length so that nobody may extend the file until this transaction ends.
func (tx *Transaction) Size(fileName string) (int, error) {
	if err := tx.concurrencyManager.SLock(filemanager.NewBlockId(fileName, endOfFile)); err != nil {
		return 0, fmt.Errorf("lock the length of %s to read it: %w", fileName, err)
	}

	return tx.fileManager.Length(fileName)
}

// Append adds a block to the end of fileName and returns it. Extending the file
// changes its length, so this takes an exclusive lock on the length and nobody
// may read it until this transaction ends.
func (tx *Transaction) Append(fileName string) (*filemanager.BlockId, error) {
	if err := tx.concurrencyManager.XLock(filemanager.NewBlockId(fileName, endOfFile)); err != nil {
		return nil, fmt.Errorf("lock the length of %s to extend it: %w", fileName, err)
	}

	return tx.fileManager.Append(fileName)
}

// Commit makes the transaction's changes permanent and ends it.
//
// Releasing the locks and the pins is what lets the next transaction in, and it
// happens only once the commit is on the log. A failed commit keeps them: some
// buffers may already have been written out, so letting another transaction in
// would show it changes that are not committed. Holding the locks stalls other
// transactions until they give up, which is the lesser harm.
func (tx *Transaction) Commit() error {
	if err := tx.recoveryManager.Commit(); err != nil {
		return err
	}

	tx.concurrencyManager.Release()
	tx.buffers.UnpinAll()

	return nil
}

// Rollback takes back everything the transaction did and ends it. See Commit
// for why the locks and the pins are released only once that has succeeded.
func (tx *Transaction) Rollback() error {
	if err := tx.recoveryManager.Rollback(); err != nil {
		return err
	}

	tx.concurrencyManager.Release()
	tx.buffers.UnpinAll()

	return nil
}

// Recover undoes every transaction the log shows as unfinished, which is what
// the database does on start up after a crash. It has to run before any other
// transaction, since it does not lock what it repairs.
//
// There is nothing to release afterwards: recovery takes no locks, and the
// buffers it pins to restore a block are unpinned as it goes.
func (tx *Transaction) Recover() error {
	return tx.recoveryManager.Recover()
}
