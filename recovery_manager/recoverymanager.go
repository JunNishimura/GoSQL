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
