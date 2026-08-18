package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// A commit record is laid out as the op code followed by the transaction number.
const (
	opOffset         = 0
	txNumOffset      = filemanager.IntBytes
	commitRecordSize = 2 * filemanager.IntBytes
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
	record := make([]byte, commitRecordSize)
	p := filemanager.NewPageByBytes(record)
	if err := p.SetInt(opOffset, int32(Commit)); err != nil {
		return 0, fmt.Errorf("set op of commit record: %w", err)
	}
	if err := p.SetInt(txNumOffset, int32(txNum)); err != nil {
		return 0, fmt.Errorf("set txNum of commit record: %w", err)
	}
	return lm.Append(record)
}
