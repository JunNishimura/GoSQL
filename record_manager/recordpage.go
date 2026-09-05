package recordmanager

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrSlotOutOfRange reports a slot number that does not fit in the block.
var ErrSlotOutOfRange = errors.New("slot out of range")

// ErrFieldTypeMismatch reports reading or writing a field as one type when the
// schema says it is another.
var ErrFieldTypeMismatch = errors.New("field type mismatch")

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
// The block is pinned here because a record page is only useful while the block
// it stands for is in a buffer, and pinning at each read would leave the page
// working on a block that may have been replaced between two of them. The pin
// is the transaction's to give back, at Unpin or when it ends.
func NewRecordPage(tx *transaction.Transaction, blk *filemanager.BlockId, layout *Layout) (*RecordPage, error) {
	if err := tx.Pin(blk); err != nil {
		return nil, fmt.Errorf("pin %s to read it as records: %w", blk, err)
	}

	return &RecordPage{
		tx:     tx,
		blk:    blk,
		layout: layout,
	}, nil
}

// slotOffset is where slot begins within the block. Every slot is the same
// size, so a slot's position is its number times that size, which is what lets
// a record page reach a record without reading the ones before it.
//
// A slot that would run past the end of the block is refused. Page checks the
// bounds of a write, but a read indexes the buffer directly, so without this an
// out of range slot would be a panic rather than an error.
func (rp *RecordPage) slotOffset(slot int) (int, error) {
	if !rp.isValidSlot(slot) {
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

// isValidSlot reports whether slot is one of the slots this block holds: not
// before the first, and with its last byte still inside the block.
//
// The last byte is what settles it. A slot whose start is inside the block but
// whose end is not would be read and written across the boundary, so the block
// holds as many whole slots as fit and no part of another.
func (rp *RecordPage) isValidSlot(slot int) bool {
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
	for next := slot + 1; rp.isValidSlot(next); next++ {
		// slotOffset cannot fail here: the loop only runs on a slot
		// isValidSlot has accepted, which is the one thing it refuses. The
		// error is returned rather than dropped so that this stays true if it
		// grows another reason to.
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
func (rp *RecordPage) SetString(slot int, fieldName string, val string) error {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeVarchar)
	if err != nil {
		return err
	}

	return rp.tx.SetString(rp.blk, pos, val)
}
