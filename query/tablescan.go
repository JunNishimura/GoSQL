package query

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNoCurrentRecord reports reading or writing a scan that is not on a record:
// one that has not been moved to its first record yet, or one that has run past
// the last.
//
// It is kept apart from recordmanager.ErrSlotOutOfRange, which the record page
// raises for the same slot number, because the two say different things to
// whoever gets them. Out of range means a slot that could not exist; this one
// means the scan has not been asked to go anywhere.
var ErrNoCurrentRecord = errors.New("no current record")

// tableFileExtension is what a table's name is turned into a file name with.
// One table is one file, so the name of the table is enough to find it.
const tableFileExtension = ".tbl"

// beforeFirstSlot is where a scan sits when it has moved to a block but has not
// yet looked at any of its slots.
//
// It is one before the first slot rather than the first, because the searches a
// record page offers start past the slot they are given: the scan can hand this
// back and be given slot 0.
const beforeFirstSlot = -1

// TableScan is a walk over every record of one table, block by block.
//
// A record page sees one block, and a table is a file of them, so this is what
// carries a reader from the end of one block to the start of the next, and what
// adds a block when a record has to go somewhere and none of the existing ones
// has room.
//
// It holds one block pinned at a time: the one it is on. Moving off a block
// gives that pin back, which is what keeps a scan of a large table from
// emptying the buffer pool.
type TableScan struct {
	tx     *transaction.Transaction
	layout *recordmanager.Layout
	// rp is the block the scan is on, or nil before it has moved to one.
	rp *recordmanager.RecordPage
	// fileName is the table's file, kept rather than the table's name because
	// nothing below this asks for a table.
	fileName string
	// currentSlot is the slot of rp the scan is on, or beforeFirstSlot.
	currentSlot int
	// blockCount is how many blocks the table's file had when this scan last
	// looked. Asking the file is a call to stat, and a walk asks once per block
	// it leaves, so the answer is kept between those.
	//
	// It can only be behind, never ahead: blocks are added and never taken
	// away. No other transaction can add one, since opening the scan took a
	// shared lock on the file's length, but this transaction can, through this
	// scan or through another one over the same table.
	blockCount int
}

// NewTableScan opens a scan over tableName, placed before the first record.
//
// A table whose file has no blocks yet gets one, so that a scan always has a
// block to work on and whoever inserts into it does not have to treat an empty
// table as a special case.
func NewTableScan(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) (*TableScan, error) {
	ts := &TableScan{
		tx:       tx,
		layout:   layout,
		fileName: tableName + tableFileExtension,
	}

	size, err := tx.Size(ts.fileName)
	if err != nil {
		return nil, err
	}
	ts.blockCount = size

	if size == 0 {
		if err := ts.moveToNewBlock(); err != nil {
			return nil, err
		}

		return ts, nil
	}

	if err := ts.moveToBlock(0); err != nil {
		return nil, err
	}

	return ts, nil
}

// Close gives back the block the scan is holding. Every scan has to be closed,
// or the buffer it is on stays taken until the transaction ends, however long
// ago the scan was finished with.
//
// It returns nothing because nothing here can fail: giving a pin back is a
// count going down. It can be called on a scan that is already closed, so a
// caller may defer it and close early on the paths where it wants the buffer
// back sooner.
//
// A closed scan is on no block, so reading or writing one is ErrNoCurrentRecord
// rather than a use of whatever the buffer went on to hold.
//
// Moving to another block closes the scan as its first step, for the same
// reason a caller does: the block being left is one nothing is going to read.
func (ts *TableScan) Close() {
	if ts.rp == nil {
		return
	}

	ts.tx.Unpin(ts.rp.BlockID())
	ts.rp = nil
}

// MoveBeforeFirstRecord puts the scan back at the start of the table, before
// its first record.
//
// It leaves the scan on no record rather than on the first one: reading a field
// straight after this is ErrNoCurrentRecord, and it takes a move to the next
// record to arrive at the first. That is what lets a walk over a table be one
// loop, with the first record reached the same way as every other.
//
// The table is not changed. The first block is opened as it stands, records and
// all, so this can be called as often as a query needs to read the table again.
func (ts *TableScan) MoveBeforeFirstRecord() error {
	return ts.moveToBlock(0)
}

