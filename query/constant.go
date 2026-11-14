package query

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ErrConstantTypeMismatch is returned when a constant is read as a type other
// than the type it was created with.
//
// It is separate from recordmanager.ErrFieldTypeMismatch, which a record page
// returns when a field is read as the wrong type. A constant has no schema.
// This error concerns the value that a caller holds. That error concerns the
// type that the table declares for the field.
var ErrConstantTypeMismatch = errors.New("constant type mismatch")

// Constant is one field value: an int or a string, together with its type.
//
// The type is stored and not derived from the contents. Both values are
// always present, and only one of them is meaningful. The unused value is
// left at its zero value, so an int 0 and a varchar "" have the same bytes.
// Without the stored type they would be the same constant.
//
// The zero value of the unused field also makes == correct. Two constants of
// the same type differ only in the meaningful value, and two constants of
// different types differ in the type. Constant therefore has no Equals
// method. recordmanager.RecordID needs one because it is passed as a pointer,
// and == on two pointers compares addresses. As a plain value, a constant can
// also be a map key, which grouping and hash joins need.
//
// The zero value is the int 0, because both the unset type and the unset
// number are zero. No Constant represents "no value". Code that needs that
// uses a *Constant or a second return value, not a reserved Constant.
type Constant struct {
	fieldType recordmanager.FieldType
	intVal    int32
	strVal    string
}

// NewIntConstant returns a constant that holds val as an int.
func NewIntConstant(val int32) Constant {
	return Constant{
		fieldType: recordmanager.FieldTypeInt,
		intVal:    val,
	}
}

// NewStringConstant returns a constant that holds val as a varchar.
func NewStringConstant(val string) Constant {
	return Constant{
		fieldType: recordmanager.FieldTypeVarchar,
		strVal:    val,
	}
}

// Type returns the type of the constant's value, as a schema field type.
//
// Constants and schemas deliberately use the same type. Whether a constant
// can be written to a field is decided by comparing this type with the type
// of the field, with no conversion between the two.
func (c Constant) Type() recordmanager.FieldType {
	return c.fieldType
}

// AsInt returns the number of an int constant.
//
// AsInt returns an error for a varchar constant and does not convert it. A
// varchar "42" is the text of a number, not a number. A caller that received
// 42 from it would hold a value that no record ever held.
//
// Most callers check Type first and never get this error. It is still
// returned, because the type system does not prevent a call without that
// check.
func (c Constant) AsInt() (int32, error) {
	if c.fieldType != recordmanager.FieldTypeInt {
		return 0, fmt.Errorf("read %s as an int: %w", c, ErrConstantTypeMismatch)
	}

	return c.intVal, nil
}

// AsString returns the text of a varchar constant.
func (c Constant) AsString() (string, error) {
	if c.fieldType != recordmanager.FieldTypeVarchar {
		return "", fmt.Errorf("read %s as a varchar: %w", c, ErrConstantTypeMismatch)
	}

	return c.strVal, nil
}

// Compare returns -1, 0 or +1 if c is less than, equal to or greater than
// other, like cmp.Compare.
//
// A B-tree uses it to keep its entries sorted. Equality is still tested with
// ==, and Compare is consistent with it: it returns 0 for exactly the pairs
// that are ==.
//
// Ints are ordered as signed numbers. Varchars are ordered byte by byte. For
// ASCII text this is the order of the character codes, which puts every
// upper-case letter before every lower-case letter. The empty string is less
// than every other string, so it can serve as the minimum varchar value.
//
// Constants of different types have no order. Compare returns
// ErrConstantTypeMismatch for such a pair and does not rank one type first.
// An index holds the values of one field, so they all have one type. A mixed
// pair means that the caller took a value from the wrong field.
func (c Constant) Compare(other Constant) (int, error) {
	if c.fieldType != other.fieldType {
		return 0, fmt.Errorf("compare %s with %s: %w", c, other, ErrConstantTypeMismatch)
	}

	if c.fieldType == recordmanager.FieldTypeInt {
		return cmp.Compare(c.intVal, other.intVal), nil
	}

	return strings.Compare(c.strVal, other.strVal), nil
}

// String formats an int as the number and a varchar as the text in single
// quotes, so that a message shows the type of the constant.
//
// Without quotes, the int 42 and the varchar '42' would look the same. A type
// mismatch error has to show exactly that difference.
//
// The quotes are single because SQL writes strings that way. A predicate
// printed from constants is therefore SQL that can be parsed again, and the
// query of a view is stored in this form. The text is not escaped. The SQL
// parsed here cannot express a quote inside a string, so no parsed constant
// contains one.
func (c Constant) String() string {
	switch c.fieldType {
	case recordmanager.FieldTypeInt:
		return fmt.Sprintf("%d", c.intVal)
	case recordmanager.FieldTypeVarchar:
		return "'" + c.strVal + "'"
	default:
		return fmt.Sprintf("Constant(%s)", c.fieldType)
	}
}
