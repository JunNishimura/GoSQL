package recordmanager

import (
	"errors"
	"fmt"
	"unicode/utf8"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrSlotOutOfRange is returned for a slot number that is outside the block.
var ErrSlotOutOfRange = errors.New("slot out of range")

// ErrFieldTypeMismatch is returned when a field is read or written as a type
// that differs from its type in the schema.
var ErrFieldTypeMismatch = errors.New("field type mismatch")

// ErrStringTooLong is returned when a string has more characters than the
// declared length of the varchar field it is written to.
var ErrStringTooLong = errors.New("string too long")

// ErrSlotWiderThanBlock is returned for a layout whose slot is larger than a
// block, so that no block can hold even one record.
var ErrSlotWiderThanBlock = errors.New("slot wider than block")

// ErrNoSuchSlot is returned when a search reaches the end of the block
// without finding a slot in the requested state. This is the normal end of an
// iteration over a block, not a failure. A caller is expected to check for it
// and not to propagate it.
var ErrNoSuchSlot = errors.New("no such slot")

// slotState is the flag at the start of a slot. It marks whether the slot
// holds a record. It is stored as an int because a transaction can only log
// an int or a string.
type slotState int32

const (
	// slotEmpty is the state of a slot that holds no record. It is zero
	// because an appended block is filled with zeros. Such a block is then a
	// block of empty slots without any further write.
	slotEmpty slotState = iota
	// slotInUse is the state of a slot that holds a record.
	slotInUse
)

// RecordPage is one block treated as an array of slots. Each slot holds one
// record in the format that the layout defines.
//
// This type turns a schema and a layout into bytes. Every read and write goes
// through the transaction and not directly through the buffer, so that the
// change is logged and the block is locked like any other block.
type RecordPage struct {
	tx     *transaction.Transaction
	blk    *filemanager.BlockId
	layout *Layout
}

// NewRecordPage pins blk for tx and reads it as slots in the format that
// layout defines.
//
// NewRecordPage returns an error for a layout whose slot is larger than a
// block. Such a page could hold no record, because slot 0 already extends
// past the end of the block, and every call on the page would fail for that
// reason. The check here also stops the one caller that would not fail.
// TableScan.MoveToNewRecord handles "no free slot in this block" by appending
// another block and searching again. With such a layout it would repeat that
// until the disk was full.
//
// The check runs before the pin, so that an error leaves the block unpinned.
// If the block were pinned first, the buffer would stay pinned for a page
// that was never returned, and no caller would hold the page to unpin it.
//
// The block is pinned here because a record page is only usable while its
// block is in a buffer. If each read pinned the block separately, the buffer
// could be replaced between two reads. The transaction releases the pin, at
// Unpin or when it ends.
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

// BlockID returns the block of this page.
//
// The methods below take a slot and need no block from the caller. A scan
// over the table needs the block for two purposes. It compares the block
// number with the file size to detect the end of the file, and it uses the
// block number as part of every record id it returns.
func (rp *RecordPage) BlockID() *filemanager.BlockId {
	return rp.blk
}

// slotOffset returns the offset of slot within the block. Every slot has the
// same size, so the offset is the slot number times the slot size. A record
// page can therefore reach a record without reading the records before it.
//
// slotOffset returns an error for a slot that extends past the end of the
// block. Page checks the bounds of a write, but a read indexes the buffer
// directly. Without this check, an out-of-range slot would cause a panic and
// not an error.
func (rp *RecordPage) slotOffset(slot int) (int, error) {
	if !rp.HasSlot(slot) {
		return 0, fmt.Errorf("slot %d is not a slot of %s: %w", slot, rp.blk, ErrSlotOutOfRange)
	}

	return slot * rp.layout.SlotSize(), nil
}

// fieldPos returns the offset of fieldName of slot within the block. It first
// checks that the slot is inside the block and that the field has the type
// that the caller reads or writes.
//
// The type check is needed because bytes of one type can be read as another
// type without a failure. An int read from the start of a varchar is the
// string's length, so a caller that used the wrong method would get a number
// and not an error.
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

// HasSlot reports whether slot is a slot of this block: it is not negative,
// and its last byte is inside the block.
//
// The last byte decides. A slot that starts inside the block but ends outside
// it would be read and written across the block boundary. A block therefore
// holds only as many whole slots as fit.
//
// HasSlot is exported because a record id carries no layout. A record id
// created for a table with a different record size can refer to a slot that
// this block does not have. A caller that moves to a record id checks here,
// and does not wait for the first field read to fail.
func (rp *RecordPage) HasSlot(slot int) bool {
	return slot >= 0 && (slot+1)*rp.layout.SlotSize() <= rp.tx.BlockSize()
}

// searchAfter returns the first slot after slot whose flag equals state.
//
// The search starts after slot, not at it. A caller that iterates over a
// block can therefore pass the slot it has just processed and get the next
// one. Slot -1 requests the first slot of the block.
//
// If the search reaches the end of the block without a match, it returns
// ErrNoSuchSlot. This is how an iteration detects its end.
func (rp *RecordPage) searchAfter(slot int, state slotState) (int, error) {
	for next := slot + 1; rp.HasSlot(next); next++ {
		// slotOffset cannot fail here, because the loop only runs for a slot
		// that HasSlot accepted, and slotOffset rejects nothing else. The
		// error is still returned, so that the code stays correct if
		// slotOffset gains another failure case.
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

// setSlotState writes state to the flag at the start of slot.
//
// The flag is placed before the fields, not after them. It is then at the
// same offset in every slot for any schema, and a page can be searched for a
// free slot without the layout of its records.
func (rp *RecordPage) setSlotState(slot int, state slotState) error {
	offset, err := rp.slotOffset(slot)
	if err != nil {
		return err
	}

	return rp.tx.SetInt(rp.blk, offset, int32(state))
}

// InitializeNewBlock sets every slot of the block to empty and every field of
// every slot to the zero value of its type.
//
// It is meant for a block that was just appended to a file, hence "new". On a
// block that holds records, it would discard all of them.
//
// The writes go through the transaction like any other write, so they are
// logged. SimpleDB skips the log here, because a block that did not exist
// before has no earlier value to restore. That saves only the log records of
// one block each time a file grows. It would also require a way to write
// without logging, and that exception costs more than it saves.
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
// Slot -1 requests the first record in the block.
//
// ErrNoSuchSlot means that there are no more records after slot. A scan over
// a table then moves on to the next block.
func (rp *RecordPage) NextUsedSlotAfter(slot int) (int, error) {
	return rp.searchAfter(slot, slotInUse)
}

// ClaimFreeSlotAfter finds the first free slot after slot, marks it as in
// use and returns it. The fields keep their previous contents, and the caller
// is expected to write them next.
//
// The slot is marked before any field is written. If it were marked
// afterwards, the slot would still appear free in the meantime. The exclusive
// lock of a write covers the whole block, not the slot, so another
// transaction that had already read the block would find the same free slot.
//
// ErrNoSuchSlot means that the block is full. A table then appends another
// block.
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

// Delete marks slot as empty.
//
// The bytes of the record are not cleared. Nothing reads a slot whose flag is
// empty, and the next record written to the slot overwrites them, so clearing
// them would have no observable effect.
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
// SetString returns an error for a string with more characters than the
// declared length of the field. The field reserves room for its limit in the
// widest encoding. A string slightly over the limit would therefore be
// written and read back intact, and only the schema would show the violation.
// A string far over the limit would extend past the field and overwrite the
// data after it in the block. Both cases violate the declared length, so both
// are rejected here.
//
// The length is counted in characters, not bytes, because a varchar's
// declared length is a character count. A count in bytes would reject strings
// that fit, because one character can take up to four bytes.
func (rp *RecordPage) SetString(slot int, fieldName string, val string) error {
	pos, err := rp.fieldPos(slot, fieldName, FieldTypeVarchar)
	if err != nil {
		return err
	}

	// Length cannot fail here, because fieldPos has already found the field in
	// the same schema. The error is still returned, so that the code stays
	// correct if Length gains another failure case.
	length, err := rp.layout.schema.Length(fieldName)
	if err != nil {
		return err
	}
	if count := utf8.RuneCountInString(val); count > length {
		return fmt.Errorf("write %d characters to field %q, which holds %d: %w", count, fieldName, length, ErrStringTooLong)
	}

	return rp.tx.SetString(rp.blk, pos, val)
}
