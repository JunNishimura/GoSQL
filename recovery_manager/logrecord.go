package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// Every record starts with its op code. In the records that mark a
// transaction boundary (start, commit, rollback), the transaction number
// follows, and the record ends there.
const (
	opOffset     = 0
	txNumOffset  = filemanager.IntBytes
	txRecordSize = 2 * filemanager.IntBytes
)

// noTxNum is the transaction number of records that belong to no transaction.
// It equals the marker that Buffer uses for a buffer that no transaction has
// modified.
const noTxNum = -1

// Op identifies the kind of a log record. Its value is written to the log
// file as the first int of every record. It is therefore part of the on-disk
// format, and existing values must not be reordered.
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
// backwards and looks for the records of a transaction that it must roll
// back.
type LogRecord interface {
	// Op reports the kind of this record.
	Op() Op
	// TxNumber reports the transaction that wrote this record.
	TxNumber() int
}

// Undoable is implemented by the records that changed data and therefore have
// a pre-image to restore. The records that only mark a transaction boundary
// do not implement it, so recovery skips them.
//
// Undo only writes into a page and does not perform the whole restore. A
// record therefore needs no knowledge of buffers or transactions. The caller
// pins the block, marks the buffer as modified and unpins it.
type Undoable interface {
	LogRecord
	// Block reports which block this record changed.
	Block() *filemanager.BlockId
	// Undo writes the pre-image into p, which must hold Block().
	Undo(p *filemanager.Page) error
}

// CreateLogRecord decodes the record stored in bytes. Recovery reads the log
// as raw bytes, so an op code first becomes a typed record here. A malformed
// record must therefore be rejected here.
func CreateLogRecord(record []byte) (LogRecord, error) {
	if len(record) < filemanager.IntBytes {
		return nil, fmt.Errorf("log record of %d bytes is too short to hold an op code", len(record))
	}

	p := filemanager.NewPageByBytes(record)
	op := Op(p.GetInt(opOffset))

	// Each constructor validates the rest of the layout, so this switch only
	// dispatches. Each result is assigned to a variable before it is returned.
	// A typed nil pointer returned directly as a LogRecord would not compare
	// equal to nil.
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
	case SetInt:
		rec, err := NewSetIntRecord(record)
		if err != nil {
			return nil, err
		}
		return rec, nil
	case SetString:
		rec, err := NewSetStringRecord(record)
		if err != nil {
			return nil, err
		}
		return rec, nil
	default:
		return nil, fmt.Errorf("unknown log record op %d", op)
	}
}

// readTxRecord decodes the transaction number from a record whose only
// payload is that number. It first checks that the record is long enough.
// Page.GetInt does not check bounds, so a truncated record would otherwise
// cause a read past the end of the slice.
func readTxRecord(record []byte, op Op) (int, error) {
	if len(record) < txRecordSize {
		return 0, fmt.Errorf("log record for op %d is %d bytes, want at least %d", op, len(record), txRecordSize)
	}
	p := filemanager.NewPageByBytes(record)
	return int(p.GetInt(txNumOffset)), nil
}

// newTxRecord encodes a record whose only payload is a transaction number.
// The start, commit and rollback records share this layout.
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
