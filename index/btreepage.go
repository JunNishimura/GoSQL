package index

import (
	"fmt"
	"unicode/utf8"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Offsets of the two header ints of a B-tree page within its block.
const (
	flagOffset       = 0
	numRecordsOffset = flagOffset + filemanager.IntBytes
)

// btreePage is one block of a B-tree: a header followed by records sorted by
// key.
//
// The header is two ints. The first is a flag whose meaning the caller
// defines: a directory block stores its level there, and a leaf stores the
// number of its overflow block. The second is the number of records in the
// block. Records have no in-use flag of their own, so this count marks the
// end of the records: the first numRecords slots are in use, and no others.
//
// Leaves and directory blocks both use this type. They differ only in the
// layout the page is opened with, which defines the fields of a record.
type btreePage struct {
	tx     *transaction.Transaction
	blk    *filemanager.BlockId
	layout *recordmanager.Layout
}

// appendBTreeBlock appends a block to fileName and writes the header of an
// empty page to it, with the flag set to flag.
//
// Only the header is written. The slots after it are never read before a
// record is written to them, because numRecords marks them as unused.
func appendBTreeBlock(
	tx *transaction.Transaction,
	fileName string,
	layout *recordmanager.Layout,
	flag int32,
) (*filemanager.BlockId, error) {
	blk, err := tx.Append(fileName)
	if err != nil {
		return nil, fmt.Errorf("append a block to B-tree file %s: %w", fileName, err)
	}

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		return nil, err
	}
	defer page.close()

	if err := page.setFlag(flag); err != nil {
		return nil, err
	}
	if err := page.setNumRecords(0); err != nil {
		return nil, err
	}

	return blk, nil
}

// newBTreePage pins blk for tx and reads it as a B-tree page whose records
// use layout. The block stays pinned until close.
func newBTreePage(
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
) (*btreePage, error) {
	if err := tx.Pin(blk); err != nil {
		return nil, fmt.Errorf("pin %s to read it as a B-tree page: %w", blk, err)
	}

	return &btreePage{
		tx:     tx,
		blk:    blk,
		layout: layout,
	}, nil
}

// flag returns the header int whose meaning the caller defines.
func (p *btreePage) flag() (int32, error) {
	return p.tx.GetInt(p.blk, flagOffset)
}

// setFlag writes flag to the header.
func (p *btreePage) setFlag(flag int32) error {
	return p.tx.SetInt(p.blk, flagOffset, flag)
}

// numRecords returns the number of records in the block.
func (p *btreePage) numRecords() (int, error) {
	n, err := p.tx.GetInt(p.blk, numRecordsOffset)
	if err != nil {
		return 0, err
	}

	return int(n), nil
}

// setNumRecords writes n to the header as the number of records in the
// block.
func (p *btreePage) setNumRecords(n int) error {
	return p.tx.SetInt(p.blk, numRecordsOffset, int32(n))
}

// insertLeaf writes a leaf entry that maps value to rid at slot. It first
// shifts the records from slot onward by one slot to make room.
//
// The caller chooses the slot, using findSlotBefore. The page stays sorted
// only because every insert goes to the slot that preserves the order.
func (p *btreePage) insertLeaf(slot int, value query.Constant, rid *recordmanager.RecordID) error {
	if err := p.checkValue(value); err != nil {
		return err
	}
	if err := p.makeRoom(slot); err != nil {
		return err
	}

	if err := p.setValue(slot, IndexedValueField, value); err != nil {
		return err
	}
	if err := p.setIntValue(slot, BlockNumberField, rid.BlockNumber()); err != nil {
		return err
	}

	return p.setIntValue(slot, SlotField, rid.Slot())
}

// dataValue returns the key of the record at slot.
func (p *btreePage) dataValue(slot int) (query.Constant, error) {
	return p.value(slot, IndexedValueField)
}

// dataRecordID returns the record id of the table record that the leaf entry
// at slot points to.
func (p *btreePage) dataRecordID(slot int) (*recordmanager.RecordID, error) {
	blkNum, err := p.intValue(slot, BlockNumberField)
	if err != nil {
		return nil, err
	}
	recordSlot, err := p.intValue(slot, SlotField)
	if err != nil {
		return nil, err
	}

	return recordmanager.NewRecordID(blkNum, recordSlot), nil
}

// insertDir writes a directory entry at slot. The entry states that the child
// block numbered childBlockNumber covers the keys from value upward. The
// records from slot onward are first shifted by one slot to make room.
func (p *btreePage) insertDir(slot int, value query.Constant, childBlockNumber int) error {
	if err := p.checkValue(value); err != nil {
		return err
	}
	if err := p.makeRoom(slot); err != nil {
		return err
	}

	if err := p.setValue(slot, IndexedValueField, value); err != nil {
		return err
	}

	return p.setIntValue(slot, BlockNumberField, childBlockNumber)
}

// childBlockNumber returns the child block number of the directory entry at
// slot.
func (p *btreePage) childBlockNumber(slot int) (int, error) {
	return p.intValue(slot, BlockNumberField)
}

