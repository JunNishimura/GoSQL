package embedded

import (
	"database/sql/driver"
	"fmt"
	"io"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Rows reads the records that a query returns.
type Rows struct {
	scan    query.Scan
	columns []string
	schema  *recordmanager.Schema
	// autoTx is the transaction of a query that was run outside an open
	// transaction of the connection. It is committed when the rows are closed.
	// It is nil for a query that runs in the connection's open transaction.
	autoTx *transaction.Transaction
}

var (
	_ driver.Rows                           = (*Rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
	_ driver.RowsColumnTypeLength           = (*Rows)(nil)
)

// Columns returns the field names of the records, in the order of the select
// list.
func (r *Rows) Columns() []string {
	return r.columns
}

// ColumnTypeDatabaseTypeName returns the type name of the column at index:
// INT or VARCHAR.
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	// The columns are fields of the schema, so this lookup cannot fail.
	fieldType, _ := r.schema.Type(r.columns[index])
	switch fieldType {
	case recordmanager.FieldTypeInt:
		return "INT"
	case recordmanager.FieldTypeVarchar:
		return "VARCHAR"
	default:
		return ""
	}
}

// ColumnTypeLength returns the character limit of the column at index if it
// is a varchar. For an int, which has no length, ok is false.
func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	// The columns are fields of the schema, so this lookup cannot fail.
	if fieldType, _ := r.schema.Type(r.columns[index]); fieldType != recordmanager.FieldTypeVarchar {
		return 0, false
	}
	n, _ := r.schema.Length(r.columns[index])

	return int64(n), true
}

// Next writes the fields of the next record to dest: an int field as an int64
// and a varchar field as a string. It returns io.EOF after the last record.
func (r *Rows) Next(dest []driver.Value) error {
	ok, err := r.scan.MoveToNextRecord()
	if err != nil {
		return err
	}
	if !ok {
		return io.EOF
	}

	for i, column := range r.columns {
		val, err := r.scan.GetValue(column)
		if err != nil {
			return err
		}
		dest[i], err = driverValue(val)
		if err != nil {
			return fmt.Errorf("read column %s: %w", column, err)
		}
	}

	return nil
}

// driverValue converts a constant to a value type that database/sql accepts.
func driverValue(val query.Constant) (driver.Value, error) {
	switch val.Type() {
	case recordmanager.FieldTypeInt:
		n, err := val.AsInt()
		if err != nil {
			return nil, err
		}
		return int64(n), nil
	case recordmanager.FieldTypeVarchar:
		return val.AsString()
	default:
		return nil, fmt.Errorf("no driver value for a constant of type %s", val.Type())
	}
}

// Close closes the scan. If the query ran in a transaction of its own, Close
// also commits that transaction.
//
// The transaction is cleared after the commit, so a second Close does not
// commit it again.
func (r *Rows) Close() error {
	r.scan.Close()
	if r.autoTx == nil {
		return nil
	}

	if err := r.autoTx.Commit(); err != nil {
		return err
	}
	r.autoTx = nil

	return nil
}
