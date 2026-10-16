package index

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The fields of an index's own records.
//
// An index is kept as a table like anything else, and one of its records says
// that a value is held somewhere: the first two fields are a record id taken
// apart, and the third is the value that record is filed under.
//
// They are named here, where they are read and written, and are exported for
// the catalog, which lays the records out from them before any index is open.
const (
	// BlockNumberField is the block of the indexed table the record sits in.
	BlockNumberField = "block_number"
	// SlotField is the slot of that block. Together with the block number it is
	// a record id, which is how a table is read from an index without reading
	// the records before the one wanted.
	SlotField = "slot"
	// IndexedValueField is a copy of what the indexed field holds in that
	// record. It is what the index is searched by, so it is kept here rather
	// than read from the table, which is the whole point of having the index.
	IndexedValueField = "indexed_value"
)

// Index is a lookup from a value of one field to the records of a table that
// hold it.
//
// Each of its entries files a record id under a value: the value the indexed
// field holds in that record, and where the record is kept. Searching means
// picking a value and reading the entries filed under it, which hands back
// where the matching records are without reading the ones that do not match.
// That is the whole reason to have one: a select on the indexed field reads
// the records it wants rather than the whole table.
//
// It is read the way a scan is, one entry at a time and in one direction, but
// only over the entries of the value searched for. A search starts before the
// first of them, moves forward until there are none left, and to read them
// again or read another value's, starts over with a search.
//
// What it hands back is a record id, not a record. The index does not hold
// the table's records, so reading one means taking the id to the table and
// moving a scan of it there.
//
// Insert and Delete are here because an index is not kept up to date by the
// table. A statement that changes a record of an indexed table has to change
// the index alongside it, or a search will go on finding records that are gone
// and missing ones that were added.
//
// Close has to be called. Like a scan, an index holds blocks pinned while it is
// being read, and one left open keeps buffers taken until the transaction ends.
type Index interface {
	// MoveBeforeFirstMatch puts the index before the first entry filed under
	// searchKey, so that the next move arrives at it. Whatever was being read
	// before is dropped: an index searches for one value at a time.
	MoveBeforeFirstMatch(searchKey query.Constant) error
	// MoveToNextMatch moves on to the next entry filed under the search key and
	// reports whether there was one. False means the entries for that value
	// have been read to the end, not that anything went wrong, and entries
	// filed under other values are never arrived at.
	MoveToNextMatch() (bool, error)
	// DataRecordID is where the table record of the current entry is kept.
	//
	// It is the record id of the indexed table, not of the entry inside the
	// index, which is what sets it apart from CurrentRecordID on an update
	// scan.
	DataRecordID() (*recordmanager.RecordID, error)
	// Insert files rid under value, for a record that has been added to the
	// table or whose indexed field has been given value.
	Insert(value query.Constant, rid *recordmanager.RecordID) error
	// Delete takes out the entry filing rid under value, for a record that has
	// been removed from the table or whose indexed field no longer holds value.
	Delete(value query.Constant, rid *recordmanager.RecordID) error
	Close()
}
