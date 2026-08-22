package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// A checkpoint belongs to no transaction, so the op code is the whole record.
const checkpointRecordSize = filemanager.IntBytes

// CheckpointRecord marks a point at which no transaction was in progress.
type CheckpointRecord struct{}

func NewCheckpointRecord() *CheckpointRecord {
	return &CheckpointRecord{}
}

func (r *CheckpointRecord) Op() Op {
	return Checkpoint
}

// TxNumber reports noTxNum because a checkpoint is not written on behalf of
// any transaction.
func (r *CheckpointRecord) TxNumber() int {
	return noTxNum
}

func (r *CheckpointRecord) String() string {
	return "<CHECKPOINT>"
}

// WriteCheckpointRecordToLog appends a checkpoint record to the log and
// returns its LSN.
func WriteCheckpointRecordToLog(lm *logmanager.LogManager) (int, error) {
	record := make([]byte, checkpointRecordSize)
	p := filemanager.NewPageByBytes(record)
	if err := p.SetInt(opOffset, int32(Checkpoint)); err != nil {
		return 0, fmt.Errorf("set op of checkpoint record: %w", err)
	}
	return lm.Append(record)
}
