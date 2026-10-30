package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
)

var _ AggregationFn = (*CountFn)(nil)

// CountFn counts the records of a group.
//
// It reads nothing from them: every record of the group counts, whatever its
// field holds. The field is only what the count is named after.
type CountFn struct {
	fieldName string
	count     int32
}

// NewCountFn returns a count of the records of a group, named after fieldName.
func NewCountFn(fieldName string) *CountFn {
	return &CountFn{fieldName: fieldName}
}

// ProcessFirst starts the count at the first record of a group.
func (f *CountFn) ProcessFirst(s query.Scan) error {
	f.count = 1

	return nil
}

// ProcessNext counts one more record.
func (f *CountFn) ProcessNext(s query.Scan) error {
	f.count++

	return nil
}

// FieldName is "countof" followed by the field the count was made for.
func (f *CountFn) FieldName() string {
	return "countof" + f.fieldName
}

// Value is the number of records counted, as an int.
func (f *CountFn) Value() query.Constant {
	return query.NewIntConstant(f.count)
}