// findSlotBefore returns the last slot whose key is less than searchKey, or
// -1 if there is none.
//
// This is the slot before the position where the records for searchKey
// start, or would start if there were any. A search moves forward from it to
// the first of these records. An insert uses the slot after it, which places
// the new record before any existing record with the same key. A record whose
// key equals searchKey does not count as "before" it. With several records
// for searchKey, the result is therefore the slot before the first of them.
//
// If searchKey has a different type from the page's keys, findSlotBefore
// returns ErrConstantTypeMismatch. Values of different types cannot be
// ordered.
func (p *btreePage) findSlotBefore(searchKey query.Constant) (int, error) {
	n, err := p.numRecords()
	if err != nil {
		return 0, err
	}

	for slot := range n {
		v, err := p.dataValue(slot)
		if err != nil {
			return 0, err
		}

		c, err := v.Compare(searchKey)
		if err != nil {
			return 0, fmt.Errorf("find the slot before %s in %s: %w", searchKey, p.blk, err)
		}
		if c >= 0 {
			return slot - 1, nil
		}
	}

	return n - 1, nil
}

// isFull reports whether the block has no room for another record.
//
// A record is inserted before the page is checked. A page that reports full
// has therefore just received its last record, and the caller splits it
// immediately. This is why an insert does not need to check for room first:
// any page that became full was split at that time, so a page always has room
// for one more record.
func (p *btreePage) isFull() (bool, error) {
	n, err := p.numRecords()
	if err != nil {
		return false, err
	}

	// The next record would use slot n. That slot ends where slot n+1 would
	// begin.
	return p.slotPos(n+1) > p.tx.BlockSize(), nil
}

// split moves the records from splitPos onward to a new block appended to the
// same file, sets the flag of that block to flag, and returns the block.
//
// The records keep their order. The records before splitPos stay in place,
// and the moved records fill the new block from slot 0. Every key that stays
// is then less than or equal to every key that moved. The caller can
// therefore add the new block to the directory under its first key.
func (p *btreePage) split(splitPos int, flag int32) (*filemanager.BlockId, error) {
	newBlk, err := appendBTreeBlock(p.tx, p.blk.FileName(), p.layout, flag)
	if err != nil {
		return nil, err
	}

	newPage, err := newBTreePage(p.tx, newBlk, p.layout)
	if err != nil {
		return nil, err
	}
	defer newPage.close()

	n, err := p.numRecords()
	if err != nil {
		return nil, err
	}

	for slot := splitPos; slot < n; slot++ {
		if err := p.copyRecordTo(slot, newPage, slot-splitPos); err != nil {
			return nil, err
		}
	}

	// The new block's count is set only after all records have been copied
	// to it, and this block's count is reduced after that. Neither block
	// ever counts a slot that does not hold one of its records.
	if err := newPage.setNumRecords(n - splitPos); err != nil {
		return nil, err
	}
	if err := p.setNumRecords(splitPos); err != nil {
		return nil, err
	}

	return newBlk, nil
}

// delete removes the record at slot. It shifts the records after slot toward
// the front by one slot, so the remaining records still occupy the first
// slots in order.
//
// The shift starts with the record after slot and proceeds toward the end.
// Each record is therefore copied into a slot whose own record has already
// been moved or deleted. The last slot keeps a stale copy of its record.
// Nothing reads that copy, because the slot is no longer counted.
func (p *btreePage) delete(slot int) error {
	n, err := p.numRecords()
	if err != nil {
		return err
	}

	for i := slot + 1; i < n; i++ {
		if err := p.copyRecordTo(i, p, i-1); err != nil {
			return err
		}
	}

	return p.setNumRecords(n - 1)
}

// checkValue checks that value fits the key field of the page's records. It
// returns ErrFieldTypeMismatch if value has a different type from the field.
// It returns ErrStringTooLong if value is a varchar with more characters than
// the field's declared length.
//
// No lower layer catches either error. A varchar written to an int field is
// stored as its length followed by its bytes, and it reads back as the
// length. The page would then silently index the entry by a number that
// nobody inserted. A varchar over the length limit is written in full,
// because the field reserves room for its limit in the widest encoding. If it
// is slightly too long, it reads back intact. If it is much too long, it
// overwrites the next slot. Both cases violate the length the schema
// declares, and a record page rejects them for the same reason.
//
// The check runs before anything is written, not while the value is written.
// An insert shifts the records after slot and increments the count before it
// writes the new record. An error at that point would leave a gap that is
// counted as a record and holds a stale copy of the record shifted out of it.
func (p *btreePage) checkValue(value query.Constant) error {
	schema := p.layout.Schema()

	fieldType, err := schema.Type(IndexedValueField)
	if err != nil {
		return err
	}
	if value.Type() != fieldType {
		return fmt.Errorf(
			"file an entry under %s in %s, whose records are filed under a %s: %w",
			value, p.blk, fieldType, recordmanager.ErrFieldTypeMismatch,
		)
	}

	if fieldType != recordmanager.FieldTypeVarchar {
		return nil
	}

	// Neither call can fail here: Type has just found the field, and value is
	// known to be a varchar.
	length, err := schema.Length(IndexedValueField)
	if err != nil {
		return err
	}
	s, _ := value.AsString()

	// The length is counted in characters, not bytes, because a varchar's
	// declared length is a character count. A record page counts the same way.
	if count := utf8.RuneCountInString(s); count > length {
		return fmt.Errorf(
			"file an entry under %s of %d characters in %s, whose records hold %d: %w",
			value, count, p.blk, length, recordmanager.ErrStringTooLong,
		)
	}

	return nil
}

