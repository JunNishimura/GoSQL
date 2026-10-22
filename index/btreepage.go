package index

import (
	"fmt"
	"unicode/utf8"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Where the two ints of a B-tree page's header sit within its block.
const (
	flagOffset       = 0
	numRecordsOffset = flagOffset + filemanager.IntBytes
)

// btreePage is one block of a B-tree, seen as a header followed by records kept
// in order of the value they are filed under.
//
// The header is two ints. The first is a flag whose meaning is the caller's: a
// directory block keeps its level there, and a leaf the block its entries
// overflow into. The second is how many records the block holds, which is what
// says where the records end, since a record here has no flag of its own saying
// whether it is in use: the first numRecords slots are, and no others.
//
// Leaves and directory blocks are both this. What sets them apart is the layout
// a page is opened with, which says which fields a record has.
type btreePage struct {
	tx     *transaction.Transaction
	blk    *filemanager.BlockId
	layout *recordmanager.Layout
}

// appendBTreeBlock adds a block to the end of fileName and writes the header of
// an empty page there, flagged with flag.
//
// Only the header is written. The slots past it are never read before a record
// is put in them, since numRecords says none of them hold one.
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

// newBTreePage takes hold of blk on behalf of tx and reads it as a B-tree page
// of records laid out as layout says. The block stays pinned until close.
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

// flag is the int the header keeps for the caller.
func (p *btreePage) flag() (int32, error) {
	return p.tx.GetInt(p.blk, flagOffset)
}

// setFlag writes flag to the header.
func (p *btreePage) setFlag(flag int32) error {
	return p.tx.SetInt(p.blk, flagOffset, flag)
}

// numRecords is how many records the block holds.
func (p *btreePage) numRecords() (int, error) {
	n, err := p.tx.GetInt(p.blk, numRecordsOffset)
	if err != nil {
		return 0, err
	}

	return int(n), nil
}

// setNumRecords writes n to the header as the number of records the block
// holds.
func (p *btreePage) setNumRecords(n int) error {
	return p.tx.SetInt(p.blk, numRecordsOffset, int32(n))
}

// insertLeaf puts an entry filing rid under value at slot, moving the records
// from slot on back by one to make room.
//
// Where it goes is the caller's to work out, from findSlotBefore: the page
// keeps its records in order only because every insert is made at the place
// that keeps them so.
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

// dataValue is the value the record at slot is filed under.
func (p *btreePage) dataValue(slot int) (query.Constant, error) {
	return p.value(slot, IndexedValueField)
}

// dataRecordID is the record id of the indexed table that the leaf record at
// slot points to.
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

// insertDir puts a directory record at slot, saying that the entries from value
// on are reached through the child block numbered childBlockNumber. The records
// from slot on are moved back by one to make room.
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

// childBlockNumber is the block the directory record at slot points down to.
func (p *btreePage) childBlockNumber(slot int) (int, error) {
	return p.intValue(slot, BlockNumberField)
}

// findSlotBefore is the last slot whose record is filed under a value less
// than searchKey, or -1 if there is none.
//
// It is the slot before where the records for searchKey start, or would start
// if there were any: a search moves forward from it to the first of them, and
// an insert goes in the slot after it, ahead of any already filed under the
// same value. Records equal to searchKey are not "before" it, so with several
// filed under the same value, the answer is the slot ahead of the first.
//
// A searchKey of the other kind from the page's values reports
// ErrConstantTypeMismatch, since the two have no order to find a place by.
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
// A record is put in a page before the page is asked about, so a page found
// full has just taken its last record and is split there and then. That is
// what lets an insert go ahead without asking first: the page it lands in was
// split the last time it filled, so there is always room for one more.
func (p *btreePage) isFull() (bool, error) {
	n, err := p.numRecords()
	if err != nil {
		return false, err
	}

	// The next record would take the slot numbered n, which ends where slot
	// n+1 would begin.
	return p.slotPos(n+1) > p.tx.BlockSize(), nil
}

// split moves the records from splitPos on into a block appended to the same
// file, flagged with flag, and returns that block.
//
// The records keep their order: the ones before splitPos stay where they are,
// and the ones moved take up the new block from its first slot. Every value
// left here is then no greater than every value moved, which is what lets the
// caller file the new block in the directory under the first value it holds.
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

	// The moved records are counted in the new block only once they are all
	// in it, and the ones left here are counted afresh, so that neither block
	// counts a slot that does not yet, or no longer, hold one of its records.
	if err := newPage.setNumRecords(n - splitPos); err != nil {
		return nil, err
	}
	if err := p.setNumRecords(splitPos); err != nil {
		return nil, err
	}

	return newBlk, nil
}

