package parse

import "github.com/JunNishimura/GoSQL/query"

var _ UpdateCommand = ModifyData{}

// ModifyData is what an update statement asks for: one field of the records of
// a table that meet a condition, set to a new value.
//
// It is not called UpdateData because UpdateCommand already uses "update" for
// every statement that changes the database, of which this is one.
type ModifyData struct {
	tableName string
	fieldName string
	newValue  query.Expression
	pred      query.Predicate
}

func (ModifyData) isUpdateCommand() {}

// TableName is the table whose records are changed.
func (d ModifyData) TableName() string {
	return d.tableName
}

// TargetField is the field that is set.
func (d ModifyData) TargetField() string {
	return d.fieldName
}

// NewValue is what the field is set to. It is an expression rather than a
// constant, so it can be what another field of the same record holds.
func (d ModifyData) NewValue() query.Expression {
	return d.newValue
}

// Predicate is the condition a record has to meet to be changed. An update
// with no where clause has a predicate of no terms, which every record meets.
func (d ModifyData) Predicate() query.Predicate {
	return d.pred
}
