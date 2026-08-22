package recoverymanager

import (
	"fmt"

	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// RollbackRecord marks the point at which a transaction was rolled back.
type RollbackRecord struct {
	txNum int
}

func NewRollbackRecord(record []byte) (*RollbackRecord, error) {
	txNum, err := readTxRecord(record, Rollback)
	if err != nil {
		return nil, err
	}
	return &RollbackRecord{txNum: txNum}, nil
}

func (r *RollbackRecord) Op() Op {
	return Rollback
}

func (r *RollbackRecord) TxNumber() int {
	return r.txNum
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
