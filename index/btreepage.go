package index

import (
	"fmt"

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
	if err := p.makeRoom(slot); err != nil {
		return err
	}

	if err := p.setValue(slot, IndexedValueField, value); err != nil {
		return err
	}
	if err := p.setValue(slot, BlockNumberField, query.NewIntConstant(int32(rid.BlockNumber()))); err != nil {
		return err
	}

	return p.setValue(slot, SlotField, query.NewIntConstant(int32(rid.Slot())))
}

// dataValue is the value the record at slot is filed under.
func (p *btreePage) dataValue(slot int) (query.Constant, error) {
	return p.value(slot, IndexedValueField)
}

// dataRecordID is the record id of the indexed table that the leaf record at
// slot points to.
func (p *btreePage) dataRecordID(slot int) (*recordmanager.RecordID, error) {
	blkNum, err := p.value(slot, BlockNumberField)
	if err != nil {
		return nil, err
	}
	recordSlot, err := p.value(slot, SlotField)
	if err != nil {
		return nil, err
	}

	// AsInt cannot fail on either: both fields are ints in every layout an
	// index is opened with.
	n, _ := blkNum.AsInt()
	s, _ := recordSlot.AsInt()

	return recordmanager.NewRecordID(int(n), int(s)), nil
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
		if err := p.copyRecord(i-1, i); err != nil {
			return err
		}
	}

	return p.setNumRecords(n + 1)
}

// copyRecord writes every field of the record at from over the record at to.
func (p *btreePage) copyRecord(from, to int) error {
	for _, fieldName := range p.layout.Schema().Fields() {
		v, err := p.value(from, fieldName)
		if err != nil {
			return err
		}
		if err := p.setValue(to, fieldName, v); err != nil {
			return err
		}
	}

	return nil
}

// headerBytes is how much of the block the header takes, which is where the
// first slot begins.
const headerBytes = numRecordsOffset + filemanager.IntBytes

// fieldPos is where fieldName of the record at slot sits within the block.
//
// Every slot is the same size, so a slot is found by its number alone. The
// layout leaves room at the front of each slot for the in-use flag a record
// page keeps there; a B-tree page has no use for it, since numRecords says
// which slots hold a record, and leaves those bytes alone.
func (p *btreePage) fieldPos(slot int, fieldName string) (int, error) {
	offset, err := p.layout.Offset(fieldName)
	if err != nil {
		return 0, err
	}

	return headerBytes + slot*p.layout.SlotSize() + offset, nil
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
