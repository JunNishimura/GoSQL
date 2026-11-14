package parse

import "github.com/JunNishimura/GoSQL/query"

var _ UpdateCommand = ModifyData{}

// ModifyData is the content of an update statement: one field to set to a new
// value, in the records of a table that satisfy a condition.
//
// It is not named UpdateData, because UpdateCommand already uses "update" for
// every statement that changes the database, and this is only one of them.
type ModifyData struct {
	tableName string
	fieldName string
	newValue  query.Expression
	pred      query.Predicate
}

func (ModifyData) isUpdateCommand() {}

// TableName returns the table whose records are modified.
func (d ModifyData) TableName() string {
	return d.tableName
}

// TargetField returns the field that is set.
func (d ModifyData) TargetField() string {
	return d.fieldName
}

// NewValue returns the value that the field is set to. It is an expression
// and not a constant, so it can be the value of another field of the same
// record.
func (d ModifyData) NewValue() query.Expression {
	return d.newValue
}

// Predicate returns the condition that a record must satisfy to be modified.
// An update with no where clause has a predicate with no terms, which every
// record satisfies.
func (d ModifyData) Predicate() query.Predicate {
	return d.pred
}
