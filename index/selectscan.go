package index

import (
	"github.com/JunNishimura/GoSQL/query"
)

// SelectScan reads the records of a table that an index files under one value,
// going straight to each through the record id the index gives rather than
// reading the records in between.
//
// It is what a select on the indexed field comes to once there is an index on
// it: the records the predicate could keep are the ones filed under the value
// it equates the field with, and the index knows which those are. The fields
// of a record are read from the table, since the index holds only where the
// record is.
type SelectScan struct {
	ts    query.UpdateScan
	idx   Index
	value query.Constant
}

var _ query.Scan = (*SelectScan)(nil)

// NewSelectScan returns a scan of the records of ts that idx files under value,
// before the first of them.
//
// ts is taken as an UpdateScan because the records are reached by moving it to
// a record id, which only an update scan can be moved to. It is only read
// through. The scan owns both ts and idx from here on, and Close closes them;
// if the first search fails, they are closed before the error comes back,
// since there is then no scan for the caller to close.
func NewSelectScan(ts query.UpdateScan, idx Index, value query.Constant) (*SelectScan, error) {
	s := &SelectScan{
		ts:    ts,
		idx:   idx,
		value: value,
	}
	if err := s.MoveBeforeFirstRecord(); err != nil {
		s.Close()

		return nil, err
	}

	return s, nil
}

// MoveBeforeFirstRecord searches the index for the value again, so that the
// next move arrives at the first record filed under it.
func (s *SelectScan) MoveBeforeFirstRecord() error {
	return s.idx.MoveBeforeFirstMatch(s.value)
}

// MoveToNextRecord moves the table to the next record the index files under
// the value, and reports whether there was one.
func (s *SelectScan) MoveToNextRecord() (bool, error) {
	ok, err := s.idx.MoveToNextMatch()
	if err != nil || !ok {
		return false, err
	}

	rid, err := s.idx.DataRecordID()
	if err != nil {
		return false, err
	}
	if err := s.ts.MoveToRecordID(rid); err != nil {
		return false, err
	}

	return true, nil
}

// GetInt reads fieldName of the current record from the table.
func (s *SelectScan) GetInt(fieldName string) (int32, error) {
	return s.ts.GetInt(fieldName)
}

// GetString reads fieldName of the current record from the table.
func (s *SelectScan) GetString(fieldName string) (string, error) {
	return s.ts.GetString(fieldName)
}

// GetValue reads fieldName of the current record from the table.
func (s *SelectScan) GetValue(fieldName string) (query.Constant, error) {
	return s.ts.GetValue(fieldName)
}

// HasField reports whether the table has a field called fieldName.
func (s *SelectScan) HasField(fieldName string) bool {
	return s.ts.HasField(fieldName)
}

// Close closes the index and the table.
func (s *SelectScan) Close() {
	s.idx.Close()
	s.ts.Close()
}
