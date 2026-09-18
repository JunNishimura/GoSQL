package recordmanager

import (
	"errors"
	"fmt"
	"unicode/utf8"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrSlotOutOfRange reports a slot number that does not fit in the block.
var ErrSlotOutOfRange = errors.New("slot out of range")

// ErrFieldTypeMismatch reports reading or writing a field as one type when the
// schema says it is another.
var ErrFieldTypeMismatch = errors.New("field type mismatch")

// ErrStringTooLong reports writing a string of more characters than the varchar
// field it is written to was declared to hold.
var ErrStringTooLong = errors.New("string too long")

// ErrSlotWiderThanBlock reports a layout whose slot does not fit in a block, so
// that no block can hold even one record of it.
var ErrSlotWiderThanBlock = errors.New("slot wider than block")

// ErrNoSuchSlot reports that a search reached the end of the block without
// finding a slot in the state it was looking for. It is the ordinary way a walk
// over a block ends, not a fault, which is why a caller is expected to test for
// it rather than pass it on.
var ErrNoSuchSlot = errors.New("no such slot")

// slotState is the flag at the front of a slot, saying whether the slot holds a
// record. It is stored as an int because that is one of the two widths a
// transaction knows how to log.
type slotState int32

const (
	// slotEmpty is the state of a slot that holds no record. It is zero so
	// that an appended block, which comes back filled with zeroes, reads as a
	// block of empty slots without having to be written over first.
	slotEmpty slotState = iota
	// slotInUse is the state of a slot that holds a record.
	slotInUse
)

// RecordPage is one block seen as an array of slots, each slot holding one
// record laid out the way its layout says.
//
// It is where a schema and a layout stop being descriptions and start being
// bytes. Every read and write goes through the transaction rather than the
// buffer, so that the change is logged and the block is locked like any other.
type RecordPage struct {
	tx     *transaction.Transaction
	blk    *filemanager.BlockId
	layout *Layout
}

// NewRecordPage takes hold of blk on behalf of tx and reads it as slots of the
// shape layout gives.
//
// A layout whose slot is wider than a block is refused. Such a page could hold
// no records at all, since slot 0 alone already runs past the end of the block,
// and every call on it would fail one at a time for that reason. Saying so once
// here also stops the one caller that would not fail: TableScan.MoveToNewRecord
// reads "no free slot in this block" as "append another block and look again",
// and against a layout nothing fits in it would do that until the disk filled.
//
// The check comes before the pin so that a refusal leaves the block as it was
// found. Pinning and then refusing would hold a buffer on behalf of a page
// that was never handed out, and nothing would be left holding the page to
// give it back.
//
// The block is pinned here because a record page is only useful while the block
// it stands for is in a buffer, and pinning at each read would leave the page
// working on a block that may have been replaced between two of them. The pin
// is the transaction's to give back, at Unpin or when it ends.
func NewRecordPage(tx *transaction.Transaction, blk *filemanager.BlockId, layout *Layout) (*RecordPage, error) {
	if slotSize := layout.SlotSize(); slotSize > tx.BlockSize() {
		return nil, fmt.Errorf("read %s as records of %d bytes, which a block of %d bytes cannot hold one of: %w", blk, slotSize, tx.BlockSize(), ErrSlotWiderThanBlock)
	}

	if err := tx.Pin(blk); err != nil {
		return nil, fmt.Errorf("pin %s to read it as records: %w", blk, err)
	}

	return &RecordPage{
		tx:     tx,
		blk:    blk,
		layout: layout,
	}, nil
}

// BlockID is the block this page stands for.
//
// A page is only ever read through the methods below, which take a slot and
// need no block from the caller. What needs it is a scan over the table the
// block belongs to: the block number is how it tells the end of one block from
// the end of the file, and it is half of every record id it hands out.
func (rp *RecordPage) BlockID() *filemanager.BlockId {
	return rp.blk
}

// slotOffset is where slot begins within the block. Every slot is the same
// size, so a slot's position is its number times that size, which is what lets
// a record page reach a record without reading the ones before it.
//
// A slot that would run past the end of the block is refused. Page checks the
// bounds of a write, but a read indexes the buffer directly, so without this an
// out of range slot would be a panic rather than an error.
func (rp *RecordPage) slotOffset(slot int) (int, error) {
	if !rp.HasSlot(slot) {
		return 0, fmt.Errorf("slot %d is not a slot of %s: %w", slot, rp.blk, ErrSlotOutOfRange)
	}

	return slot * rp.layout.SlotSize(), nil
}

// fieldPos is where fieldName of slot sits within the block, once the slot has
// been found to fit and the field to be of the type the caller is reading or
// writing it as.
//
// The type is checked because the bytes of one type read as another are still
// bytes: an int taken from the front of a varchar is its length, and a caller
// that had the wrong method would get a number rather than an error.
func (rp *RecordPage) fieldPos(slot int, fieldName string, fieldType FieldType) (int, error) {
	slotOffset, err := rp.slotOffset(slot)
	if err != nil {
		return 0, err
	}

	actual, err := rp.layout.schema.Type(fieldName)
	if err != nil {
		return 0, err
	}
	if actual != fieldType {
		return 0, fmt.Errorf("field %q is a %s, not a %s: %w", fieldName, actual, fieldType, ErrFieldTypeMismatch)
	}

	fieldOffset, err := rp.layout.Offset(fieldName)
	if err != nil {
		return 0, err
	}

	return slotOffset + fieldOffset, nil
}

// HasSlot reports whether slot is one of the slots this block holds: not
// before the first, and with its last byte still inside the block.
//
// The last byte is what settles it. A slot whose start is inside the block but
// whose end is not would be read and written across the boundary, so the block
// holds as many whole slots as fit and no part of another.
//
// It is exported because a record id carries no layout, so one made against a
// table whose records are a different size names a slot this block may not
// have. Whoever moves to a record id asks here rather than finding out at the
// first read of a field.
func (rp *RecordPage) HasSlot(slot int) bool {
	return slot >= 0 && (slot+1)*rp.layout.SlotSize() <= rp.tx.BlockSize()
}

// searchAfter returns the first slot after slot whose flag is state.
//
// The search starts past slot rather than at it, so that a caller walking a
// block can hand back the slot it has just finished with and get the next one.
// Slot -1 asks for the first slot of the block.
//
// Reaching the end of the block without a match is ErrNoSuchSlot, which is how
// a walk finds out it is over.
func (rp *RecordPage) searchAfter(slot int, state slotState) (int, error) {
	for next := slot + 1; rp.HasSlot(next); next++ {
		// slotOffset cannot fail here: the loop only runs on a slot HasSlot
		// has accepted, which is the one thing it refuses. The error is
		// returned rather than dropped so that this stays true if it grows
		// another reason to.
		offset, err := rp.slotOffset(next)
		if err != nil {
			return 0, err
		}

		flag, err := rp.tx.GetInt(rp.blk, offset)
		if err != nil {
			return 0, err
		}
		if slotState(flag) == state {
			return next, nil
		}
	}

	return 0, fmt.Errorf("search %s after slot %d: %w", rp.blk, slot, ErrNoSuchSlot)
}

// setSlotState writes state to the flag at the front of slot.
//
// The flag sits before the fields rather than after them so that it is at the
// same place in every slot whatever the schema is, which is what lets a page be
// searched for a free slot without a layout for the records in it.
func (rp *RecordPage) setSlotState(slot int, state slotState) error {
	offset, err := rp.slotOffset(slot)
	if err != nil {
		return err
	}

	return rp.tx.SetInt(rp.blk, offset, int32(state))
}

// InitializeNewBlock makes every slot of the block empty and every field of
// every slot the zero value of its type.
//
// It is meant for a block just appended to a file, which is why it says "new":
// called on a block that holds records, it would throw all of them away.
//
// The writes go through the transaction like any other, so they are logged.
// SimpleDB skips the log here on the grounds that a block that did not exist
// before has no earlier value worth restoring, but the saving is one block's
// worth of records at the moment a file grows, and the exception would have to
// be a way of writing without logging, which is worth more than it costs.
func (rp *RecordPage) InitializeNewBlock() error {
	schema := rp.layout.schema

	for slot := 0; rp.HasSlot(slot); slot++ {
		if err := rp.setSlotState(slot, slotEmpty); err != nil {
			return err
		}

		for _, fieldName := range schema.fields {
			var err error

			switch fieldType := schema.info[fieldName].fieldType; fieldType {
			case FieldTypeInt:
				err = rp.SetInt(slot, fieldName, 0)
			case FieldTypeVarchar:
				err = rp.SetString(slot, fieldName, "")
			default:
				err = fmt.Errorf("initialize field %q of slot %d: %s has no value to start from", fieldName, slot, fieldType)
			}

			if err != nil {
				return err
			}
		}
	}

	return nil
}

// NextUsedSlotAfter returns the first slot after slot that holds a record.
// Slot -1 asks for the first record in the block.
//
// ErrNoSuchSlot means there are no more records past that point, which is how a
// scan over a table learns to move on to the next block.
func (rp *RecordPage) NextUsedSlotAfter(slot int) (int, error) {
	return rp.searchAfter(slot, slotInUse)
}

// ClaimFreeSlotAfter takes the first free slot after slot and returns it,
// marking it as holding a record. The fields are left as they were; filling
// them in is the caller's next step.
//
// The slot is marked before anything is written to it. Marking it afterwards
// would leave a gap in which the slot still reads as free, and the exclusive
// lock a write takes is on the whole block, not on the slot: another
// transaction that had already read the block would find the same free slot.
//
// ErrNoSuchSlot means the block is full, which is when a table appends another.
func (rp *RecordPage) ClaimFreeSlotAfter(slot int) (int, error) {
	free, err := rp.searchAfter(slot, slotEmpty)
	if err != nil {
		return 0, err
	}

	if err := rp.setSlotState(free, slotInUse); err != nil {
		return 0, err
	}

	return free, nil
}

// Delete marks slot as holding no record.
//
// The bytes of the record are left where they are. Nothing looks at a slot the
// flag calls empty, and the next record written there covers them, so clearing
// them would be work no reader could tell had been done.
func (rp *RecordPage) Delete(slot int) error {
	return rp.setSlotState(slot, slotEmpty)
}

// GetInt returns the int field fieldName of slot.
func (rp *RecordPage) GetInt(slot int, fieldName string) (int32, error) {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeInt)
	if err != nil {
		return 0, err
	}

	return rp.tx.GetInt(rp.blk, pos)
}

