package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ AggregationFn = (*MaxFn)(nil)

// MaxFn finds the largest value a field holds among the records of a group, in
// the order Constant.Compare puts values in.
type MaxFn struct {
	fieldName string
	max       query.Constant
}

// NewMaxFn returns the largest value of fieldName among the records of a
// group.
func NewMaxFn(fieldName string) *MaxFn {
	return &MaxFn{fieldName: fieldName}
}

// ProcessFirst takes the value of the first record of a group as the largest.
func (f *MaxFn) ProcessFirst(s query.Scan) error {
	val, err := s.GetValue(f.fieldName)
	if err != nil {
		return err
	}
	f.max = val

	return nil
}

// ProcessNext keeps the value of the record if it is larger than the largest
// so far.
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

// FieldName is "maxof" followed by the field whose largest value is found.
func (f *MaxFn) FieldName() string {
	return "maxof" + f.fieldName
}

// Value is the largest value found.
func (f *MaxFn) Value() query.Constant {
	return f.max
}

// AddFieldTo adds the largest value to schema, of the type and length of the
// field of src it is found in.
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
