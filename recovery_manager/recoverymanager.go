package recoverymanager

import (
	"fmt"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// RecoveryManager writes the log records of a single transaction and undoes
// them when that transaction is rolled back. Each transaction owns one
// instance, so txNum is fixed at construction.
type RecoveryManager struct {
	logManager    *logmanager.LogManager
	bufferManager *buffermanager.BufferManager
	txNum         int
}

// NewRecoveryManager creates the recovery manager for transaction txNum and
// appends its start record, so that the transaction is on the log from the
// moment it begins.
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

// Commit makes the changes of the transaction permanent.
//
// The steps are ordered so that the commit record is never on disk before the
// data it commits. The modified buffers are written first, and the commit
// record is appended and flushed after that. After a crash between the two
// steps, the transaction appears unfinished, and recovery can undo it. In the
// reverse order, a crash could leave a committed transaction whose changes
// were lost.
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

// LogSetInt logs the current int at offset in buf, before the caller
// overwrites it, and returns the LSN of the log record.
//
// LogSetInt reads the pre-image itself and does not take it from the caller.
// The logged value is therefore always the value that was in the buffer, and
// the caller cannot log the new value by mistake. This matches what undo
// expects. The caller writes the new value afterwards and passes the returned
// LSN to Buffer.SetModified, so the record is on the log before the buffer
// can be flushed.
func (rm *RecoveryManager) LogSetInt(buf *buffermanager.Buffer, offset int) (int, error) {
	oldVal := buf.Contents().GetInt(offset)

	lsn, err := WriteSetIntRecordToLog(rm.logManager, rm.txNum, buf.Block(), offset, oldVal)
	if err != nil {
		return 0, fmt.Errorf("log the int at offset %d of %s for tx %d: %w", offset, buf.Block(), rm.txNum, err)
	}
	return lsn, nil
}

// LogSetString logs the current string at offset in buf, before the caller
// overwrites it, and returns the LSN of the log record. Like LogSetInt, it
// reads the pre-image itself.
func (rm *RecoveryManager) LogSetString(buf *buffermanager.Buffer, offset int) (int, error) {
	oldVal := buf.Contents().GetString(offset)

	lsn, err := WriteSetStringRecordToLog(rm.logManager, rm.txNum, buf.Block(), offset, oldVal)
	if err != nil {
		return 0, fmt.Errorf("log the string at offset %d of %s for tx %d: %w", offset, buf.Block(), rm.txNum, err)
	}
	return lsn, nil
}

// Rollback undoes every change of the transaction and marks it as finished.
//
// The order mirrors Commit. The restored blocks are written before the
// rollback record is appended, so the record never reports a rollback that is
// not on disk. After a crash between the two steps, the transaction appears
// unfinished and recovery undoes it again. This is harmless, because
// restoring a pre-image twice gives the same block.
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

// undoOwnRecords reads the log backwards and restores the pre-image of every
// record that this transaction wrote.
//
// The backward order handles repeated changes to the same value correctly,
// because the earliest pre-image is applied last. The scan stops at the
// transaction's own start record, because no earlier record can belong to the
// transaction.
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

// undo restores the pre-image of one record. The record only writes the value
// into a page. undo therefore pins the block of the record, marks the buffer
// as modified and unpins it.
//
// The buffer is marked with an LSN of -1. An undo writes no log record, so
// the buffer does not depend on a log flush before it can be written.
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

// Recover undoes every transaction that the log shows as unfinished. The
// database calls it at startup after a crash.
//
// It ends by writing a checkpoint, so that a later recovery can stop there
// and does not scan the whole log again.
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

// undoUnfinishedRecords reads the log backwards and restores the pre-image of
// every record whose transaction neither committed nor rolled back.
//
// The backward order makes the set of finished transactions reliable. The
// commit or rollback record of a transaction is written after all its other
// records, so the scan always reads it before them. The backward order also
// handles repeated changes to the same value correctly, because the earliest
// pre-image is applied last.
//
// The scan stops at a checkpoint. A checkpoint is only written when no
// transaction is in progress and every buffer has been flushed, so no record
// before it needs an undo.
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
