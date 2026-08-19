package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

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
// backwards and calls Undo on the records that belong to a transaction it needs
// to roll back.
type LogRecord interface {
	// Op reports the kind of this record.
	Op() Op
	// TxNumber reports the transaction that wrote this record.
	TxNumber() int
	// Undo restores the state this record overwrote, on behalf of the
	// transaction identified by txNum. Records that changed no data return nil
	// without doing any work.
	Undo(txNum int) error
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
