package recordmanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// slotFlagBytes is the room set aside at the start of every slot for the flag
// saying whether the slot holds a record.
//
// It is the width of an int rather than of a single byte because a record page
// reads and writes that flag through a Transaction, and the only widths a
// transaction knows how to log are an int and a string. Three bytes per slot is
// what that costs.
const slotFlagBytes = filemanager.IntBytes

// Layout is where each field of a record sits within its slot, and how large a
// slot is. It is what turns a Schema, which says only what a table's fields
// are, into something a record page can read and write.
//
// Every slot is the same size, and every field is at the same offset in every
// slot. That is what lets a record page find a field without reading the record
// first, and it is why a varchar is sized by the limit its schema gives rather
// than by the string that happens to be in it.
type Layout struct {
	schema   *Schema
	offsets  map[string]int
	slotSize int
}

// NewLayout works out the offsets and the slot size for schema, walking its
// fields in order and leaving room at the front for the in-use flag.
//
// It cannot fail, which is why it returns no error: it only ever asks the
// schema about the fields the schema itself listed.
func NewLayout(schema *Schema) *Layout {
	offsets := make(map[string]int, len(schema.fields))

	pos := slotFlagBytes
	for _, fieldName := range schema.fields {
		offsets[fieldName] = pos
		pos += lengthInBytes(schema.info[fieldName])
	}

	return &Layout{
		schema:   schema,
		offsets:  offsets,
		slotSize: pos,
	}
}

// lengthInBytes is the room one field of this kind takes up in a slot. A
// varchar is sized by the limit its schema gives, not by any value, so that
// every slot comes out the same size.
func lengthInBytes(info fieldInfo) int {
	if info.fieldType == FieldTypeInt {
		return filemanager.IntBytes
	}

	return filemanager.MaxLength(info.length)
}

// Schema returns the schema this layout was built from.
func (l *Layout) Schema() *Schema {
	return l.schema
}

// Offset returns the position of fieldName within a slot.
func (l *Layout) Offset(fieldName string) (int, error) {
	offset, ok := l.offsets[fieldName]
	if !ok {
		return 0, fmt.Errorf("read the offset of field %q: %w", fieldName, ErrFieldNotFound)
	}

	return offset, nil
}

// SlotSize returns the number of bytes one slot takes up.
func (l *Layout) SlotSize() int {
	return l.slotSize
}
