package index

import (
	"github.com/JunNishimura/GoSQL/query"
)

// JoinScan reads the pairs of a record of one scan and a record of a table
// that an index on the table files under what the first record holds in a
// field, which is a join of the two on that field and the indexed one.
//
// For each record on the left, the index is searched for the value of the
// join field, and the table is moved to each record it gives in turn. That
// reads the records on the right that the join keeps and no others, where a
// product would read every record on the right for every one on the left and
// leave the join to a select over it.
type JoinScan struct {
	lhs       query.Scan
	idx       Index
	joinField string
	rhs       query.UpdateScan
	// onLeftRecord says whether lhs is on a record, which it is from a move
	// before the first record until the left side runs out.
	onLeftRecord bool
}

var _ query.Scan = (*JoinScan)(nil)

// NewJoinScan returns a scan of the pairs of a record of lhs and a record of
// rhs that idx files under what the record of lhs holds in joinField, before
// the first of them.
//
// rhs is taken as an UpdateScan because its records are reached by moving it
// to a record id. The scan owns lhs, idx and rhs from here on, and Close
// closes them; if the first move fails, they are closed before the error comes
// back, since there is then no scan for the caller to close.
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
// the index for its join value, so that the next move arrives at the first
// pair.
func (s *JoinScan) MoveBeforeFirstRecord() error {
	if err := s.lhs.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	return s.moveToNextLeftRecord()
}

// MoveToNextRecord moves on to the next pair and reports whether there was
// one.
//
// The next pair is the next record the index files under the current left
// record's value, and once there are none, the first such record of the next
// left record that has one. A left record nothing on the right matches is
// passed over.
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

// moveToNextLeftRecord moves the left scan on by one record and, if there is
// one, searches the index for what it holds in the join field.
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

// GetInt reads fieldName of the current pair, from the right record if it has
// the field and from the left one otherwise.
func (s *JoinScan) GetInt(fieldName string) (int32, error) {
	if s.rhs.HasField(fieldName) {
		return s.rhs.GetInt(fieldName)
	}

	return s.lhs.GetInt(fieldName)
}

// GetString reads fieldName of the current pair, from the right record if it
// has the field and from the left one otherwise.
func (s *JoinScan) GetString(fieldName string) (string, error) {
	if s.rhs.HasField(fieldName) {
		return s.rhs.GetString(fieldName)
	}

	return s.lhs.GetString(fieldName)
}

// GetValue reads fieldName of the current pair, from the right record if it
// has the field and from the left one otherwise.
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
