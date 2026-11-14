package query

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNoCurrentRecord is returned when a scan is read or written while it is
// not on a record: before the first move to a record, or after the last
// record.
//
// It is separate from recordmanager.ErrSlotOutOfRange, which a record page
// returns for the same slot number, because the two errors mean different
// things to the caller. Out of range means that the slot cannot exist. This
// error means that the scan has not been moved to a record.
var ErrNoCurrentRecord = errors.New("no current record")

// tableFileExtension is appended to a table name to form its file name. One
// table is one file, so the table name is enough to find the file.
const tableFileExtension = ".tbl"

// TableFileName returns the name of the file that stores the records of
// tableName.
//
// It is exported for scans that access the blocks of a table by number and
// not through a TableScan, such as a scan that pins several blocks at once.
// They then use the same file as a TableScan.
func TableFileName(tableName string) string {
	return tableName + tableFileExtension
}

// beforeFirstSlot is the slot position of a scan that has moved to a block
// but has not read any slot yet.
//
// It is one less than the first slot, because the searches of a record page
// start after the slot they are given. A search from this value returns
// slot 0.
const beforeFirstSlot = -1

// A table scan is the scan that every other scan is built on, so it must
// satisfy UpdateScan. This variable is never read. It makes a method whose
// name or signature no longer matches the interface a compile error in this
// file, and not at the first caller that uses the type as an UpdateScan.
var _ UpdateScan = (*TableScan)(nil)

// TableScan scans every record of one table, block by block.
//
// A record page covers one block, and a table is a file of blocks. A table
// scan moves a reader from the end of one block to the start of the next. It
// also appends a block when a record must be inserted and no existing block
// has room.
//
// It keeps one block pinned at a time: the current block. Moving to another
// block unpins the previous one, so a scan of a large table does not exhaust
// the buffer pool.
type TableScan struct {
	tx     *transaction.Transaction
	layout *recordmanager.Layout
	// rp is the record page of the current block, or nil before the first move.
	rp *recordmanager.RecordPage
	// fileName is the file of the table. The scan keeps it instead of the
	// table name, because the lower layers only use file names.
	fileName string
	// currentSlot is the current slot in rp, or beforeFirstSlot.
	currentSlot int
	// blockCount is the number of blocks that the file had when the scan last
	// checked. Reading the file size is a stat call, and a scan needs the
	// size once for each block it leaves, so the result is cached.
	//
	// The cached count can be too low but never too high, because blocks are
	// appended and never removed. No other transaction can append a block,
	// because the scan took a shared lock on the file length when it was
	// opened. This transaction can, through this scan or through another scan
	// of the same table.
	blockCount int
}

