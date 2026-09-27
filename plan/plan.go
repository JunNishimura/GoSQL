package plan

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Plan is one operator of a query, described rather than run: what it would
// cost to read, how many records it would give back, and what fields they
// would have.
//
// A planner weighs plans against each other by these numbers before any of
// them touches a record, which is why a plan and the scan it opens are two
// things. Building a plan reads statistics; only Open reads the data.
type Plan interface {
	// Open returns a scan that reads what the plan describes.
	Open() (query.Scan, error)
	// BlocksAccessed is a guess at how many blocks reading the plan's output
	// touches.
	BlocksAccessed() int
	// RecordsOutput is a guess at how many records the plan gives back.
	RecordsOutput() int
	// DistinctValues is a guess at how many different values fieldName holds
	// in the plan's output.
	DistinctValues(fieldName string) int
	// Schema is the fields of the records the plan gives back.
	Schema() *recordmanager.Schema
}
