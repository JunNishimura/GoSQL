package plan

import (
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*TablePlan)(nil)

// TablePlan is the plan for a full scan of a stored table.
//
// Its estimates are the statistics of the table at the time the plan was
// created. They are read once, in the constructor, and not on every call. A
// plan therefore gives the same answers however often a planner asks.
type TablePlan struct {
	tx        *transaction.Transaction
	tableName string
	layout    *recordmanager.Layout
	stats     *metadatamanager.TableStatistics
}

// NewTablePlan returns the plan for a scan of tableName. It reads the layout
// and the statistics of the table through mm.
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

// Open returns a table scan of the table.
//
// The scan is returned as a query.Scan to satisfy Plan. Its dynamic type also
// implements query.UpdateScan, and an update planner asserts that type before
// it writes through the scan.
func (tp *TablePlan) Open() (query.Scan, error) {
	return query.NewTableScan(tp.tx, tp.tableName, tp.layout)
}

// BlocksAccessed returns the number of blocks of the table.
func (tp *TablePlan) BlocksAccessed() int {
	return tp.stats.BlocksAccessed()
}

// RecordsOutput returns the number of records in the table.
func (tp *TablePlan) RecordsOutput() int {
	return tp.stats.RecordsOutput()
}

// DistinctValues returns the statistics' estimate of the number of distinct
// values of fieldName in the table.
func (tp *TablePlan) DistinctValues(fieldName string) int {
	return tp.stats.DistinctValues(fieldName)
}

// Schema returns the schema that the table was created with.
func (tp *TablePlan) Schema() *recordmanager.Schema {
	return tp.layout.Schema()
}
