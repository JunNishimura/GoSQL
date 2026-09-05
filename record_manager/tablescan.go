package recordmanager

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNoCurrentRecord reports reading or writing a scan that is not on a record:
// one that has not been moved to its first record yet, or one that has run past
// the last.
//
// It is kept apart from ErrSlotOutOfRange, which the record page raises for the
// same slot number, because the two say different things to whoever gets them.
// Out of range means a slot that could not exist; this one means the scan has
// not been asked to go anywhere.
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
	layout *Layout
	// rp is the block the scan is on, or nil before it has moved to one.
	rp *RecordPage
	// fileName is the table's file, kept rather than the table's name because
	// nothing below this asks for a table.
	fileName string
	// currentSlot is the slot of rp the scan is on, or beforeFirstSlot.
	currentSlot int
}

// NewTableScan opens a scan over tableName, placed before the first record.
//
// A table whose file has no blocks yet gets one, so that a scan always has a
// block to work on and whoever inserts into it does not have to treat an empty
// table as a special case.
func NewTableScan(tx *transaction.Transaction, tableName string, layout *Layout) (*TableScan, error) {
	ts := &TableScan{
		tx:       tx,
		layout:   layout,
		fileName: tableName + tableFileExtension,
	}

	size, err := tx.Size(ts.fileName)
	if err != nil {
		return nil, err
	}

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

// requireCurrentRecord reports why the scan has no record to read or write, and
// nil when it has one. The four field methods all need the same thing of it, so
// they ask here rather than each deciding what counts as being on a record.
func (ts *TableScan) requireCurrentRecord() error {
	if ts.rp == nil {
		return fmt.Errorf("the scan of %s is on no block: %w", ts.fileName, ErrNoCurrentRecord)
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
	ts.releaseCurrentBlock()

	rp, err := NewRecordPage(ts.tx, ts.blockID(blkNum), ts.layout)
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
	ts.releaseCurrentBlock()

	blk, err := ts.tx.Append(ts.fileName)
	if err != nil {
		return err
	}

	rp, err := NewRecordPage(ts.tx, blk, ts.layout)
	if err != nil {
		return err
	}
	if err := rp.InitializeNewBlock(); err != nil {
		return err
	}

	ts.rp = rp
	ts.currentSlot = beforeFirstSlot

	return nil
}

// releaseCurrentBlock gives back the pin on the block the scan is on, if it is
// on one. A scan that has moved off a block has no further use for it, and
// holding the pin would keep a buffer for a block nothing is reading.
func (ts *TableScan) releaseCurrentBlock() {
	if ts.rp == nil {
		return
	}

	ts.tx.Unpin(ts.rp.blk)
	ts.rp = nil
}

// blockID is the block the scan is on. It is here rather than inline so that
// the file name and the block number are put together in one place.
func (ts *TableScan) blockID(blkNum int) *filemanager.BlockId {
	return filemanager.NewBlockId(ts.fileName, blkNum)
}
