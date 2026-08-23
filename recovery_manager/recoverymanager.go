package recoverymanager

import (
	"fmt"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// RecoveryManager writes the log records of a single transaction and undoes
// them when that transaction has to be rolled back. Each transaction owns one
// instance, which is why txNum is fixed at construction.
type RecoveryManager struct {
	logManager    *logmanager.LogManager
	bufferManager *buffermanager.BufferManager
	txNum         int
}

// NewRecoveryManager creates the recovery manager for transaction txNum and
// appends its start record, so the transaction is on the log from the moment it
// begins.
func NewRecoveryManager(lm *logmanager.LogManager, bm *buffermanager.BufferManager, txNum int) (*RecoveryManager, error) {
	if _, err := WriteStartRecordToLog(lm, txNum); err != nil {
		return nil, fmt.Errorf("write start record for tx %d: %w", txNum, err)
	}

	return &RecoveryManager{
		logManager:    lm,
		bufferManager: bm,
		txNum:         txNum,
	}, nil
}

// Commit makes the transaction's changes permanent.
//
// The steps are ordered so that the commit record is never on disk before the
// data it commits: the modified buffers are written out first, and only then is
// the commit record appended and flushed. A crash between the two leaves the
// transaction looking unfinished, which recovery can undo, whereas the reverse
// order would leave a committed transaction whose changes were lost.
func (rm *RecoveryManager) Commit() error {
	if err := rm.bufferManager.FlushAll(rm.txNum); err != nil {
		return fmt.Errorf("flush buffers of tx %d: %w", rm.txNum, err)
	}

	lsn, err := WriteCommitRecordToLog(rm.logManager, rm.txNum)
	if err != nil {
		return fmt.Errorf("write commit record for tx %d: %w", rm.txNum, err)
	}

	if err := rm.logManager.Flush(lsn); err != nil {
		return fmt.Errorf("flush log up to lsn %d: %w", lsn, err)
	}

	return nil
}

// Rollback undoes everything the transaction did and marks it as finished.
//
// The order mirrors Commit: the restored blocks are written out before the
// rollback record is appended, so the record never claims a rollback that is
// not on disk yet. A crash in between leaves the transaction looking unfinished
// and recovery undoes it again, which is harmless because restoring a
// pre-image twice yields the same block.
func (rm *RecoveryManager) Rollback() error {
	if err := rm.undoOwnRecords(); err != nil {
		return err
	}

	if err := rm.bufferManager.FlushAll(rm.txNum); err != nil {
		return fmt.Errorf("flush buffers of tx %d: %w", rm.txNum, err)
	}

	lsn, err := WriteRollbackRecordToLog(rm.logManager, rm.txNum)
	if err != nil {
		return fmt.Errorf("write rollback record for tx %d: %w", rm.txNum, err)
	}

	if err := rm.logManager.Flush(lsn); err != nil {
		return fmt.Errorf("flush log up to lsn %d: %w", lsn, err)
	}

	return nil
}

// undoOwnRecords walks the log backwards, restoring the pre-image of every
// record this transaction wrote.
//
// Reading backwards is what makes repeated changes to the same value come out
// right: the earliest pre-image is applied last. The walk stops at the
// transaction's own start record, since nothing written before it began can
// belong to it.
func (rm *RecoveryManager) undoOwnRecords() error {
	it, err := rm.logManager.Iterator()
	if err != nil {
		return fmt.Errorf("read the log to roll back tx %d: %w", rm.txNum, err)
	}

	for it.HasNext() {
		bytes, err := it.Next()
		if err != nil {
			return fmt.Errorf("read the log to roll back tx %d: %w", rm.txNum, err)
		}

		rec, err := CreateLogRecord(bytes)
		if err != nil {
			return fmt.Errorf("roll back tx %d: %w", rm.txNum, err)
		}
		if rec.TxNumber() != rm.txNum {
			continue
		}
		if rec.Op() == Start {
			return nil
		}

		undoable, ok := rec.(Undoable)
		if !ok {
			continue
		}
		if err := rm.undo(undoable); err != nil {
			return err
		}
	}

	return nil
}

// undo restores one record's pre-image. The record only knows how to write the
// value into a page, so pinning the block it belongs to, marking the buffer as
// modified and unpinning are done here.
//
// The buffer is marked with an LSN of -1 because an undo writes no log record
// of its own, so there is nothing the buffer has to wait for before it can be
// flushed.
func (rm *RecoveryManager) undo(u Undoable) error {
	blk := u.Block()

	buf, err := rm.bufferManager.Pin(blk)
	if err != nil {
		return fmt.Errorf("pin %s to undo a record of tx %d: %w", blk, rm.txNum, err)
	}
	defer rm.bufferManager.Unpin(buf)

	if err := u.Undo(buf.Contents()); err != nil {
		return fmt.Errorf("undo a record of tx %d: %w", rm.txNum, err)
	}
	buf.SetModified(rm.txNum, -1)

	return nil
}

// Recover undoes every transaction that the log shows as unfinished, which is
// what the database does on start up after a crash.
//
// It ends by writing a checkpoint, so that a later recovery can stop there
// instead of walking the whole log again.
func (rm *RecoveryManager) Recover() error {
	if err := rm.undoUnfinishedRecords(); err != nil {
		return err
	}

	if err := rm.bufferManager.FlushAll(rm.txNum); err != nil {
		return fmt.Errorf("flush buffers of tx %d: %w", rm.txNum, err)
	}

	lsn, err := WriteCheckpointRecordToLog(rm.logManager)
	if err != nil {
		return fmt.Errorf("write checkpoint record: %w", err)
	}

	if err := rm.logManager.Flush(lsn); err != nil {
		return fmt.Errorf("flush log up to lsn %d: %w", lsn, err)
	}

	return nil
}

// undoUnfinishedRecords walks the log backwards, restoring the pre-image of
// every record whose transaction never committed or rolled back.
//
// Walking backwards is what makes the finished set usable: a transaction's
// commit or rollback record is written after everything else it did, so it is
// always seen before the records it settles. It is also what makes repeated
// changes to the same value come out right, since the earliest pre-image is
// applied last.
//
// The walk stops at a checkpoint. A checkpoint is only written when no
// transaction is in progress and every buffer has been flushed, so nothing
// before it can need undoing.
func (rm *RecoveryManager) undoUnfinishedRecords() error {
	it, err := rm.logManager.Iterator()
	if err != nil {
		return fmt.Errorf("read the log to recover: %w", err)
	}

	finished := make(map[int]struct{})
	for it.HasNext() {
		bytes, err := it.Next()
		if err != nil {
			return fmt.Errorf("read the log to recover: %w", err)
		}

		rec, err := CreateLogRecord(bytes)
		if err != nil {
			return fmt.Errorf("recover: %w", err)
		}

		switch rec.Op() {
		case Checkpoint:
			return nil
		case Commit, Rollback:
			finished[rec.TxNumber()] = struct{}{}
			continue
		}

		if _, done := finished[rec.TxNumber()]; done {
			continue
		}

		undoable, ok := rec.(Undoable)
		if !ok {
			continue
		}
		if err := rm.undo(undoable); err != nil {
			return err
		}
	}

	return nil
}
