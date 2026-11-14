package parse

import (
	"errors"
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ErrInvalidVarcharLength is returned for a varchar field that is declared
// with a negative length.
//
// It is separate from ErrBadSyntax because the statement is grammatical: the
// grammar allows any int as the length. The value is the problem. No string
// has fewer than zero characters, and a layout with a negative length would
// give the field a negative size in a record.
var ErrInvalidVarcharLength = errors.New("invalid varchar length")

var _ UpdateCommand = CreateTableData{}

// CreateTableData is the content of a create table statement: a table with
// the given name, whose records have the fields of the schema.
type CreateTableData struct {
	tableName string
	schema    *recordmanager.Schema
}

func (CreateTableData) isUpdateCommand() {}

// TableName returns the name of the table to create.
func (d CreateTableData) TableName() string {
	return d.tableName
}

// Schema returns the fields of the table, in the order written.
//
// The result is a copy, like the fields and tables of QueryData. A schema is
// modified through AddField, so returning the stored schema would let a
// caller change the table that this statement creates.
func (d CreateTableData) Schema() *recordmanager.Schema {
	copied := recordmanager.NewSchema()
	if err := copied.AddAll(d.schema); err != nil {
		// A schema with no fields has no name that a copied field can
		// duplicate, so AddAll cannot fail.
		panic(fmt.Sprintf("copy the schema of table %s: %v", d.tableName, err))
	}

	return copied
}