// MoveToNextRecord puts the scan on the record after the one it is on, and
// reports whether there was one. False means the table has been read to the
// end, not that anything went wrong.
//
// Empty slots are passed over, and so are whole blocks of them: a table that
// has had records deleted reads as the records it still holds, with no gaps for
// the caller to step around.
//
// Running off the end of a block goes on to the next rather than stopping,
// which is what makes a table look like one run of records rather than a file
// of blocks. Only the block being read is held, so a walk over a large table
// takes one buffer however long it runs.
func (ts *TableScan) MoveToNextRecord() (bool, error) {
	if err := ts.requireCurrentBlock(); err != nil {
		return false, err
	}

	for {
		slot, err := ts.rp.NextUsedSlotAfter(ts.currentSlot)
		if err == nil {
			ts.currentSlot = slot
			return true, nil
		}
		if !errors.Is(err, recordmanager.ErrNoSuchSlot) {
			return false, err
		}

		// The block holds no more records. Whether that is the end of the
		// table or only the end of a block is the one thing this cannot tell
		// from the block itself.
		onLastBlock, err := ts.isOnLastBlock()
		if err != nil {
			return false, err
		}
		if onLastBlock {
			return false, nil
		}

		if err := ts.moveToBlock(ts.rp.BlockID().Number() + 1); err != nil {
			return false, err
		}
	}
}

// MoveToNewRecord puts the scan on a record of its own, taken from the first
// free slot at or after where it is, and leaves the fields at the values an
// empty slot holds. Filling them in is the caller's next step.
//
// It searches from where the scan is rather than from the start of the table,
// so a caller inserting a run of records does not read past the ones it has
// just written for each of them. A caller that wants the earliest free slot,
// and so wants the space of deleted records back, asks to be moved before the
// first record first.
//
// Where MoveToNextRecord stops at the end of the table, this adds a block: a
// record has to go somewhere, and having run out of slots is what a table
// growing looks like.
func (ts *TableScan) MoveToNewRecord() error {
	if err := ts.requireCurrentBlock(); err != nil {
		return err
	}

	for {
		slot, err := ts.rp.ClaimFreeSlotAfter(ts.currentSlot)
		if err == nil {
			ts.currentSlot = slot
			return nil
		}
		if !errors.Is(err, recordmanager.ErrNoSuchSlot) {
			return err
		}

		onLastBlock, err := ts.isOnLastBlock()
		if err != nil {
			return err
		}
		if onLastBlock {
			if err := ts.moveToNewBlock(); err != nil {
				return err
			}

			continue
		}

		if err := ts.moveToBlock(ts.rp.BlockID().Number() + 1); err != nil {
			return err
		}
	}
}

// DeleteCurrentRecord takes the record the scan is on out of the table. The
// scan stays where it is, on a slot that now holds no record, so a walk carries
// on from there and never returns the record again.
func (ts *TableScan) DeleteCurrentRecord() error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.Delete(ts.currentSlot)
}

// CurrentRecordID names the record the scan is on, so that a caller can come
// back to it later without keeping the scan where it is.
func (ts *TableScan) CurrentRecordID() (*recordmanager.RecordID, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return nil, err
	}

	return recordmanager.NewRecordID(ts.rp.BlockID().Number(), ts.currentSlot), nil
}

// MoveToRecordID puts the scan straight onto the record rid names, without
// reading the ones before it. That is what makes an index worth having: it
// gives out record ids, and this is how a table is read from one.
//
// The scan lands on the record rather than before it, unlike the other moves,
// because rid says which record is wanted rather than where to start.
//
// A slot that does not fit the block is refused here rather than at the first
// read. A record id carries no file name, so one belonging to another table
// cannot be told apart by its type, and a table whose records are a different
// size is exactly where the number comes out wrong.
func (ts *TableScan) MoveToRecordID(rid *recordmanager.RecordID) error {
	if err := ts.moveToBlock(rid.BlockNumber()); err != nil {
		return err
	}

	if !ts.rp.HasSlot(rid.Slot()) {
		return fmt.Errorf("move to %s of %s: %w", rid, ts.fileName, recordmanager.ErrSlotOutOfRange)
	}
	ts.currentSlot = rid.Slot()

	return nil
}