// NewTableScan opens a scan of tableName, positioned before the first record.
//
// If the file of the table has no blocks, one is appended. A scan then always
// has a block to work on, and code that inserts records needs no special case
// for an empty table.
func NewTableScan(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) (*TableScan, error) {
	ts := &TableScan{
		tx:       tx,
		layout:   layout,
		fileName: TableFileName(tableName),
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

// Close unpins the current block. Every scan must be closed. Otherwise its
// buffer stays pinned until the transaction ends, long after the scan was
// last used.
//
// Close returns no error because it cannot fail: an unpin only decrements a
// count. It can be called on a scan that is already closed. A caller can
// therefore defer it and also close early where it needs the buffer sooner.
//
// A closed scan is on no block. Reading or writing it returns
// ErrNoCurrentRecord and does not access the block that the buffer holds
// later.
//
// A move to another block starts with Close, for the same reason that a
// caller closes a scan: the previous block is no longer read.
func (ts *TableScan) Close() {
	if ts.rp == nil {
		return
	}

	ts.tx.Unpin(ts.rp.BlockID())
	ts.rp = nil
}

// MoveBeforeFirstRecord positions the scan at the start of the table, before
// its first record.
//
// The scan is then on no record, not on the first one. A field read directly
// after this call returns ErrNoCurrentRecord, and MoveToNextRecord moves to
// the first record. A scan of a table can therefore be a single loop that
// reaches the first record in the same way as every other record.
//
// The table is not changed. The first block is opened with its existing
// records, so a query can call this as often as it needs to read the table
// again.
func (ts *TableScan) MoveBeforeFirstRecord() error {
	return ts.moveToBlock(0)
}

// MoveToNextRecord moves the scan to the record after the current one, and
// reports whether one exists. False means that the scan has reached the end
// of the table. It is not an error.
//
// Empty slots are skipped, and so are blocks with only empty slots. A table
// with deleted records is read as the records it still holds, and the caller
// sees no gaps.
//
// At the end of a block the scan continues with the next block and does not
// stop. A table therefore appears as one sequence of records, not as a file
// of blocks. Only the current block is pinned, so a scan of a large table
// uses one buffer however long it runs.
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

		// The block holds no more records. The block itself does not show
		// whether this is the end of the table or only the end of a block.
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

// MoveToNewRecord moves the scan to a new record in the first free slot at or
// after its current position. The fields keep the values of an empty slot,
// and the caller is expected to set them next.
//
// The search starts at the current position, not at the start of the table.
// A caller that inserts many records therefore does not read past the records
// it has just written for each insert. A caller that wants the earliest free
// slot, to reuse the space of deleted records, first calls
// MoveBeforeFirstRecord.
//
// MoveToNextRecord stops at the end of the table. MoveToNewRecord appends a
// block there, because the record must be stored somewhere. A table grows
// when it has no free slot left.
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

// DeleteCurrentRecord deletes the current record from the table. The scan
// stays on the same slot, which is now empty. A scan that continues from
// there does not return the record again.
func (ts *TableScan) DeleteCurrentRecord() error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.Delete(ts.currentSlot)
}

// CurrentRecordID returns the record id of the current record, so that a
// caller can return to the record later without keeping the scan on it.
func (ts *TableScan) CurrentRecordID() (*recordmanager.RecordID, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return nil, err
	}

	return recordmanager.NewRecordID(ts.rp.BlockID().Number(), ts.currentSlot), nil
}

// MoveToRecordID moves the scan directly to the record that rid identifies,
// without reading the records before it. An index depends on this: it returns
// record ids, and this method reads the table records for them.
//
// Unlike the other moves, this one positions the scan on the record and not
// before it, because rid identifies the wanted record and not a starting
// point.
//
// A slot that is outside the block is rejected here, not at the first read. A
// record id has no file name, so its type does not show that it belongs to
// another table. A table with a different record size is exactly the case in
// which the slot number is wrong.
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

// isOnLastBlock reports whether the scan is on the last block of the table. A
// scan that finds no more records stops there and does not move on.
//
// The cached count is enough to answer no: if the count shows a block after
// the current one, there is more to read. The cached count is not enough to
// answer yes, because an append by this transaction through another scan can
// have made it stale. For that answer the method reads the file size and
// updates the count.
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

// requireCurrentBlock returns an error if the scan is on no block. A failed
// move leaves the scan in that state.
func (ts *TableScan) requireCurrentBlock() error {
	if ts.rp == nil {
		return fmt.Errorf("the scan of %s is on no block: %w", ts.fileName, ErrNoCurrentRecord)
	}

	return nil
}

// requireCurrentRecord returns an error that explains why the scan has no
// current record, or nil if it has one. The four field methods all call it,
// so that the definition of "on a record" is in one place.
func (ts *TableScan) requireCurrentRecord() error {
	if err := ts.requireCurrentBlock(); err != nil {
		return err
	}

	if ts.currentSlot == beforeFirstSlot {
		return fmt.Errorf("the scan of %s is before its first record: %w", ts.fileName, ErrNoCurrentRecord)
	}

	return nil
}

// GetInt returns the int field fieldName of the current record.
func (ts *TableScan) GetInt(fieldName string) (int32, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return 0, err
	}

	return ts.rp.GetInt(ts.currentSlot, fieldName)
}

