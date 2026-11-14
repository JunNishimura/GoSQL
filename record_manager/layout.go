package recordmanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// slotFlagBytes is the number of bytes reserved at the start of every slot
// for the flag that marks whether the slot holds a record.
//
// The flag is as wide as an int, not a single byte. A record page reads and
// writes the flag through a Transaction, and a transaction can only log an
// int or a string. This costs three extra bytes per slot.
const slotFlagBytes = filemanager.IntBytes

// Layout holds the offset of each field within a slot, and the slot size. A
// Schema only describes the fields of a table. A Layout adds the positions
// that a record page needs to read and write them.
//
// Every slot has the same size, and every field has the same offset in every
// slot. A record page can therefore locate a field without reading the record
// first. For the same reason a varchar is sized by the limit in its schema,
// not by the string stored in it.
type Layout struct {
	schema   *Schema
	offsets  map[string]int
	slotSize int
}

// NewLayout computes the field offsets and the slot size for schema. It
// places the fields in schema order, after the in-use flag at the start of
// the slot.
//
// NewLayout returns no error because it cannot fail: it only queries the
// schema about fields that the schema itself listed.
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

// lengthInBytes returns the number of bytes that a field with this info takes
// in a slot. A varchar is sized by the limit in its schema, not by a value,
// so that every slot has the same size.
func lengthInBytes(info fieldInfo) int {
	if info.fieldType == FieldTypeInt {
		return filemanager.IntBytes
	}

	return filemanager.MaxLength(info.length)
}

// NewLayoutFromCatalog restores the layout that a table was created with. It
// uses the saved offsets and slot size and does not compute them again.
//
// Computing them again would be wrong, even though NewLayout follows the same
// rules. The records on disk were written with the saved layout. A version of
// this code that placed fields differently would read every record at the
// wrong offsets.
//
// The layout keeps only the offsets of the schema's own fields: one offset
// per field, and no others. A field with no saved offset causes an error.
// Such a field means that the catalog does not match the schema, and without
// the error the field would be read at offset zero.
func NewLayoutFromCatalog(schema *Schema, offsets map[string]int, slotSize int) (*Layout, error) {
	own := make(map[string]int, len(schema.fields))
	for _, fieldName := range schema.fields {
		offset, ok := offsets[fieldName]
		if !ok {
			return nil, fmt.Errorf("restore the layout: no saved offset for field %q: %w", fieldName, ErrFieldNotFound)
		}
		own[fieldName] = offset
	}

	return &Layout{
		schema:   schema,
		offsets:  own,
		slotSize: slotSize,
	}, nil
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
