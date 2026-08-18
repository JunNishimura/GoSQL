package recoverymanager

const intBytes = 4

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
