package index

import (
	"github.com/JunNishimura/GoSQL/query"
)

// JoinScan joins a scan with a table by using an index on the table. It
// reads each pair of a left record and a table record whose indexed field
// equals the left record's join field.
//
// For each left record, JoinScan searches the index for the value of the join
// field. It then moves the table to each record id that the index returns.
// This reads only the right records that the join keeps. A product scan would
// instead read every right record for every left record, and a select scan
// over it would filter the pairs.
type JoinScan struct {
	lhs       query.Scan
	idx       Index
	joinField string
	rhs       query.UpdateScan
	// onLeftRecord reports whether lhs is positioned on a record. It is true
	// from the first move until lhs has no more records.
	onLeftRecord bool
}

var _ query.Scan = (*JoinScan)(nil)

// NewJoinScan returns a scan over the pairs of a record of lhs and a record
// of rhs, where idx maps the lhs record's value of joinField to the rhs
// record. The scan is positioned before the first pair.
//
// rhs must be an UpdateScan because the scan reaches its records by record
// id. The scan takes ownership of lhs, idx and rhs, and Close closes them. If
// the initial positioning fails, NewJoinScan closes them before it returns
// the error, because the caller then has no scan to close.
func NewJoinScan(lhs query.Scan, idx Index, joinField string, rhs query.UpdateScan) (*JoinScan, error) {
	s := &JoinScan{
		lhs:       lhs,
		idx:       idx,
		joinField: joinField,
		rhs:       rhs,
	}
	if err := s.MoveBeforeFirstRecord(); err != nil {
		s.Close()

		return nil, err
	}

	return s, nil
}

// MoveBeforeFirstRecord moves the left scan to its first record and searches
// the index for that record's join value. The next call to MoveToNextRecord
// then moves to the first pair.
func (s *JoinScan) MoveBeforeFirstRecord() error {
	if err := s.lhs.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	return s.moveToNextLeftRecord()
}

// MoveToNextRecord moves to the next pair and reports whether one exists.
//
// The next pair uses the next index entry for the current left record's join
// value. When there are no more entries, the scan advances to the next left
// record that has at least one entry. A left record with no matching right
// record is skipped.
func (s *JoinScan) MoveToNextRecord() (bool, error) {
	for s.onLeftRecord {
		ok, err := s.idx.MoveToNextMatch()
		if err != nil {
			return false, err
		}
		if ok {
			rid, err := s.idx.DataRecordID()
			if err != nil {
				return false, err
			}
			if err := s.rhs.MoveToRecordID(rid); err != nil {
				return false, err
			}

			return true, nil
		}

		if err := s.moveToNextLeftRecord(); err != nil {
			return false, err
		}
	}

	return false, nil
}

// moveToNextLeftRecord advances the left scan by one record. If a record
// exists, it searches the index for that record's join value.
func (s *JoinScan) moveToNextLeftRecord() error {
	ok, err := s.lhs.MoveToNextRecord()
	if err != nil {
		return err
	}
	s.onLeftRecord = ok
	if !ok {
		return nil
	}

	value, err := s.lhs.GetValue(s.joinField)
	if err != nil {
		return err
	}

	return s.idx.MoveBeforeFirstMatch(value)
}

// GetInt reads fieldName of the current pair. It reads from the right record
// if that record has the field, and from the left record otherwise.
func (s *JoinScan) GetInt(fieldName string) (int32, error) {
	if s.rhs.HasField(fieldName) {
		return s.rhs.GetInt(fieldName)
	}

	return s.lhs.GetInt(fieldName)
}

// GetString reads fieldName of the current pair. It reads from the right
// record if that record has the field, and from the left record otherwise.
func (s *JoinScan) GetString(fieldName string) (string, error) {
	if s.rhs.HasField(fieldName) {
		return s.rhs.GetString(fieldName)
	}

	return s.lhs.GetString(fieldName)
}

// GetValue reads fieldName of the current pair. It reads from the right
// record if that record has the field, and from the left record otherwise.
func (s *JoinScan) GetValue(fieldName string) (query.Constant, error) {
	if s.rhs.HasField(fieldName) {
		return s.rhs.GetValue(fieldName)
	}

	return s.lhs.GetValue(fieldName)
}

// HasField reports whether either side has a field called fieldName.
func (s *JoinScan) HasField(fieldName string) bool {
	return s.rhs.HasField(fieldName) || s.lhs.HasField(fieldName)
}

// Close closes the left scan, the index and the table.
func (s *JoinScan) Close() {
	s.lhs.Close()
	s.idx.Close()
	s.rhs.Close()
}
