package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// StartRecord marks the point at which a transaction began.
type StartRecord struct {
	txNum int
}

func NewStartRecord(p *filemanager.Page) *StartRecord {
	return &StartRecord{txNum: int(p.GetInt(txNumOffset))}
}

func (r *StartRecord) Op() Op {
	return Start
}

func (r *StartRecord) TxNumber() int {
	return r.txNum
}

// Undo does nothing: a start record records no data change, so there is
// nothing to restore.
func (r *StartRecord) Undo(txNum int) error {
	return nil
}

func (r *StartRecord) String() string {
	return fmt.Sprintf("<START %d>", r.txNum)
}

// WriteStartRecordToLog appends a start record for txNum to the log and
// returns its LSN.
func WriteStartRecordToLog(lm *logmanager.LogManager, txNum int) (int, error) {
	record, err := newTxRecord(Start, txNum)
	if err != nil {
		return 0, err
	}
	return lm.Append(record)
}
