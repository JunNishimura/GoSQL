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
