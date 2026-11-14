package materialize

import (
	"fmt"
	"slices"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// maxRuns is the number of runs that a sort scan merges. A sort merges its
// runs in pairs until this many remain. The scan performs the last merge
// while it is read, so that the result is not written and read again.
const maxRuns = 2

// noCurrentRun is the index of the current run while the scan is on no
// record.
const noCurrentRun = -1

var _ query.Scan = (*SortScan)(nil)

// SortScan reads the records of one or two sorted runs in order. It merges
// two runs while it reads them.
//
// Each run is already sorted. The next record overall is therefore the
// smaller of the next records of the runs. The scan keeps every run on its
// next record, returns the smaller one, and advances only the run that it
// came from.
type SortScan struct {
	runs []query.UpdateScan
	// hasMore reports for each run whether it is on a record. It is false for
	// a run that is past its last record.
	hasMore []bool
	// current is the index of the run that holds the current record, or
	// noCurrentRun.
	current int
	comp    *RecordComparator
}

// NewSortScan opens the runs and returns a scan of their records in the order
// of comp, positioned before the first record.
//
// The number of runs must be one or two. A sort of no records still has one
// empty run, so zero runs is a caller error. More runs than the scan can
// merge is also a caller error. If an open fails, the scans already opened
// are closed, because no scan is returned that the caller could close.
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

// MoveBeforeFirstRecord positions every run on its first record, so that the
// next move returns the smallest of them.
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

// MoveToNextRecord moves to the next record in order and reports whether one
// exists.
//
// The run that held the last record advances to its next record. The smallest
// record among the runs that still have one becomes the current record. Of
// records that compare equal, the record of the earlier run comes first.
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

// currentRun returns the run that holds the current record.
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

// HasField reports whether the runs have a field named fieldName.
//
// Every run has the fields of the sorted input, so the first run answers.
// That works before any record has been read. The current run could not
// answer then.
func (ss *SortScan) HasField(fieldName string) bool {
	return ss.runs[0].HasField(fieldName)
}

// SortPosition is a saved position of a sort scan, which the scan can be
// restored to.
//
// The current record of each run is not enough to define the position. The
// next read also depends on which run holds the current record and on which
// runs are exhausted. These are saved and restored together with the records.
type SortPosition struct {
	// rids are the record ids of the current records of the runs. An entry is
	// nil for a run that was exhausted.
	rids    []*recordmanager.RecordID
	hasMore []bool
	current int
}

// SavePosition returns the current position of the scan, which
// RestorePosition can restore. A merge join uses it to read the right
// records for a join value again for each left record with that value.
func (ss *SortScan) SavePosition() (*SortPosition, error) {
	rids := make([]*recordmanager.RecordID, len(ss.runs))
	for i, s := range ss.runs {
		// An exhausted run may never have been on a record, as with an empty
		// run. Such a run has no record id to save.
		if !ss.hasMore[i] {
			continue
		}

		rid, err := s.CurrentRecordID()
		if err != nil {
			return nil, err
		}
		rids[i] = rid
	}

	return &SortPosition{
		rids:    rids,
		hasMore: slices.Clone(ss.hasMore),
		current: ss.current,
	}, nil
}

// RestorePosition restores the scan to the position that pos saved.
//
// A run that was exhausted is not repositioned. It is not read again until
// the scan restarts, and a restart rewinds every run.
func (ss *SortScan) RestorePosition(pos *SortPosition) error {
	for i, s := range ss.runs {
		if !pos.hasMore[i] {
			continue
		}

		if err := s.MoveToRecordID(pos.rids[i]); err != nil {
			return err
		}
	}

	copy(ss.hasMore, pos.hasMore)
	ss.current = pos.current

	return nil
}

// Close closes the runs.
func (ss *SortScan) Close() {
	for _, s := range ss.runs {
		s.Close()
	}
}
