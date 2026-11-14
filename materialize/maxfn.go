package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ AggregationFn = (*MaxFn)(nil)

// MaxFn finds the largest value of a field among the records of a group,
// according to the order of Constant.Compare.
type MaxFn struct {
	fieldName string
	max       query.Constant
}

// NewMaxFn returns a function that finds the largest value of fieldName among
// the records of a group.
func NewMaxFn(fieldName string) *MaxFn {
	return &MaxFn{fieldName: fieldName}
}

// ProcessFirst sets the largest value to the value of the first record of a
// group.
func (f *MaxFn) ProcessFirst(s query.Scan) error {
	val, err := s.GetValue(f.fieldName)
	if err != nil {
		return err
	}
	f.max = val

	return nil
}

// ProcessNext replaces the largest value with the value of the record if that
// value is larger.
func (f *MaxFn) ProcessNext(s query.Scan) error {
	val, err := s.GetValue(f.fieldName)
	if err != nil {
		return err
	}

	c, err := val.Compare(f.max)
	if err != nil {
		return err
	}
	if c > 0 {
		f.max = val
	}

	return nil
}

// FieldName returns "maxof" followed by the name of the field.
func (f *MaxFn) FieldName() string {
	return "maxof" + f.fieldName
}

// Value returns the largest value found.
func (f *MaxFn) Value() query.Constant {
	return f.max
}

// AddFieldTo adds the field of the largest value to schema, with the type and
// length that the field has in src.
func (f *MaxFn) AddFieldTo(schema, src *recordmanager.Schema) error {
	fieldType, err := src.Type(f.fieldName)
	if err != nil {
		return err
	}
	length, err := src.Length(f.fieldName)
	if err != nil {
		return err
	}

	return schema.AddField(f.FieldName(), fieldType, length)
}
