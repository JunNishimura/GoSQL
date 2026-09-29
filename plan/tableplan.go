package plan

import (
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*TablePlan)(nil)

// TablePlan is the plan of reading a stored table from start to end.
//
// Its numbers are the table's statistics as they stood when the plan was
// made. They are read once, here, rather than at every question, so that one
// plan answers alike however many times a planner asks it.
type TablePlan struct {
	tx        *transaction.Transaction
	tableName string
	layout    *recordmanager.Layout
	stats     *metadatamanager.TableStatistics
}

// NewTablePlan returns the plan of reading tableName, reading its layout and
// statistics through mm.
func NewTablePlan(tx *transaction.Transaction, tableName string, mm *metadatamanager.MetadataManager) (*TablePlan, error) {
	layout, err := mm.GetLayout(tx, tableName)
	if err != nil {
		return nil, err
	}

	stats, err := mm.GetStatistics(tx, tableName)
	if err != nil {
		return nil, err
	}

	return &TablePlan{
		tx:        tx,
		tableName: tableName,
		layout:    layout,
		stats:     stats,
	}, nil
}

// Open returns a table scan over the table.
//
// The scan is returned as a query.Scan to satisfy Plan, but it is a
// query.UpdateScan underneath, which is what an update planner asserts it to
// before writing through it.
func (tp *TablePlan) Open() (query.Scan, error) {
	return query.NewTableScan(tp.tx, tp.tableName, tp.layout)
}

// BlocksAccessed is the number of blocks the table takes up.
func (tp *TablePlan) BlocksAccessed() int {
	return tp.stats.BlocksAccessed()
}

// RecordsOutput is the number of records the table holds.
func (tp *TablePlan) RecordsOutput() int {
	return tp.stats.RecordsOutput()
}

// DistinctValues is the statistics' guess at how many different values
// fieldName holds in the table.
func (tp *TablePlan) DistinctValues(fieldName string) int {
	return tp.stats.DistinctValues(fieldName)
}

// Schema is the schema the table was created with.
func (tp *TablePlan) Schema() *recordmanager.Schema {
	return tp.layout.Schema()
}
