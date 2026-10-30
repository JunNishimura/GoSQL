package materialize

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
)

// maxRuns is how many runs a sort scan merges. A sort merges its runs in
// pairs until this many are left, and the scan does the last merge as it is
// read, rather than writing it out to be read again.
const maxRuns = 2

// noCurrentRun is the index of the current run when the scan is on no record.
const noCurrentRun = -1

var _ query.Scan = (*SortScan)(nil)

// SortScan reads the records of one or two sorted runs in order, merging the
// two as it goes.
//
// Each run is sorted already, so the next record of the whole is whichever of
// the runs' next records comes first. The scan keeps every run on its next
// record and hands out the earlier of them, moving on only the run it came
// from.
type SortScan struct {
	runs []query.UpdateScan
	// hasMore says, for each run, whether it is on a record rather than past
	// its last.
	hasMore []bool
	// current is the index of the run whose record the scan is on, or
	// noCurrentRun.
	current int
	comp    *RecordComparator
}

// NewSortScan opens the runs and returns a scan of their records in the order
// comp puts them, before the first.
//
// There have to be one or two runs. A sort of no records still has a run, an
// empty one, so no runs at all is a mistake of the caller, as is more than
// the scan merges. Whatever was opened before a failure is closed again, since
// no scan comes back for the caller to close.
func NewSortScan(runs []*TempTable, comp *RecordComparator) (*SortScan, error) {
	if len(runs) == 0 || len(runs) > maxRuns {
		return nil, fmt.Errorf("open a sort scan over %d runs: it merges one to %d", len(runs), maxRuns)
	}

	ss := &SortScan{
		hasMore: make([]bool, len(runs)),
		comp:    comp,
	}
	for _, run := range runs {
		s, err := run.Open()
		if err != nil {
			ss.Close()

			return nil, err
		}
		ss.runs = append(ss.runs, s)
	}

	if err := ss.MoveBeforeFirstRecord(); err != nil {
		ss.Close()

		return nil, err
	}

	return ss, nil
}

// MoveBeforeFirstRecord puts every run back on its first record, so that the
// next move hands out the earliest of them.
func (ss *SortScan) MoveBeforeFirstRecord() error {
	ss.current = noCurrentRun
	for i, s := range ss.runs {
		if err := s.MoveBeforeFirstRecord(); err != nil {
			return err
		}
		ok, err := s.MoveToNextRecord()
		if err != nil {
			return err
		}
		ss.hasMore[i] = ok
	}

	return nil
}

// MoveToNextRecord moves on to the next record in order and reports whether
// there was one.
//
// The run the last record came from is moved on to its next, and the earliest
// record among the runs that still have one becomes the current. Of records
// that compare equal, the one of the earlier run comes first.
func (ss *SortScan) MoveToNextRecord() (bool, error) {
	if ss.current != noCurrentRun {
		ok, err := ss.runs[ss.current].MoveToNextRecord()
		if err != nil {
			return false, err
		}
		ss.hasMore[ss.current] = ok
	}

	ss.current = noCurrentRun
	for i, s := range ss.runs {
		if !ss.hasMore[i] {
			continue
		}
		if ss.current == noCurrentRun {
			ss.current = i
			continue
		}

		c, err := ss.comp.Compare(s, ss.runs[ss.current])
		if err != nil {
			return false, err
		}
		if c < 0 {
			ss.current = i
		}
	}

	return ss.current != noCurrentRun, nil
}

// currentRun is the run whose record the scan is on.
func (ss *SortScan) currentRun() (query.Scan, error) {
	if ss.current == noCurrentRun {
		return nil, fmt.Errorf("read a sort scan: %w", query.ErrNoCurrentRecord)
	}

	return ss.runs[ss.current], nil
}

// GetInt reads fieldName of the current record.
func (ss *SortScan) GetInt(fieldName string) (int32, error) {
	s, err := ss.currentRun()
	if err != nil {
		return 0, err
	}

	return s.GetInt(fieldName)
}

// GetString reads fieldName of the current record.
func (ss *SortScan) GetString(fieldName string) (string, error) {
	s, err := ss.currentRun()
	if err != nil {
		return "", err
	}

	return s.GetString(fieldName)
}

// GetValue reads fieldName of the current record.
func (ss *SortScan) GetValue(fieldName string) (query.Constant, error) {
	s, err := ss.currentRun()
	if err != nil {
		return query.Constant{}, err
	}

	return s.GetValue(fieldName)
}

// HasField reports whether the runs have a field called fieldName.
//
// Every run has the fields of what was sorted, so the first is asked. That
// answers before any record has been read, which asking the current run would
// not.
func (ss *SortScan) HasField(fieldName string) bool {
	return ss.runs[0].HasField(fieldName)
}

// Close closes the runs.
func (ss *SortScan) Close() {
	for _, s := range ss.runs {
		s.Close()
	}
}
