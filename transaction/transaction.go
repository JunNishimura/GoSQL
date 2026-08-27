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

// Pin keeps blk in a buffer so that the transaction can read and write it.
// Every pin has to be matched by an Unpin, or by the release that ends the
// transaction.
//
// The pin goes through the transaction's own list rather than straight to the
// pool, which is what makes that release possible: the pool records that a
// buffer is in use, but not by whom.
func (tx *Transaction) Pin(blk *filemanager.BlockId) error {
	return tx.buffers.Pin(blk)
}

// Unpin gives up one pin on blk. The transaction may still hold others, so this
// does not necessarily free the buffer.
func (tx *Transaction) Unpin(blk *filemanager.BlockId) {
	tx.buffers.Unpin(blk)
}

// GetInt returns the int at offset in blk, taking a shared lock on the block so
// that nobody may change it until this transaction ends. Other transactions may
// read it at the same time.
func (tx *Transaction) GetInt(blk *filemanager.BlockId, offset int) (int32, error) {
	if err := tx.concurrencyManager.SLock(blk); err != nil {
		return 0, fmt.Errorf("lock %s to read an int: %w", blk, err)
	}

	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return 0, fmt.Errorf("read the int at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetInt(offset), nil
}

// GetString returns the string at offset in blk. See GetInt for how the block
// is locked.
func (tx *Transaction) GetString(blk *filemanager.BlockId, offset int) (string, error) {
	if err := tx.concurrencyManager.SLock(blk); err != nil {
		return "", fmt.Errorf("lock %s to read a string: %w", blk, err)
	}

	buf := tx.buffers.Buffer(blk)
	if buf == nil {
		return "", fmt.Errorf("read the string at offset %d of %s: %w", offset, blk, ErrBlockNotPinned)
	}

	return buf.Contents().GetString(offset), nil
}

// SetInt writes val at offset in blk, taking an exclusive lock so that nobody
// may read or write the block until this transaction ends.
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
