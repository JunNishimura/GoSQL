package metadatamanager

// distinctValuesDivisor is what the guess at a field's distinct values divides
// the record count by: a field is taken to repeat each of its values about this
// many times.
//
// The number is not measured from anything. It stands in for a count nobody
// keeps, and it is the same for every field of every table.
const distinctValuesDivisor = 3

// TableStatistics is what a planner is told about one table when it works out
// what a query will cost: how many blocks reading it touches, how many records
// come out, and how many different values a field holds.
//
// None of it has to be right. A plan chosen from numbers that are out of date
// is a plan that may be slower than the best one, not a plan that gives a wrong
// answer, and that is what makes it worth keeping these in memory and letting
// them go stale rather than writing them to a catalog and keeping them true.
//
// The counts are of the table as it was when they were gathered, and nothing
// updates them as records are written. Deciding when to gather them again is
// the job of whatever holds them, which is where the cost of being out of date
// is weighed against the cost of reading every table over.
type TableStatistics struct {
	numBlocks  int
	numRecords int
}

// NewTableStatistics returns what is known about a table of numBlocks blocks holding
// numRecords records.
func NewTableStatistics(numBlocks int, numRecords int) *TableStatistics {
	return &TableStatistics{
		numBlocks:  numBlocks,
		numRecords: numRecords,
	}
}

// BlocksAccessed is how many blocks reading the whole table touches.
//
// It is the number of blocks the table takes up, since a scan of a table reads
// every one of them. It is named for what it costs rather than for what it
// counts, because what a planner compares plans by is the reading.
func (stats *TableStatistics) BlocksAccessed() int {
	return stats.numBlocks
}

// RecordsOutput is how many records reading the whole table gives back.
func (stats *TableStatistics) RecordsOutput() int {
	return stats.numRecords
}

// DistinctValues is a guess at how many different values fieldName holds.
//
// It is only a guess, and a crude one: the field is named but not looked at, so
// every field of a table is guessed the same. Nothing here knows what is in a
// field, and finding out would mean reading the table one field at a time,
// which costs more than the plans it would improve are worth.
//
// A table with no records still guesses one. A query divides by this to work
// out how many records a condition on the field leaves, and none of the tables
// it has to weigh should be the one that makes that a division by zero.
//
// The field is named in the call even though it goes unread, so that a caller
// asks the question it means, and so that a later version that does look can
// answer it without every call site changing.
func (stats *TableStatistics) DistinctValues(fieldName string) int {
	return 1 + stats.numRecords/distinctValuesDivisor
}
