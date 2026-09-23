package query

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Scan is a run of records read one at a time, from the start to the end.
//
// A table is one, and so is the result of selecting rows out of a scan,
// projecting fields out of one, or joining two together. That is what makes the
// operators of a query fit together: each of them takes scans and is one
// itself, so a query is a tree of them and running it is reading the scan at
// the root.
//
// Reading goes one way. A scan starts before its first record, moves forward a
// record at a time until there are none left, and gets back to an earlier
// record only by starting over. Nothing here says how many records there are,
// or reaches one by number, so an operator that has to read its input twice
// reads it twice.
//
// HasField is here because the fields a scan has are the fields of whatever it
// draws from, which the caller does not otherwise know: a query over two tables
// works out which of them a name belongs to by asking each.
//
// Close has to be called. A scan holds blocks pinned while it is being read,
// and a scan built on others closes those in turn, so one left open keeps
// buffers taken until the transaction ends.
type Scan interface {
	// MoveBeforeFirstRecord puts the scan back before its first record, so that
	// the next move arrives at the first.
	MoveBeforeFirstRecord() error
	// MoveToNextRecord moves on to the next record and reports whether there
	// was one. False means the scan has been read to the end, not that
	// anything went wrong.
	MoveToNextRecord() (bool, error)
	GetInt(fieldName string) (int32, error)
	GetString(fieldName string) (string, error)
	// GetValue returns the field as a constant, of whichever kind the field is.
	// It is what an operator reads through when it does not care which kind
	// that is, which is most of them.
	GetValue(fieldName string) (Constant, error)
	HasField(fieldName string) bool
	Close()
}

// UpdateScan is a scan whose records can also be written to, added and removed.
//
// Not every scan can be. A projection has dropped fields that a whole record
// still needs, and a product has no one record standing behind the one it hands
// out. So the writes are a second interface rather than part of the first, and
// a statement that changes the database asks for this one, which is what keeps
// it from being planned over an input that could not carry it out.
//
// Record ids are here for the same reason. One names where a record is kept,
// and only a scan standing for records that are really stored has anywhere to
// point.
type UpdateScan interface {
	Scan

	SetInt(fieldName string, val int32) error
	SetString(fieldName string, val string) error
	// SetValue writes a constant to the field, whichever kind the constant is.
	SetValue(fieldName string, val Constant) error
	// MoveToNewRecord puts the scan on a record of its own, taken from a free
	// slot, for the caller to fill in.
	MoveToNewRecord() error
	// DeleteCurrentRecord takes the record the scan is on out of the table,
	// leaving the scan where it is.
	DeleteCurrentRecord() error
	CurrentRecordID() (*recordmanager.RecordID, error)
	MoveToRecordID(rid *recordmanager.RecordID) error
}
