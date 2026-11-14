package parse

import (
	"errors"
	"slices"

	"github.com/JunNishimura/GoSQL/query"
)

// ErrFieldValueCountMismatch is returned for an insert whose value list and
// field list have different lengths.
//
// It is separate from ErrBadSyntax because the statement is grammatical: each
// list is well formed on its own. The problem is that the lists do not match.
// Values are assigned to fields by position, so a value without a field, or a
// field without a value, cannot be written.
var ErrFieldValueCountMismatch = errors.New("field and value counts differ")

var _ UpdateCommand = InsertData{}

// InsertData is the content of an insert statement: a record to write to a
// table, with each value assigned to the field at the same position.
//
// The fields and values are returned as copies, like the fields and tables of
// QueryData.
type InsertData struct {
	tableName string
	fields    []string
	vals      []query.Constant
}

func (InsertData) isUpdateCommand() {}

// TableName returns the table that the record is written to.
func (d InsertData) TableName() string {
	return d.tableName
}

// Fields returns the fields that the values are written to, in the order
// written.
func (d InsertData) Fields() []string {
	return slices.Clone(d.fields)
}

// Values returns the values to write. Each value belongs to the field at the
// same position in Fields.
func (d InsertData) Values() []query.Constant {
	return slices.Clone(d.vals)
}
