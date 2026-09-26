package parse

import (
	"errors"
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ErrInvalidVarcharLength reports a varchar field declared with a negative
// length.
//
// It is kept apart from ErrBadSyntax because the statement follows the grammar,
// which allows any int as the length. What is wrong is the value: no string is
// shorter than no characters, and a layout given a negative length would give
// the field negative room in a record.
var ErrInvalidVarcharLength = errors.New("invalid varchar length")

var _ UpdateCommand = CreateTableData{}

// CreateTableData is what a create table statement asks for: a table of the
// given name, whose records have the fields of the schema.
type CreateTableData struct {
	tableName string
	schema    *recordmanager.Schema
}

func (CreateTableData) isUpdateCommand() {}

// TableName is the table to create.
func (d CreateTableData) TableName() string {
	return d.tableName
}

// Schema is the fields of the table, in the order written.
//
// It is a copy, as the fields and tables of QueryData are. A schema is written
// to through AddField, so handing out the one held here would let a caller
// change the table this statement creates.
func (d CreateTableData) Schema() *recordmanager.Schema {
	copied := recordmanager.NewSchema()
	if err := copied.AddAll(d.schema); err != nil {
		// A schema of no fields has no name for one being copied to clash
		// with, so AddAll has nothing to refuse.
		panic(fmt.Sprintf("copy the schema of table %s: %v", d.tableName, err))
	}

	return copied
}
