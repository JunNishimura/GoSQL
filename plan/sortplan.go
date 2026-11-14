package plan

import (
	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// maxSortScanRuns is the number of runs at which a sort stops merging into
// temp tables. The scan that the sort opens merges that many runs while it is
// read.
const maxSortScanRuns = 2

var _ Plan = (*SortPlan)(nil)

// SortPlan is the plan that returns the records of another plan in the order
// of a list of fields. It uses merge sort.
//
// Open performs the sort. The records are split into runs, which are
// sequences that are already sorted, and each run is written to a temp table.
// The runs are merged in pairs into new temp tables until at most two remain.
// The returned scan merges those two while it is read, so the result of the
// last merge is never written.
type SortPlan struct {
	tx         *transaction.Transaction
	src        Plan
	sortFields []string
	comp       *materialize.RecordComparator
}

// NewSortPlan returns the plan that sorts the records of src by sortFields:
// by the first field, then by the second field for records with equal first
// fields, and so on.
func NewSortPlan(tx *transaction.Transaction, src Plan, sortFields ...string) *SortPlan {
	return &SortPlan{
		tx:         tx,
		src:        src,
		sortFields: sortFields,
		comp:       materialize.NewRecordComparator(sortFields...),
	}
}

// Open sorts the records of src and returns a scan of them in order,
// positioned before the first record.
//
// src is read once, while it is split into runs, and closed before the runs
// are merged. The returned scan therefore holds only the last runs.
func (sp *SortPlan) Open() (query.Scan, error) {
	ss, err := sp.openSortScan()
	if err != nil {
		// A nil *SortScan returned directly would become a non-nil query.Scan
		// interface value.
		return nil, err
	}

	return ss, nil
}

// openSortScan does the work of Open and returns the scan with its concrete
// type. A merge join needs that type to restore the position of its right
// side.
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

// splitIntoRuns writes the remaining records of src to temp tables. It starts
// a new table whenever a record is ordered before the previous record, so
// that each table holds one sorted run.
//
// The result always has at least one run. A src with no records gives one
// empty run, which a sort scan reads as no records. With zero runs the sort
// scan would have nothing to open.
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
// and returns the new runs. With an odd number of runs, the last run is
// passed on unchanged.
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

// mergeTwoRuns writes the records of two runs to a new run, in sorted order.
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
		// The record of the first run is taken unless the second run has a
		// record that is ordered before it. Records with equal sort fields
		// therefore keep the order of their runs.
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

// BlocksAccessed returns the blocks of one read of the sorted records. This
// is the number of blocks of their materialization: the last runs hold every
// record, stored like a temp table with the schema of src.
//
// The cost of the sort is not included. The sort runs once, when the plan is
// opened. This estimate is the cost of one read of the plan, which an
// operator above can repeat many times. The sort cost could not be estimated
// well anyway. The number of merges depends on the number of runs, which
// depends on the existing order of the records, and no statistic describes
// that order.
func (sp *SortPlan) BlocksAccessed() int {
	return NewMaterializePlan(sp.tx, sp.src).BlocksAccessed()
}

// RecordsOutput returns the records of src.
func (sp *SortPlan) RecordsOutput() int {
	return sp.src.RecordsOutput()
}

// DistinctValues returns the distinct values of fieldName in src.
func (sp *SortPlan) DistinctValues(fieldName string) int {
	return sp.src.DistinctValues(fieldName)
}

// Schema returns the schema of src.
func (sp *SortPlan) Schema() *recordmanager.Schema {
	return sp.src.Schema()
}
