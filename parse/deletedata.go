package parse

import "github.com/JunNishimura/GoSQL/query"

var _ UpdateCommand = DeleteData{}

// DeleteData is what a delete statement asks for: the records of a table that
// meet a condition, to be removed.
type DeleteData struct {
	tableName string
	pred      query.Predicate
}

func (DeleteData) isUpdateCommand() {}

// TableName is the table the records are removed from.
func (d DeleteData) TableName() string {
	return d.tableName
}

// Predicate is the condition a record has to meet to be removed. A delete with
// no where clause has a predicate of no terms, which every record meets.
func (d DeleteData) Predicate() query.Predicate {
	return d.pred
}
