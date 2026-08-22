package recoverymanager

import (
	"fmt"

	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// CommitRecord marks the point at which a transaction committed.
type CommitRecord struct {
	txNum int
}

func NewCommitRecord(record []byte) (*CommitRecord, error) {
	txNum, err := readTxRecord(record, Commit)
	if err != nil {
		return nil, err
	}
	return &CommitRecord{txNum: txNum}, nil
}

func (r *CommitRecord) Op() Op {
	return Commit
}

func (r *CommitRecord) TxNumber() int {
	return r.txNum
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
