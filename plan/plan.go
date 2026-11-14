package plan

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Plan describes one operator of a query without running it: its estimated
// cost, the estimated number of records it returns, and the fields of those
// records.
//
// A planner compares plans by these estimates before any plan reads a record.
// This is why a plan and the scan it opens are separate types. Creating a
// plan reads only statistics, and only Open reads the data.
type Plan interface {
	// Open returns a scan that reads the output of the plan.
	Open() (query.Scan, error)
	// BlocksAccessed returns the estimated number of blocks that a read of
	// the plan's output accesses.
	BlocksAccessed() int
	// RecordsOutput returns the estimated number of records in the output.
	RecordsOutput() int
	// DistinctValues returns the estimated number of distinct values of
	// fieldName in the output.
	DistinctValues(fieldName string) int
	// Schema returns the fields of the records in the output.
	Schema() *recordmanager.Schema
}