// SetInt writes val to the int field fieldName of slot.
func (rp *RecordPage) SetInt(slot int, fieldName string, val int32) error {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeInt)
	if err != nil {
		return err
	}

	return rp.tx.SetInt(rp.blk, pos, val)
}

// GetString returns the varchar field fieldName of slot.
func (rp *RecordPage) GetString(slot int, fieldName string) (string, error) {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeVarchar)
	if err != nil {
		return "", err
	}

	return rp.tx.GetString(rp.blk, pos)
}

// SetString writes val to the varchar field fieldName of slot.
//
// A string of more characters than the field was declared to hold is refused
// rather than written. The field is given room for its limit in the widest
// encoding there is, so a string a little over it would be written and read
// back intact, and the schema would be the only thing saying anything was
// wrong; a string far enough over reaches past the field and overwrites
// whatever follows it in the block. Both are the same violation of the width
// the schema declared, so both are refused here.
//
// The count is of characters rather than bytes because that is what a varchar's
// length means. Counting bytes would refuse strings that fit, since a character
// may take up to four of them.
func (rp *RecordPage) SetString(slot int, fieldName string, val string) error {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeVarchar)
	if err != nil {
		return err
	}

	// Length cannot fail here: fieldPos has already looked the field up in the
	// same schema. The error is returned rather than dropped so that this stays
	// true if it grows another reason to.
	length, err := rp.layout.schema.Length(fieldName)
	if err != nil {
		return err
	}
	if count := utf8.RuneCountInString(val); count > length {
		return fmt.Errorf("write %d characters to field %q, which holds %d: %w", count, fieldName, length, ErrStringTooLong)
	}

	return rp.tx.SetString(rp.blk, pos, val)
}
