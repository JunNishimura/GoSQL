package metadatamanager

// distinctValuesDivisor is the divisor in the estimate of the distinct values
// of a field: the record count is divided by it. The estimate assumes that
// each value of a field occurs about this many times.
//
// The number is not measured. It replaces a count that nothing maintains, and
// it is the same for every field of every table.
const distinctValuesDivisor = 3

// TableStatistics holds the figures that a planner uses to estimate the cost
// of a query on one table: the number of blocks a scan reads, the number of
// records it returns, and the number of distinct values of a field.
//
// The figures do not have to be exact. A plan chosen from stale figures can
// be slower than the best plan, but it does not give a wrong result. It is
// therefore acceptable to keep the figures in memory and let them become
// stale, instead of storing them in a catalog and keeping them exact.
//
// The figures describe the table at the time they were gathered, and nothing
// updates them when records are written. The holder of the statistics decides
// when to gather them again. It weighs the cost of stale figures against the
// cost of a scan of every table.
type TableStatistics struct {
	numBlocks  int
	numRecords int
}

// NewTableStatistics returns the statistics of a table with numBlocks blocks
// and numRecords records.
func NewTableStatistics(numBlocks int, numRecords int) *TableStatistics {
	return &TableStatistics{
		numBlocks:  numBlocks,
		numRecords: numRecords,
	}
}

// BlocksAccessed returns the number of blocks that a scan of the whole table
// reads.
//
// This is the number of blocks of the table, because a table scan reads all
// of them. The method is named after the cost and not after the count,
// because a planner compares plans by the blocks they read.
func (stats *TableStatistics) BlocksAccessed() int {
	return stats.numBlocks
}

// RecordsOutput returns the number of records that a scan of the whole table
// returns.
func (stats *TableStatistics) RecordsOutput() int {
	return stats.numRecords
}

// DistinctValues returns an estimate of the number of distinct values of
// fieldName.
//
// The estimate is crude. The field name is not used, so every field of a
// table gets the same estimate. Nothing here knows the contents of a field,
// and measuring them would require a scan of the table for each field. That
// would cost more than the improved plans would save.
//
// For a table with no records the estimate is still one. A query divides by
// this value to estimate how many records a condition on the field keeps, and
// an empty table must not cause a division by zero.
//
// The method takes the field name although it does not use it. A caller then
// asks the question it means, and a later version that does use the field can
// answer it without a change at any call site.
func (stats *TableStatistics) DistinctValues(fieldName string) int {
	return 1 + stats.numRecords/distinctValuesDivisor
}
