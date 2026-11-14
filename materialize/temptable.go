package materialize

import (
	"strconv"
	"sync/atomic"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// lastTableNum is the number of the most recently created temp table. All
// transactions share it, because they all write to the same directory.
var lastTableNum atomic.Int64

// TempTable is a table that an operator writes its input to. The operator can
// then read the records again, in its own order, without computing them
// again.
//
// A temp table is in no catalog. Only the operator that created it knows its
// name, and it is not meant to outlive the query.
type TempTable struct {
	tx        *transaction.Transaction
	tableName string
	layout    *recordmanager.Layout
}

// NewTempTable returns a temp table with the given schema and a unique name.
//
// It writes nothing. The first scan opened on the table creates its file, as
// for a stored table.
func NewTempTable(tx *transaction.Transaction, schema *recordmanager.Schema) *TempTable {
	return &TempTable{
		tx:        tx,
		tableName: filemanager.TempFilePrefix + strconv.FormatInt(lastTableNum.Add(1), 10),
		layout:    recordmanager.NewLayout(schema),
	}
}

// Open returns a scan of the table that can write records as well as read
// them.
func (tt *TempTable) Open() (query.UpdateScan, error) {
	return query.NewTableScan(tt.tx, tt.tableName, tt.layout)
}

// TableName returns the name of the table.
func (tt *TempTable) TableName() string {
	return tt.tableName
}

// Layout returns the layout of the table's records.
func (tt *TempTable) Layout() *recordmanager.Layout {
	return tt.layout
}
