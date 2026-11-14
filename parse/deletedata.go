package parse

import "github.com/JunNishimura/GoSQL/query"

var _ UpdateCommand = DeleteData{}

// DeleteData is the content of a delete statement: a table, and the predicate
// that selects the records to delete.
type DeleteData struct {
	tableName string
	pred      query.Predicate
}

func (DeleteData) isUpdateCommand() {}

// TableName returns the table that the records are deleted from.
func (d DeleteData) TableName() string {
	return d.tableName
}

// Predicate returns the condition that a record must satisfy to be deleted. A
// delete with no where clause has a predicate with no terms, which every
// record satisfies.
func (d DeleteData) Predicate() query.Predicate {
	return d.pred
}
