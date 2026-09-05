package recordmanager

import (
	"errors"
	"fmt"
	"slices"
)

// ErrFieldNotFound reports a lookup of a field the schema does not have.
var ErrFieldNotFound = errors.New("field not found")

// ErrDuplicateField reports an attempt to add a field the schema already has.
var ErrDuplicateField = errors.New("duplicate field")

// FieldType is the type of a field's values. A schema records it so that
// callers know which of the transaction's typed accessors to reach for, and so
// that a layout can work out how much room a value needs.
type FieldType int

const (
	// FieldTypeInt is a 32 bit integer, stored in a fixed number of bytes.
	FieldTypeInt FieldType = iota
	// FieldTypeVarchar is a string of at most some number of characters. That
	// limit is the field's length, and it is what makes the field's size in a
	// record fixed even though the strings put in it vary.
	FieldTypeVarchar
)

// String names the type the way a reader of an error message would, so that
// being told a field is a varchar rather than an int says something.
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

// fieldInfo is what a schema knows about one field. Length is the character
// limit of a varchar field and is unused for an int, whose width is fixed.
type fieldInfo struct {
	fieldType FieldType
	length    int
}

// Schema is the field structure of a table: the name, type and length of each
// of its fields, in the order they were added.
//
// It says nothing about where a field sits in a record or how many bytes it
// takes up. That is a layout's job, and keeping the two apart means a schema
// can be built and passed around by whoever is describing a table without
// committing to how records are written to a page.
//
// The order is kept because a layout assigns offsets by walking the fields, so
// two schemas with the same fields added in a different order are the same
// table but not the same record format.
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

// AddField adds a field of the given type and length. length is the character
// limit for a varchar and is ignored for an int.
//
// A name the schema already has is refused rather than redefined. Redefining it
// would leave the name in fields twice, and a layout walking those would give
// the one field two offsets and count its bytes twice.
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

// AddIntField adds an int field, which needs no length of its own.
func (s *Schema) AddIntField(fieldName string) error {
	return s.AddField(fieldName, FieldTypeInt, 0)
}

// AddStringField adds a varchar field holding at most length characters.
func (s *Schema) AddStringField(fieldName string, length int) error {
	return s.AddField(fieldName, FieldTypeVarchar, length)
}

// Add copies fieldName from other into this schema. It is how a query's output
// schema is built out of the schemas it draws from, so the type and length come
// from other rather than from the caller.
func (s *Schema) Add(fieldName string, other *Schema) error {
	info, ok := other.info[fieldName]
	if !ok {
		return fmt.Errorf("copy field %q from the other schema: %w", fieldName, ErrFieldNotFound)
	}

	return s.AddField(fieldName, info.fieldType, info.length)
}

// AddAll copies every field of other into this schema, keeping other's order.
//
// Nothing is copied unless all of it can be: a schema left holding half of
// another one is neither of the two the caller meant to combine, and the caller
// cannot tell from the error how far it got.
// The clashing names are looked for before anything is copied. Leaving it to
// AddField would report the clash just as well, but only after the fields ahead
// of it had already been added.
func (s *Schema) AddAll(other *Schema) error {
	for _, fieldName := range other.fields {
		if _, ok := s.info[fieldName]; ok {
			return fmt.Errorf("copy every field of the other schema: add field %q: %w", fieldName, ErrDuplicateField)
		}
	}

	// AddField cannot fail here: the loop above ruled out a clash with this
	// schema, and other cannot hold a name twice for the same reason. The error
	// is returned rather than dropped so that this stays true if AddField grows
	// another way to refuse a field.
	for _, fieldName := range other.fields {
		info := other.info[fieldName]
		if err := s.AddField(fieldName, info.fieldType, info.length); err != nil {
			return err
		}
	}

	return nil
}

// Fields returns the field names in the order they were added.
//
// The slice is a copy. The order is what a layout walks to assign offsets, so a
// caller that sorted or overwrote the schema's own slice would change the record
// format out from under it, and silently: the field names and types would still
// answer correctly, and only the offsets computed later would be wrong.
func (s *Schema) Fields() []string {
	return slices.Clone(s.fields)
}

// HasField reports whether the schema has a field of this name.
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

// Length returns the character limit of fieldName, which is 0 for an int field.
func (s *Schema) Length(fieldName string) (int, error) {
	info, ok := s.info[fieldName]
	if !ok {
		return 0, fmt.Errorf("read the length of field %q: %w", fieldName, ErrFieldNotFound)
	}

	return info.length, nil
}
