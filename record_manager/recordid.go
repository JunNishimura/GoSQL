package recordmanager

import "fmt"

// RecordID is where one record sits in a table: which block of the table's
// file, and which slot of that block.
//
// It names a record without holding it, which is what lets a query keep a list
// of records it has found and come back to them later, rather than keeping the
// blocks pinned in the meantime.
//
// The file is not part of it. A record id only means something alongside the
// table it came from, so two records of different tables can have the same one,
// and whoever holds it is expected to know which table that is.
type RecordID struct {
	blkNum int
	slot   int
}

// NewRecordID returns the id of the record in the given slot of the given
// block.
func NewRecordID(blkNum, slot int) *RecordID {
	return &RecordID{
		blkNum: blkNum,
		slot:   slot,
	}
}

// BlockNumber is the block of the table's file the record sits in.
//
// It is what lets a caller outside this package tell how far through a table a
// record is, which is how the blocks a table takes up are counted: the last
// record found is in the last block that holds anything.
func (r *RecordID) BlockNumber() int {
	return r.blkNum
}

// Equals reports whether other names the same slot of the same block.
func (r *RecordID) Equals(other *RecordID) bool {
	return r.blkNum == other.blkNum && r.slot == other.slot
}

func (r *RecordID) String() string {
	return fmt.Sprintf("[block %d, slot %d]", r.blkNum, r.slot)
}