// makeRoom shifts the records from slot to the last one toward the end by one
// slot, so that slot is free to be written. It also increments the record
// count for the record about to be written.
//
// The shift starts with the last record and proceeds toward slot. Each record
// is therefore copied into a slot whose own record has already been moved.
func (p *btreePage) makeRoom(slot int) error {
	n, err := p.numRecords()
	if err != nil {
		return err
	}

	for i := n; i > slot; i-- {
		if err := p.copyRecordTo(i-1, p, i); err != nil {
			return err
		}
	}

	return p.setNumRecords(n + 1)
}

// copyRecordTo copies every field of this page's record at slot from into
// dst's record at slot to. dst must use the same layout as this page. dst can
// be this page itself, which is how records are shifted within one block.
func (p *btreePage) copyRecordTo(from int, dst *btreePage, to int) error {
	for _, fieldName := range p.layout.Schema().Fields() {
		v, err := p.value(from, fieldName)
		if err != nil {
			return err
		}
		if err := dst.setValue(to, fieldName, v); err != nil {
			return err
		}
	}

	return nil
}

// headerBytes is the size of the header in bytes. The first slot begins at
// this offset.
const headerBytes = numRecordsOffset + filemanager.IntBytes

// slotPos returns the offset of slot within the block. All slots have the
// same size, so the offset depends only on the slot number.
func (p *btreePage) slotPos(slot int) int {
	return headerBytes + slot*p.layout.SlotSize()
}

// fieldPos returns the offset of fieldName of the record at slot within the
// block.
//
// The layout reserves space at the start of each slot for the in-use flag of
// a record page. A B-tree page does not use that flag, because numRecords
// already determines which slots hold a record. It leaves those bytes
// untouched.
func (p *btreePage) fieldPos(slot int, fieldName string) (int, error) {
	offset, err := p.layout.Offset(fieldName)
	if err != nil {
		return 0, err
	}

	return p.slotPos(slot) + offset, nil
}

// value reads fieldName of the record at slot as a constant of the type that
// the layout declares for the field.
func (p *btreePage) value(slot int, fieldName string) (query.Constant, error) {
	pos, err := p.fieldPos(slot, fieldName)
	if err != nil {
		return query.Constant{}, err
	}
	fieldType, err := p.layout.Schema().Type(fieldName)
	if err != nil {
		return query.Constant{}, err
	}

	if fieldType == recordmanager.FieldTypeInt {
		n, err := p.tx.GetInt(p.blk, pos)
		if err != nil {
			return query.Constant{}, err
		}

		return query.NewIntConstant(n), nil
	}

	s, err := p.tx.GetString(p.blk, pos)
	if err != nil {
		return query.Constant{}, err
	}

	return query.NewStringConstant(s), nil
}

// intValue reads fieldName of the record at slot as an int.
//
// It is used for the fields that every index declares as ints: the block
// numbers and the slot. These are used as positions, not compared as keys.
// Reading them through value would wrap the int in a constant only to unwrap
// it again.
func (p *btreePage) intValue(slot int, fieldName string) (int, error) {
	pos, err := p.fieldPos(slot, fieldName)
	if err != nil {
		return 0, err
	}

	n, err := p.tx.GetInt(p.blk, pos)
	if err != nil {
		return 0, err
	}

	return int(n), nil
}

// setIntValue writes n to the int field fieldName of the record at slot.
func (p *btreePage) setIntValue(slot int, fieldName string, n int) error {
	pos, err := p.fieldPos(slot, fieldName)
	if err != nil {
		return err
	}

	return p.tx.SetInt(p.blk, pos, int32(n))
}

// setValue writes v to fieldName of the record at slot.
func (p *btreePage) setValue(slot int, fieldName string, v query.Constant) error {
	pos, err := p.fieldPos(slot, fieldName)
	if err != nil {
		return err
	}

	if v.Type() == recordmanager.FieldTypeInt {
		// AsInt cannot fail here: the type has just been checked.
		n, _ := v.AsInt()

		return p.tx.SetInt(p.blk, pos, n)
	}

	// AsString cannot fail either: a constant is either an int or a string.
	s, _ := v.AsString()

	return p.tx.SetString(p.blk, pos, s)
}

// close unpins the block that was pinned when the page was opened.
func (p *btreePage) close() {
	p.tx.Unpin(p.blk)
}
