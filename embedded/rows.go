package embedded

import (
	"database/sql/driver"
	"fmt"
	"io"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Rows reads the records a query gives back.
type Rows struct {
	scan    query.Scan
	columns []string
	schema  *recordmanager.Schema
	// autoTx is the transaction the query runs in when it was made outside one
	// begun on the connection, to be committed when the rows are closed. It is
	// nil for a query run in a transaction begun on the connection.
	autoTx *transaction.Transaction
}

var (
	_ driver.Rows                           = (*Rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
	_ driver.RowsColumnTypeLength           = (*Rows)(nil)
)

// Columns names the fields of the records, in the order of the select list.
func (r *Rows) Columns() []string {
	return r.columns
}

// ColumnTypeDatabaseTypeName names the type of the column at index, as INT or
// VARCHAR.
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	// The columns are the schema's own fields, so asking it about one cannot
	// fail.
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

// ColumnTypeLength reports the character limit of the column at index if it is
// a varchar, and ok as false if it is an int, which has no length.
func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	// The columns are the schema's own fields, so asking it about one cannot
	// fail.
	if fieldType, _ := r.schema.Type(r.columns[index]); fieldType != recordmanager.FieldTypeVarchar {
		return 0, false
	}
	n, _ := r.schema.Length(r.columns[index])

	return int64(n), true
}

// Next puts the fields of the next record into dest, an int field as an int64
// and a varchar field as a string, and reports io.EOF past the last record.
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

// driverValue turns a constant into the kind of value database/sql takes.
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

// Close closes the scan, and commits the transaction the query ran in if it was
// one of its own.
//
// The transaction is let go of once committed, so closing the rows again does
// not commit it twice.
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
