package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// AggregationFn computes one value from the records of a group. A group by
// passes the records to it one at a time.
//
// ProcessFirst is called for the first record of each group and resets the
// value. ProcessNext is called for each later record. Value returns the
// value for the records processed since the last ProcessFirst.
type AggregationFn interface {
	ProcessFirst(s query.Scan) error
	ProcessNext(s query.Scan) error
	// FieldName returns the name of the value in the records of the group by.
	FieldName() string
	Value() query.Constant
	// AddFieldTo adds the field of the value to schema. The type of the field
	// is the type of the value when the aggregated records have the schema
	// src.
	AddFieldTo(schema, src *recordmanager.Schema) error
}
