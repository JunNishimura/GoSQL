package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// RollbackRecord marks the point at which a transaction was rolled back.
type RollbackRecord struct {
	txNum int
}

func NewRollbackRecord(p *filemanager.Page) *RollbackRecord {
	return &RollbackRecord{txNum: int(p.GetInt(txNumOffset))}
}

func (r *RollbackRecord) Op() Op {
	return Rollback
}

func (r *RollbackRecord) TxNumber() int {
	return r.txNum
}

// Undo does nothing: a rollback record records no data change, so there is
// nothing to restore.
func (r *RollbackRecord) Undo(txNum int) error {
	return nil
}

func (r *RollbackRecord) String() string {
	return fmt.Sprintf("<ROLLBACK %d>", r.txNum)
}

// WriteRollbackRecordToLog appends a rollback record for txNum to the log and
// returns its LSN.
func WriteRollbackRecordToLog(lm *logmanager.LogManager, txNum int) (int, error) {
	record, err := newTxRecord(Rollback, txNum)
	if err != nil {
		return 0, err
	}
	return lm.Append(record)
}
