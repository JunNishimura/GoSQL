package recordmanager

import "fmt"

// RecordID is the position of one record in a table: a block number in the
// table's file, and a slot in that block.
//
// A record id identifies a record without holding it. A query can therefore
// keep a list of the records it has found and return to them later, without
// keeping their blocks pinned.
//
// The file name is not part of a record id. A record id is only meaningful
// together with its table, so records of different tables can have the same
// id. The holder of a record id is expected to know which table it belongs
// to.
type RecordID struct {
	blkNum int
	slot   int
}

// NewRecordID returns the id of the record at the given slot of the given
// block.
func NewRecordID(blkNum, slot int) *RecordID {
	return &RecordID{
		blkNum: blkNum,
		slot:   slot,
	}
}

// BlockNumber returns the number of the block that holds the record, within
// the table's file.
//
// Callers outside this package use it to determine how far into a table a
// record is. The number of blocks in a table is counted this way: the last
// record found is in the last block that holds a record.
func (r *RecordID) BlockNumber() int {
	return r.blkNum
}

// Slot returns the slot of the record within its block.
//
// The block number and the slot together form the whole record id. A scan
// that moves to a record id needs both: the block to open, and the slot to
// position on.
func (r *RecordID) Slot() int {
	return r.slot
}

// Equals reports whether other refers to the same slot of the same block.
func (r *RecordID) Equals(other *RecordID) bool {
	return r.blkNum == other.blkNum && r.slot == other.slot
}

func (r *RecordID) String() string {
	return fmt.Sprintf("[block %d, slot %d]", r.blkNum, r.slot)
}
