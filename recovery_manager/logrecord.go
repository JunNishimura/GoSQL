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
func CreateLogRecord(bytes []byte) (LogRecord, error) {
	if len(bytes) < filemanager.IntBytes {
		return nil, fmt.Errorf("log record of %d bytes is too short to hold an op code", len(bytes))
	}

	p := filemanager.NewPageByBytes(bytes)
	op := Op(p.GetInt(opOffset))

	switch op {
	case Checkpoint:
		return NewCheckpointRecord(), nil
	case Start:
		if err := checkTxRecordSize(op, bytes); err != nil {
			return nil, err
		}
		return NewStartRecord(p), nil
	case Commit:
		if err := checkTxRecordSize(op, bytes); err != nil {
			return nil, err
		}
		return NewCommitRecord(p), nil
	case Rollback:
		if err := checkTxRecordSize(op, bytes); err != nil {
			return nil, err
		}
		return NewRollbackRecord(p), nil
	case SetInt, SetString:
		return nil, fmt.Errorf("op %d: %w", op, ErrUnimplementedRecord)
	default:
		return nil, fmt.Errorf("unknown log record op %d", op)
	}
}

// checkTxRecordSize guards the reads that follow the op code. Page.GetInt does
// not bounds check, so a truncated record would panic rather than fail.
func checkTxRecordSize(op Op, bytes []byte) error {
	if len(bytes) < txRecordSize {
		return fmt.Errorf("log record for op %d is %d bytes, want at least %d", op, len(bytes), txRecordSize)
	}
	return nil
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