// SetInt writes val to the int field fieldName of the current record.
func (ts *TableScan) SetInt(fieldName string, val int32) error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.SetInt(ts.currentSlot, fieldName, val)
}

// GetString returns the varchar field fieldName of the current record.
func (ts *TableScan) GetString(fieldName string) (string, error) {
	if err := ts.requireCurrentRecord(); err != nil {
		return "", err
	}

	return ts.rp.GetString(ts.currentSlot, fieldName)
}

// SetString writes val to the varchar field fieldName of the current record.
func (ts *TableScan) SetString(fieldName string, val string) error {
	if err := ts.requireCurrentRecord(); err != nil {
		return err
	}

	return ts.rp.SetString(ts.currentSlot, fieldName, val)
}

// HasField reports whether the table has a field named fieldName.
//
// The answer comes from the schema, not from a record, so the method works on
// a scan that is on no record. A query decides which of its scans a field
// belongs to before it reads any record, and it calls HasField for that.
func (ts *TableScan) HasField(fieldName string) bool {
	return ts.layout.Schema().HasField(fieldName)
}

// GetValue returns the field fieldName of the current record as a constant of
// the type that the schema declares for the field.
//
// The type must come from the schema, because the bytes of a slot can be read
// as either type. A varchar "42" and an int 42 differ only in the type that
// the table declares. A scan that derived the type from the bytes would
// return the wrong type for one of them.
func (ts *TableScan) GetValue(fieldName string) (Constant, error) {
	fieldType, err := ts.layout.Schema().Type(fieldName)
	if err != nil {
		return Constant{}, err
	}

	switch fieldType {
	case recordmanager.FieldTypeInt:
		val, err := ts.GetInt(fieldName)
		if err != nil {
			return Constant{}, err
		}

		return NewIntConstant(val), nil
	case recordmanager.FieldTypeVarchar:
		val, err := ts.GetString(fieldName)
		if err != nil {
			return Constant{}, err
		}

		return NewStringConstant(val), nil
	default:
		return Constant{}, fmt.Errorf("read field %q of %s: a %s has no value to read", fieldName, ts.fileName, fieldType)
	}
}

// SetValue writes val to the field fieldName of the current record, according
// to the type of the constant.
//
// The type comes from the constant and not from the schema, so SetValue does
// not query the schema. The record page already checks the field against the
// type that is written. A constant of the wrong type for the field causes the
// same ErrFieldTypeMismatch as a typed setter on the wrong field. This gives
// one error to document instead of two, and saves one schema lookup.
func (ts *TableScan) SetValue(fieldName string, val Constant) error {
	switch val.Type() {
	case recordmanager.FieldTypeInt:
		// AsInt cannot fail here, because the switch has just checked the type.
		// The error is still returned, so that the code stays correct if AsInt
		// gains another failure case.
		intVal, err := val.AsInt()
		if err != nil {
			return err
		}

		return ts.SetInt(fieldName, intVal)
	case recordmanager.FieldTypeVarchar:
		strVal, err := val.AsString()
		if err != nil {
			return err
		}

		return ts.SetString(fieldName, strVal)
	default:
		return fmt.Errorf("write %s to field %q of %s: a %s is not a value a record holds", val, fieldName, ts.fileName, val.Type())
	}
}

// moveToBlock moves the scan to an existing block of the table, before its
// first slot.
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

// moveToNewBlock appends a block to the file of the table and moves the scan
// to it, before its first slot.
//
// The new block is initialized, unlike a block that moveToBlock opens. A
// block that was just appended has never been used as slots, so the flags
// that mark its slots as free have not been written yet.
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

// blockID returns the id of block blkNum in the file of the table. It is a
// separate method so that the file name and the block number are combined in
// one place.
func (ts *TableScan) blockID(blkNum int) *filemanager.BlockId {
	return filemanager.NewBlockId(ts.fileName, blkNum)
}
