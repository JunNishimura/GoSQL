package materialize

import (
	"strconv"
	"sync/atomic"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// tempTablePrefix starts the name of every temp table.
//
// It begins with '#', which the tokenizer never puts in a word, so no statement
// can name a table that starts with it: a user's table cannot share a temp
// table's file, and nothing but a temp table is ever named this way.
const tempTablePrefix = "#temp"

// lastTableNum is the number given to the temp table made most recently. It is
// shared by every transaction, since they all write to the one directory.
var lastTableNum atomic.Int64

// TempTable is a table an operator writes its input into so that it can read
// the records again, in an order of its own, without computing them again.
//
// It is in no catalog: nothing but the operator that made it knows its name,
// and it is not meant to outlive the query.
type TempTable struct {
	tx        *transaction.Transaction
	tableName string
	layout    *recordmanager.Layout
}

// NewTempTable returns a temp table of the given schema, under a name no other
// table has.
//
// Nothing is written here. The table's file is made by the first scan opened
// over it, as a stored table's is.
func NewTempTable(tx *transaction.Transaction, schema *recordmanager.Schema) *TempTable {
	return &TempTable{
		tx:        tx,
		tableName: tempTablePrefix + strconv.FormatInt(lastTableNum.Add(1), 10),
		layout:    recordmanager.NewLayout(schema),
	}
}

// Open returns a scan over the table, for writing its records as well as
// reading them.
func (tt *TempTable) Open() (query.UpdateScan, error) {
	return query.NewTableScan(tt.tx, tt.tableName, tt.layout)
}

// TableName is the name the table was given.
func (tt *TempTable) TableName() string {
	return tt.tableName
}

// Layout is the layout of the table's records.
func (tt *TempTable) Layout() *recordmanager.Layout {
	return tt.layout
}
