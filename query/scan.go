package query

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Scan is a sequence of records that is read one record at a time, from start
// to end.
//
// A table is a scan. So is the result of selecting rows from a scan,
// projecting fields from a scan, or joining two scans. This is how the
// operators of a query compose: each operator takes scans and is a scan
// itself. A query is therefore a tree of scans, and running it means reading
// the scan at the root.
//
// A scan is read in one direction. It starts before its first record and
// moves forward one record at a time until none are left. It can only return
// to an earlier record by starting over. The interface does not report the
// number of records and has no access by position, so an operator that must
// read its input twice scans it twice.
//
// HasField exists because the fields of a scan come from its inputs, which
// the caller does not otherwise know. A query over two tables asks each scan
// to find out which table a field name belongs to.
//
// Callers must call Close. A scan pins blocks while it is being read, and a
// scan built on other scans closes them in turn. A scan left open holds its
// buffers until the transaction ends.
type Scan interface {
	// MoveBeforeFirstRecord positions the scan before its first record, so
	// that the next call to MoveToNextRecord moves to the first record.
	MoveBeforeFirstRecord() error
	// MoveToNextRecord moves to the next record and reports whether one
	// exists. False means that the scan has reached its end. It is not an
	// error.
	MoveToNextRecord() (bool, error)
	GetInt(fieldName string) (int32, error)
	GetString(fieldName string) (string, error)
	// GetValue returns the field as a constant of the field's type. Most
	// operators do not depend on the type of a field, and read through this
	// method.
	GetValue(fieldName string) (Constant, error)
	HasField(fieldName string) bool
	Close()
}

// UpdateScan is a scan whose records can also be modified, inserted and
// deleted.
//
// Not every scan supports this. A projection has dropped fields that a
// complete record needs, and a record of a product is not backed by a single
// stored record. The write methods are therefore a second interface and not
// part of Scan. A statement that changes the database requires an UpdateScan,
// so it cannot be planned over an input that cannot perform the change.
//
// Record ids are part of this interface for the same reason. A record id
// identifies where a record is stored, and only a scan over stored records
// has such a position.
type UpdateScan interface {
	Scan

	SetInt(fieldName string, val int32) error
	SetString(fieldName string, val string) error
	// SetValue writes a constant to the field, according to the constant's
	// type.
	SetValue(fieldName string, val Constant) error
	// MoveToNewRecord positions the scan on a new record in a free slot. The
	// caller then sets its fields.
	MoveToNewRecord() error
	// DeleteCurrentRecord deletes the current record from the table. The
	// position of the scan does not change.
	DeleteCurrentRecord() error
	CurrentRecordID() (*recordmanager.RecordID, error)
	MoveToRecordID(rid *recordmanager.RecordID) error
}
