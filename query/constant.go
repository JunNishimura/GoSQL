package query

import (
	"errors"
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ErrConstantTypeMismatch reports reading a constant as one type when it was
// made as the other.
//
// It is kept apart from recordmanager.ErrFieldTypeMismatch, which the record
// page raises when a field is read as the wrong type, because a constant
// carries no schema. This one is about the value a caller is holding; that one
// is about what the table says the field is.
var ErrConstantTypeMismatch = errors.New("constant type mismatch")

// Constant is one value of a field: a number or a string, along with which of
// the two it is.
//
// The kind is carried rather than worked out from what is stored, because both
// values are always present and only one of them means anything. The one that
// does not is left at the zero value of its type, so an int of 0 and a varchar
// of "" are the same bytes; without the kind they would be the same constant.
//
// Leaving the unused value at its zero value is also what makes == right: two
// constants of the same kind differ only in the one value that means anything,
// and two of different kinds differ in the kind whatever they hold. So there is
// no Equals method here, unlike recordmanager.RecordID, which needs one because
// it is passed as a pointer and == on two of those compares where they are.
// Being a plain value also makes a constant usable as a map key, which is what
// grouping rows and hashing a join will want.
//
// The zero value is the int 0, since that is what an unset kind and an unset
// number come to. A constant standing for no value at all is not one of these:
// whoever needs that says so with a *Constant or a second return value, rather
// than with a Constant reserved to mean nothing.
type Constant struct {
	fieldType recordmanager.FieldType
	intVal    int32
	strVal    string
}

// NewIntConstant returns the constant holding val as an int.
func NewIntConstant(val int32) Constant {
	return Constant{
		fieldType: recordmanager.FieldTypeInt,
		intVal:    val,
	}
}

// NewStringConstant returns the constant holding val as a varchar.
func NewStringConstant(val string) Constant {
	return Constant{
		fieldType: recordmanager.FieldTypeVarchar,
		strVal:    val,
	}
}

// Type is the kind of value the constant holds, in the same terms a schema
// describes a field.
//
// The two are the same vocabulary on purpose: what a constant may be written to
// is settled by comparing this against the field's type, with nothing in
// between to translate one into the other.
func (c Constant) Type() recordmanager.FieldType {
	return c.fieldType
}

// AsInt returns the number a constant made from an int holds.
//
// A constant of the other kind is refused rather than converted. A varchar of
// "42" is the text of a number and not a number, and a caller handed 42 back
// from it would hold a value no record ever did.
//
// A caller that reached here having looked at Type first cannot see this error,
// which is most of them. It is returned rather than left out because nothing in
// the type stops a caller from asking without looking.
func (c Constant) AsInt() (int32, error) {
	if c.fieldType != recordmanager.FieldTypeInt {
		return 0, fmt.Errorf("read %s as an int: %w", c, ErrConstantTypeMismatch)
	}

	return c.intVal, nil
}

// AsString returns the text a constant made from a string holds.
func (c Constant) AsString() (string, error) {
	if c.fieldType != recordmanager.FieldTypeVarchar {
		return "", fmt.Errorf("read %s as a varchar: %w", c, ErrConstantTypeMismatch)
	}

	return c.strVal, nil
}

// String writes an int as the number and a varchar as the text in single
// quotes, so that a message naming a constant says which of the two it was.
//
// Unquoted, the int 42 and the varchar '42' read as the same thing, and telling
// those two apart is the whole of what a type mismatch has to report.
//
// The quotes are single because that is how SQL writes a string, so a
// predicate printed from these is SQL that can be parsed again, which is how a
// view's query is stored. Nothing inside the text is escaped: SQL as parsed here
// has no way to write a quote inside a string, so no constant parsed from it
// holds one.
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
