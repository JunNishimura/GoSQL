package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// AggregationFn works out one value from the records of a group, a record at
// a time, as a group by reads them.
//
// ProcessFirst is called on the first record of each group and starts the
// value over, and ProcessNext on each record after it. Value is the value of
// the records processed since the last ProcessFirst.
type AggregationFn interface {
	ProcessFirst(s query.Scan) error
	ProcessNext(s query.Scan) error
	// FieldName is the name the value goes by in the records of the group by.
	FieldName() string
	Value() query.Constant
	// AddFieldTo adds the field the value goes by to schema, of the type the
	// value is when the records aggregated are of src.
	AddFieldTo(schema, src *recordmanager.Schema) error
}
