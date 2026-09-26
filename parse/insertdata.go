package parse

import (
	"errors"
	"slices"

	"github.com/JunNishimura/GoSQL/query"
)

// ErrFieldValueCountMismatch reports an insert that lists a different number
// of values than of fields.
//
// It is kept apart from ErrBadSyntax because the statement follows the grammar:
// each list is well formed on its own. What is wrong is that the two do not
// pair up, since which value goes to which field is settled by position, and a
// value with no field or a field with no value has nowhere to be written.
var ErrFieldValueCountMismatch = errors.New("field and value counts differ")

var _ UpdateCommand = InsertData{}

// InsertData is what an insert statement asks for: a record written to a
// table, with each value going to the field in the same position.
//
// The fields and values are handed out as copies, as the fields and tables of
// QueryData are.
type InsertData struct {
	tableName string
	fields    []string
	vals      []query.Constant
}

func (InsertData) isUpdateCommand() {}

// TableName is the table the record is written to.
func (d InsertData) TableName() string {
	return d.tableName
}

// Fields are the fields the values are written to, in the order written.
func (d InsertData) Fields() []string {
	return slices.Clone(d.fields)
}

// Values are the values written, each to the field at the same position of
// Fields.
func (d InsertData) Values() []query.Constant {
	return slices.Clone(d.vals)
}
