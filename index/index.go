package index

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Field names of an index record.
//
// An index is stored as a table, and each of its records is one index entry.
// The first two fields hold the record id of a table record. The third field
// holds the key of that record.
//
// The names are exported for the catalog. The catalog builds the layout of
// index records from them before any index is open.
const (
	// BlockNumberField holds the block number of the indexed table record.
	BlockNumberField = "block_number"
	// SlotField holds the slot of that record within its block. The block
	// number and the slot together form a record id. A record id lets a scan
	// move straight to a record without reading the records before it.
	SlotField = "slot"
	// IndexedValueField holds the key: a copy of the indexed field's value in
	// that record. A search compares against this copy, so it does not have
	// to read the table.
	IndexedValueField = "indexed_value"
)

// Index maps a value of one field to the table records that hold that value.
//
// Each entry pairs a key with a record id. The key is the indexed field's
// value in a record, and the record id locates that record. A search selects
// one key and reads the entries for it. The entries locate the matching
// records, so a select on the indexed field does not scan the whole table.
//
// An index is read like a scan: one entry at a time, forward only. It visits
// only the entries for the search key. A search starts before the first of
// these entries. Reading them again, or reading the entries for another key,
// requires a new search.
//
// An index returns record ids, not records. To read a record, the caller
// moves a scan of the table to the record id.
//
// The table does not maintain its indexes. A statement that changes a record
// of an indexed table must call Insert or Delete on each index. Otherwise a
// search returns deleted records and misses inserted ones.
//
// Callers must call Close. An index pins blocks while it is being read, like
// a scan. An index left open holds its buffers until the transaction ends.
type Index interface {
	// MoveBeforeFirstMatch positions the index before the first entry for
	// searchKey, so that the next call to MoveToNextMatch moves to that entry.
	// It discards any search in progress: an index searches for one key at a
	// time.
	MoveBeforeFirstMatch(searchKey query.Constant) error
	// MoveToNextMatch moves to the next entry for the search key and reports
	// whether one exists. False means that all entries for the key have been
	// read. It is not an error. The index never moves to an entry for another
	// key.
	MoveToNextMatch() (bool, error)
	// DataRecordID returns the record id of the table record that the current
	// entry points to.
	//
	// This is a record id in the indexed table, not the position of the entry
	// inside the index. CurrentRecordID on an update scan, by contrast,
	// returns the scan's own position.
	DataRecordID() (*recordmanager.RecordID, error)
	// Insert adds an entry that maps value to rid. Call it when a record is
	// added to the table, or when a record's indexed field is set to value.
	Insert(value query.Constant, rid *recordmanager.RecordID) error
	// Delete removes the entry that maps value to rid. Call it when a record
	// is removed from the table, or when a record's indexed field no longer
	// holds value.
	Delete(value query.Constant, rid *recordmanager.RecordID) error
	Close()
}
