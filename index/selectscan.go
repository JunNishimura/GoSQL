package index

import (
	"github.com/JunNishimura/GoSQL/query"
)

// SelectScan reads the table records that an index holds for one key. It
// moves straight to each record by the record id from the index, and does not
// read the records in between.
//
// It replaces a select scan on the indexed field when an index exists. The
// records that the predicate can keep are the ones whose indexed field equals
// the value in the predicate, and the index locates exactly those. The fields
// of a record are read from the table, because the index stores only the
// record id.
type SelectScan struct {
	ts    query.UpdateScan
	idx   Index
	value query.Constant
}

var _ query.Scan = (*SelectScan)(nil)

// NewSelectScan returns a scan over the records of ts that idx holds for
// value. The scan is positioned before the first of them.
//
// ts must be an UpdateScan because only an update scan can move to a record
// id. The scan never writes through it. The scan takes ownership of ts and
// idx, and Close closes them. If the initial search fails, NewSelectScan
// closes them before it returns the error, because the caller then has no
// scan to close.
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

// MoveBeforeFirstRecord searches the index for the value again. The next call
// to MoveToNextRecord then moves to the first record for it.
func (s *SelectScan) MoveBeforeFirstRecord() error {
	return s.idx.MoveBeforeFirstMatch(s.value)
}

// MoveToNextRecord moves the table to the next record that the index holds
// for the value, and reports whether one exists.
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
