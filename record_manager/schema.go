package recordmanager

import (
	"errors"
	"fmt"
	"slices"
)

// ErrFieldNotFound is returned for a lookup of a field that the schema does
// not have.
var ErrFieldNotFound = errors.New("field not found")

// ErrDuplicateField is returned when a field is added that the schema already
// has.
var ErrDuplicateField = errors.New("duplicate field")

// FieldType is the type of a field's values. A schema records it so that
// callers know which typed accessor of the transaction to use, and so that a
// layout can compute the size of a value.
type FieldType int

const (
	// FieldTypeInt is a 32 bit integer, stored in a fixed number of bytes.
	FieldTypeInt FieldType = iota
	// FieldTypeVarchar is a string with a maximum number of characters. That
	// limit is the length of the field. It gives the field a fixed size in a
	// record, although the stored strings vary in length.
	FieldTypeVarchar
)

// String returns the name of the type as it should appear in an error
// message, such as "varchar" or "int".
func (ft FieldType) String() string {
	switch ft {
	case FieldTypeInt:
		return "int"
	case FieldTypeVarchar:
		return "varchar"
	default:
		return fmt.Sprintf("FieldType(%d)", int(ft))
	}
}

// fieldInfo holds the type and length of one field. Length is the character
// limit of a varchar field. It is unused for an int, whose size is fixed.
type fieldInfo struct {
	fieldType FieldType
	length    int
}

// Schema is the field structure of a table: the name, type and length of each
// field, in the order in which the fields were added.
//
// A schema does not define where a field is stored in a record or how many
// bytes it takes. A layout defines that. With this separation, code that
// describes a table can build and pass a schema without choosing how records
// are written to a page.
//
// The order is kept because a layout assigns offsets in field order. Two
// schemas with the same fields in a different order describe the same table,
// but not the same record format.
type Schema struct {
	fields []string
	info   map[string]fieldInfo
}

// NewSchema returns a schema with no fields.
func NewSchema() *Schema {
	return &Schema{
		fields: []string{},
		info:   map[string]fieldInfo{},
	}
}

// AddField adds a field with the given type and length. length is the
// character limit for a varchar, and is ignored for an int.
//
// AddField returns an error for a name that the schema already has, and does
// not redefine the field. A redefinition would put the name into fields
// twice. A layout would then assign the field two offsets and count its bytes
// twice.
func (s *Schema) AddField(fieldName string, fieldType FieldType, length int) error {
	if _, ok := s.info[fieldName]; ok {
		return fmt.Errorf("add field %q: %w", fieldName, ErrDuplicateField)
	}

	s.fields = append(s.fields, fieldName)
	s.info[fieldName] = fieldInfo{
		fieldType: fieldType,
		length:    length,
	}

	return nil
}

// AddIntField adds an int field. An int field needs no length.
func (s *Schema) AddIntField(fieldName string) error {
	return s.AddField(fieldName, FieldTypeInt, 0)
}

// AddStringField adds a varchar field that holds at most length characters.
func (s *Schema) AddStringField(fieldName string, length int) error {
	return s.AddField(fieldName, FieldTypeVarchar, length)
}

// Add copies the field fieldName from other into this schema. The output
// schema of a query is built this way from its input schemas, so the type and
// length come from other and not from the caller.
func (s *Schema) Add(fieldName string, other *Schema) error {
	info, ok := other.info[fieldName]
	if !ok {
		return fmt.Errorf("copy field %q from the other schema: %w", fieldName, ErrFieldNotFound)
	}

	return s.AddField(fieldName, info.fieldType, info.length)
}

// AddAll copies every field of other into this schema, in other's order.
//
// AddAll copies either all fields or none. A schema that holds half of
// another schema is neither of the two schemas that the caller wanted to
// combine, and the error does not tell the caller how many fields were added.
// AddAll therefore checks for duplicate names before it copies anything.
// AddField would also report a duplicate, but only after the fields before it
// had been added.
func (s *Schema) AddAll(other *Schema) error {
	for _, fieldName := range other.fields {
		if _, ok := s.info[fieldName]; ok {
			return fmt.Errorf("copy every field of the other schema: add field %q: %w", fieldName, ErrDuplicateField)
		}
	}

	// AddField cannot fail here. The loop above excluded a duplicate with this
	// schema, and other cannot hold a name twice for the same reason. The error
	// is still returned, so that the code stays correct if AddField gains
	// another failure case.
	for _, fieldName := range other.fields {
		info := other.info[fieldName]
		if err := s.AddField(fieldName, info.fieldType, info.length); err != nil {
			return err
		}
	}

	return nil
}

// Fields returns the field names in the order in which they were added.
//
// The returned slice is a copy. A layout assigns offsets in this order. If a
// caller sorted or overwrote the schema's own slice, the record format would
// change silently: the field names and types would still be correct, and only
// the offsets computed later would be wrong.
func (s *Schema) Fields() []string {
	return slices.Clone(s.fields)
}

// HasField reports whether the schema has a field named fieldName.
func (s *Schema) HasField(fieldName string) bool {
	_, ok := s.info[fieldName]
	return ok
}

// Type returns the type of fieldName.
func (s *Schema) Type(fieldName string) (FieldType, error) {
	info, ok := s.info[fieldName]
	if !ok {
		return 0, fmt.Errorf("read the type of field %q: %w", fieldName, ErrFieldNotFound)
	}

	return info.fieldType, nil
}

// Length returns the character limit of fieldName. It is 0 for an int field.
func (s *Schema) Length(fieldName string) (int, error) {
	info, ok := s.info[fieldName]
	if !ok {
		return 0, fmt.Errorf("read the length of field %q: %w", fieldName, ErrFieldNotFound)
	}

	return info.length, nil
}
