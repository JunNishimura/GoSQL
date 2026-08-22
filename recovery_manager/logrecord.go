package recoverymanager

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// ErrUnimplementedRecord reports an op code that this package reserves but has
// no record type for yet. Callers can tell it apart from a corrupted log, where
// the op code itself is not one we ever write.
var ErrUnimplementedRecord = errors.New("log record type not implemented")

// Every record starts with its op code. The records that describe a
// transaction boundary (start, commit, rollback) follow it with the
// transaction number and end there.
const (
	opOffset     = 0
	txNumOffset  = filemanager.IntBytes
	txRecordSize = 2 * filemanager.IntBytes
)

// noTxNum is reported by records that belong to no transaction. It matches the
// marker Buffer uses for a buffer that no transaction has modified.
const noTxNum = -1

// Op identifies the kind of a log record. The value is written into the log
// file as the first int of every record, so it is part of the on-disk format
// and existing values must not be reordered.
type Op int

const (
	Checkpoint Op = iota
	Start
	Commit
	Rollback
	SetInt
	SetString
)

// LogRecord is a single entry in the log. The recovery manager reads the log
// backwards, looking for the records that belong to a transaction it needs to
// roll back.
type LogRecord interface {
	// Op reports the kind of this record.
	Op() Op
	// TxNumber reports the transaction that wrote this record.
	TxNumber() int
}

// Undoable is implemented by the records that changed data and therefore have
// a pre-image to restore. The records that only mark a transaction boundary do
// not implement it, so recovery skips them.
//
// Undo writes into a page rather than performing the whole restore, so that a
// record needs to know nothing about buffers or transactions. Pinning the
// block, marking the buffer modified and unpinning belong to the caller.
type Undoable interface {
	LogRecord
	// Block reports which block this record changed.
	Block() *filemanager.BlockId
	// Undo writes the pre-image into p, which must hold Block().
	Undo(p *filemanager.Page) error
}

// CreateLogRecord rebuilds the record stored in bytes. Recovery reads the log
// as raw bytes, so this is where an op code first becomes a typed record and
// therefore where a malformed record has to be rejected.
func CreateLogRecord(record []byte) (LogRecord, error) {
	if len(record) < filemanager.IntBytes {
		return nil, fmt.Errorf("log record of %d bytes is too short to hold an op code", len(record))
	}

	p := filemanager.NewPageByBytes(record)
	op := Op(p.GetInt(opOffset))

	// Each constructor validates the rest of the layout, so this switch only
	// dispatches. The results are assigned before being returned because a typed
	// nil pointer put straight into a LogRecord would not compare equal to nil.
	switch op {
	case Checkpoint:
		return NewCheckpointRecord(), nil
	case Start:
		rec, err := NewStartRecord(record)
		if err != nil {
			return nil, err
		}
		return rec, nil
	case Commit:
		rec, err := NewCommitRecord(record)
		if err != nil {
			return nil, err
		}
		return rec, nil
	case Rollback:
		rec, err := NewRollbackRecord(record)
		if err != nil {
			return nil, err
		}
		return rec, nil
	case SetInt, SetString:
		return nil, fmt.Errorf("op %d: %w", op, ErrUnimplementedRecord)
	default:
		return nil, fmt.Errorf("unknown log record op %d", op)
	}
}

// readTxRecord decodes the transaction number out of a record whose only
// payload is one, and checks that the record is long enough to hold it.
// Page.GetInt does not bounds check, so a truncated record would otherwise be
// read past the end of the slice.
func readTxRecord(record []byte, op Op) (int, error) {
	if len(record) < txRecordSize {
		return 0, fmt.Errorf("log record for op %d is %d bytes, want at least %d", op, len(record), txRecordSize)
	}
	p := filemanager.NewPageByBytes(record)
	return int(p.GetInt(txNumOffset)), nil
}

// newTxRecord encodes a record whose only payload is a transaction number,
// which is the layout shared by the start, commit and rollback records.
func newTxRecord(op Op, txNum int) ([]byte, error) {
	record := make([]byte, txRecordSize)
	p := filemanager.NewPageByBytes(record)
	if err := p.SetInt(opOffset, int32(op)); err != nil {
		return nil, fmt.Errorf("set op of %d record: %w", op, err)
	}
	if err := p.SetInt(txNumOffset, int32(txNum)); err != nil {
		return nil, fmt.Errorf("set txNum of %d record: %w", op, err)
	}
	return record, nil
}
