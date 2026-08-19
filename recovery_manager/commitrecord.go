package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// CommitRecord marks the point at which a transaction committed.
type CommitRecord struct {
	txNum int
}

func NewCommitRecord(p *filemanager.Page) *CommitRecord {
	return &CommitRecord{txNum: int(p.GetInt(txNumOffset))}
}

func (r *CommitRecord) Op() Op {
	return Commit
}

func (r *CommitRecord) TxNumber() int {
	return r.txNum
}

// Undo does nothing: a commit record records no data change, so there is
// nothing to restore.
func (r *CommitRecord) Undo(txNum int) error {
	return nil
}

func (r *CommitRecord) String() string {
	return fmt.Sprintf("<COMMIT %d>", r.txNum)
}

// WriteCommitRecordToLog appends a commit record for txNum to the log and
// returns its LSN.
func WriteCommitRecordToLog(lm *logmanager.LogManager, txNum int) (int, error) {
	record, err := newTxRecord(Commit, txNum)
	if err != nil {
		return 0, err
	}
	return lm.Append(record)
}
