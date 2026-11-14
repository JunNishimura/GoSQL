package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ AggregationFn = (*CountFn)(nil)

// CountFn counts the records of a group.
//
// It reads nothing from the records. Every record of the group counts,
// whatever the value of its field. The field only provides the name of the
// count.
type CountFn struct {
	fieldName string
	count     int32
}

// NewCountFn returns a count of the records of a group, named after
// fieldName.
func NewCountFn(fieldName string) *CountFn {
	return &CountFn{fieldName: fieldName}
}

// ProcessFirst starts the count with the first record of a group.
func (f *CountFn) ProcessFirst(s query.Scan) error {
	f.count = 1

	return nil
}

// ProcessNext counts one more record.
func (f *CountFn) ProcessNext(s query.Scan) error {
	f.count++

	return nil
}

// FieldName returns "countof" followed by the field that the count is named
// after.
func (f *CountFn) FieldName() string {
	return "countof" + f.fieldName
}

// Value returns the number of records counted, as an int.
func (f *CountFn) Value() query.Constant {
	return query.NewIntConstant(f.count)
}

// AddFieldTo adds the count to schema as an int field.
func (f *CountFn) AddFieldTo(schema, src *recordmanager.Schema) error {
	return schema.AddIntField(f.FieldName())
}