// isOnLastBlock reports whether the scan is on the final block of the table,
// which is where a walk that finds no more records has to stop rather than move
// on.
//
// The count it keeps is enough to say no: a block it already knows comes after
// this one is proof there is more to read. Saying yes is what the count cannot
// be trusted for, since it may have been left behind by an append this
// transaction made elsewhere, so that answer is settled against the file and
// the count brought up to date.
func (ts *TableScan) isOnLastBlock() (bool, error) {
	if err := ts.requireCurrentBlock(); err != nil {
		return false, err
	}

	if ts.rp.BlockID().Number() < ts.blockCount-1 {
		return false, nil
	}

	size, err := ts.tx.Size(ts.fileName)
	if err != nil {
		return false, err
	}
	ts.blockCount = size

	return ts.rp.BlockID().Number() == size-1, nil
}

// requireCurrentBlock reports that the scan is on no block, which is the state
// a failed move leaves it in.
func (ts *TableScan) requireCurrentBlock() error {
	if ts.rp == nil {
		return fmt.Errorf("the scan of %s is on no block: %w", ts.fileName, ErrNoCurrentRecord)
	}

	return nil
}

// requireCurrentRecord reports why the scan has no record to read or write, and
// nil when it has one. The four field methods all need the same thing of it, so
// they ask here rather than each deciding what counts as being on a record.
func (ts *TableScan) requireCurrentRecord() error {
	if err := ts.requireCurrentBlock(); err != nil {
		return err
	}

	if ts.currentSlot == beforeFirstSlot {
		return fmt.Errorf("the scan of %s is before its first record: %w", ts.fileName, ErrNoCurrentRecord)
	}

	return nil
}

// GetInt returns the int field fieldName of the record the scan is on.
func (ts *TableScan) GetInt(fieldName string) (int32, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return 0, err
	}

	return ts.rp.GetInt(ts.currentSlot, fieldName)
}

// SetInt writes val to the int field fieldName of the record the scan is on.
func (ts *TableScan) SetInt(fieldName string, val int32) error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.SetInt(ts.currentSlot, fieldName, val)
}

// GetString returns the varchar field fieldName of the record the scan is on.
func (ts *TableScan) GetString(fieldName string) (string, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return "", err
	}

	return ts.rp.GetString(ts.currentSlot, fieldName)
}

// SetString writes val to the varchar field fieldName of the record the scan is
// on.
func (ts *TableScan) SetString(fieldName string, val string) error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.SetString(ts.currentSlot, fieldName, val)
}

// moveToBlock puts the scan on a block the table already has, before its first
// slot.
func (ts *TableScan) moveToBlock(blkNum int) error {
	ts.Close()

	rp, err := recordmanager.NewRecordPage(ts.tx, ts.blockID(blkNum), ts.layout)
	if err != nil {
		return err
	}

	ts.rp = rp
	ts.currentSlot = beforeFirstSlot

	return nil
}

// moveToNewBlock adds a block to the end of the table's file and puts the scan
// on it, before its first slot.
//
// The new block is initialized, which the one moveToBlock reaches is not. A
// block that has just been appended has never been read as slots, so nothing
// has yet written the flags that say which of them are free.
func (ts *TableScan) moveToNewBlock() error {
	ts.Close()

	blk, err := ts.tx.Append(ts.fileName)
	if err != nil {
		return err
	}

	rp, err := recordmanager.NewRecordPage(ts.tx, blk, ts.layout)
	if err != nil {
		return err
	}
	if err := rp.InitializeNewBlock(); err != nil {
		return err
	}

	ts.rp = rp
	ts.currentSlot = beforeFirstSlot
	ts.blockCount = blk.Number() + 1

	return nil
}

// blockID is the block the scan is on. It is here rather than inline so that
// the file name and the block number are put together in one place.
func (ts *TableScan) blockID(blkNum int) *filemanager.BlockId {
	return filemanager.NewBlockId(ts.fileName, blkNum)
}
