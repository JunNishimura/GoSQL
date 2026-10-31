package plan

import (
	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// maxSortScanRuns is how many runs a sort merges into temp tables down to, and
// so how many the scan it opens merges as it is read.
const maxSortScanRuns = 2

var _ Plan = (*SortPlan)(nil)

// SortPlan is the plan of reading the records of another plan in order of a
// list of fields, by merge sort.
//
// Opening it does the sorting: the records are split into runs, stretches
// already in order, each written to a temp table, and the runs are merged in
// pairs into new temp tables until no more than two are left. The scan it
// returns merges those two as it is read, so the last merge is never written
// out.
type SortPlan struct {
	tx         *transaction.Transaction
	src        Plan
	sortFields []string
	comp       *materialize.RecordComparator
}

// NewSortPlan returns the plan of sorting the records of src by sortFields:
// by the first, then by the second among records the first does not tell
// apart, and so on.
func NewSortPlan(tx *transaction.Transaction, src Plan, sortFields ...string) *SortPlan {
	return &SortPlan{
		tx:         tx,
		src:        src,
		sortFields: sortFields,
		comp:       materialize.NewRecordComparator(sortFields...),
	}
}

// Open sorts the records of src and returns a scan of them in order, before
// the first.
//
// src is read once, while it is split into runs, and closed before the runs
// are merged, so the scan handed back holds only the last runs.
func (sp *SortPlan) Open() (query.Scan, error) {
	ss, err := sp.openSortScan()
	if err != nil {
		// Returned as it is, a nil *SortScan would be a query.Scan that is not
		// nil.
		return nil, err
	}

	return ss, nil
}

// openSortScan is Open, returning the scan as the sort scan it is, for a
// merge join, which puts the scan of its right side back to where it was.
func (sp *SortPlan) openSortScan() (*materialize.SortScan, error) {
	src, err := sp.src.Open()
	if err != nil {
		return nil, err
	}
	runs, err := sp.splitIntoRuns(src)
	src.Close()
	if err != nil {
		return nil, err
	}

	for len(runs) > maxSortScanRuns {
		runs, err = sp.mergeRunsInPairs(runs)
		if err != nil {
			return nil, err
		}
	}

	return materialize.NewSortScan(runs, sp.comp)
}

// splitIntoRuns writes the records src has left into temp tables, starting a
// new one wherever a record comes before the one written just before it, so
// that each table holds a run of records in order.
//
// There is always at least one run. A src with no records gives one empty run,
// which a sort scan reads as no records, rather than none for it to read.
func (sp *SortPlan) splitIntoRuns(src query.Scan) ([]*materialize.TempTable, error) {
	run := materialize.NewTempTable(sp.tx, sp.src.Schema())
	runs := []*materialize.TempTable{run}
	dest, err := run.Open()
	if err != nil {
		return nil, err
	}
	defer func() {
		if dest != nil {
			dest.Close()
		}
	}()

	fieldNames := sp.src.Schema().Fields()
	written := false
	for {
		ok, err := src.MoveToNextRecord()
		if err != nil {
			return nil, err
		}
		if !ok {
			return runs, nil
		}

		if written {
			c, err := sp.comp.Compare(src, dest)
			if err != nil {
				return nil, err
			}
			if c < 0 {
				dest.Close()
				dest = nil

				run = materialize.NewTempTable(sp.tx, sp.src.Schema())
				runs = append(runs, run)
				if dest, err = run.Open(); err != nil {
					return nil, err
				}
			}
		}

		if err := copyRecord(src, dest, fieldNames); err != nil {
			return nil, err
		}
		written = true
	}
}

// mergeRunsInPairs merges the runs two at a time, each pair into one new run,
// and returns the new runs. A run left over from an odd number is passed on as
// it is.
func (sp *SortPlan) mergeRunsInPairs(runs []*materialize.TempTable) ([]*materialize.TempTable, error) {
	merged := []*materialize.TempTable{}
	for len(runs) >= 2 {
		run, err := sp.mergeTwoRuns(runs[0], runs[1])
		if err != nil {
			return nil, err
		}
		merged = append(merged, run)
		runs = runs[2:]
	}

	return append(merged, runs...), nil
}

// mergeTwoRuns writes the records of two runs into a new run in order.
func (sp *SortPlan) mergeTwoRuns(run1, run2 *materialize.TempTable) (*materialize.TempTable, error) {
	s1, err := run1.Open()
	if err != nil {
		return nil, err
	}
	defer s1.Close()

	s2, err := run2.Open()
	if err != nil {
		return nil, err
	}
	defer s2.Close()

	result := materialize.NewTempTable(sp.tx, sp.src.Schema())
	dest, err := result.Open()
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	fieldNames := sp.src.Schema().Fields()

	hasMore1, err := s1.MoveToNextRecord()
	if err != nil {
		return nil, err
	}
	hasMore2, err := s2.MoveToNextRecord()
	if err != nil {
		return nil, err
	}

	for hasMore1 || hasMore2 {
		// The record of the first run goes first unless the second has one
		// that comes before it, so records of equal fields keep the order of
		// their runs.
		takeFirst := hasMore1
		if hasMore1 && hasMore2 {
			c, err := sp.comp.Compare(s1, s2)
			if err != nil {
				return nil, err
			}
			takeFirst = c <= 0
		}

		if takeFirst {
			if err := copyRecord(s1, dest, fieldNames); err != nil {
				return nil, err
			}
			if hasMore1, err = s1.MoveToNextRecord(); err != nil {
				return nil, err
			}
		} else {
			if err := copyRecord(s2, dest, fieldNames); err != nil {
				return nil, err
			}
			if hasMore2, err = s2.MoveToNextRecord(); err != nil {
				return nil, err
			}
		}
	}

	return result, nil
}

// BlocksAccessed is the blocks of reading the sorted records once, which is
// the blocks their materialization takes up: the last runs hold every record,
// laid out as a temp table of src's schema would.
//
// Sorting them is left out. It is done once, when the plan is opened, while
// this is the cost of reading the plan, which an operator above may do many
// times over. It could not be counted well anyway: how many merges a sort
// takes depends on how many runs the records split into, which depends on the
// order they are in, and no statistic says that.
func (sp *SortPlan) BlocksAccessed() int {
	return NewMaterializePlan(sp.tx, sp.src).BlocksAccessed()
}

// RecordsOutput is the records of src.
func (sp *SortPlan) RecordsOutput() int {
	return sp.src.RecordsOutput()
}

// DistinctValues is the distinct values of fieldName in src.
func (sp *SortPlan) DistinctValues(fieldName string) int {
	return sp.src.DistinctValues(fieldName)
}

// Schema is the schema of src.
func (sp *SortPlan) Schema() *recordmanager.Schema {
	return sp.src.Schema()
}