// delete takes out the record at slot, moving the records after it forward by
// one so that the records left still take up the first slots in order.
//
// The records are moved from the one after slot onward, so that each is copied
// into a slot whose own record has already been moved out of it or deleted.
// The last slot is left holding the record just copied out of it, which nothing
// reads once it is no longer counted.
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

// checkValue reports whether value fits the field the page's records are filed
// under: ErrFieldTypeMismatch if it is of the other kind, and ErrStringTooLong
// if it is a varchar of more characters than the field was declared to hold.
//
// Nothing below would catch either. A varchar written over an int field is its
// length followed by its bytes, and reads back as the length: the page would
// file the entry under a number nobody inserted, and say nothing. A varchar
// over its limit is written whole, since the field has room for its limit in
// the widest encoding; a little over, it reads back intact, and far enough
// over, it runs into the next slot. Either way it breaks the width the schema
// declared, which a record page refuses for the same reason.
//
// It is checked before anything is written rather than as the value is,
// because an insert moves the records after slot and counts the new one before
// it writes it. Refused at that point, it would leave a gap holding whatever
// the record moved out of it held, counted as a record of its own.
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

	// Neither can fail here: Type has just found the field, and value has
	// just been found to be a varchar.
	length, err := schema.Length(IndexedValueField)
	if err != nil {
		return err
	}
	s, _ := value.AsString()

	// Characters are counted rather than bytes, as a record page counts them,
	// because that is what a varchar's length means.
	if count := utf8.RuneCountInString(s); count > length {
		return fmt.Errorf(
			"file an entry under %s of %d characters in %s, whose records hold %d: %w",
			value, count, p.blk, length, recordmanager.ErrStringTooLong,
		)
	}

	return nil
}

// makeRoom moves the records from slot to the last back by one slot, leaving
// slot free to be written, and counts the record about to be written there.
//
// The records are moved from the last one forward, so that each is copied into
// a slot whose own record has already been moved out of it.
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

// copyRecordTo writes every field of the record at from over the record at to
// in dst, which is laid out as this page is. dst may be this page itself, which
// is how records are moved along within one block.
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

// headerBytes is how much of the block the header takes, which is where the
// first slot begins.
const headerBytes = numRecordsOffset + filemanager.IntBytes

// slotPos is where slot begins within the block. Every slot is the same size,
// so a slot is found by its number alone.
func (p *btreePage) slotPos(slot int) int {
	return headerBytes + slot*p.layout.SlotSize()
}

// fieldPos is where fieldName of the record at slot sits within the block.
//
// The layout leaves room at the front of each slot for the in-use flag a record
// page keeps there; a B-tree page has no use for it, since numRecords says
// which slots hold a record, and leaves those bytes alone.
func (p *btreePage) fieldPos(slot int, fieldName string) (int, error) {
	offset, err := p.layout.Offset(fieldName)
	if err != nil {
		return 0, err
	}

	return p.slotPos(slot) + offset, nil
}

// value reads fieldName of the record at slot as whichever kind of value the
// layout says the field holds.
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
// It is for the fields every index lays out as ints, the block numbers and the
// slot, which are read as numbers to go somewhere rather than as values to
// compare. Reading them through value would make a constant only to take the
// int back out of it.
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

	// AsString cannot fail either, a constant being one of the two kinds.
	s, _ := v.AsString()

	return p.tx.SetString(p.blk, pos, s)
}

// close gives back the pin taken when the page was opened.
func (p *btreePage) close() {
	p.tx.Unpin(p.blk)
}
