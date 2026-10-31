package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
)

var _ query.Scan = (*MergeJoinScan)(nil)

// MergeJoinScan reads the pairs of a record of one scan and a record of a sort
// scan that hold the same value in a field of each, both scans being sorted on
// their field.
//
// The two are stepped along together, the one on the smaller value moving on,
// until they meet on a value, so each is read once for the values only one of
// them holds. The records of the right that hold a value are read again for
// each record of the left that holds it: the right is put back to the first
// of them, which is why it has to be a sort scan, the scan that can be.
type MergeJoinScan struct {
	lhs      query.Scan
	rhs      *SortScan
	lhsField string
	rhsField string
	// joinVal is the value the current pair was joined on, and hasJoinVal says
	// whether there is one: there is none before the first pair.
	joinVal    query.Constant
	hasJoinVal bool
	// joinPos is where rhs was on the first of its records holding joinVal.
	joinPos *SortPosition
}

// NewMergeJoinScan returns a scan of the pairs of a record of lhs and a record
// of rhs whose lhsField and rhsField hold the same value, before the first.
//
// lhs has to be sorted on lhsField, as rhs is on rhsField. The scan owns both
// from here on, and Close closes them; if the first move fails, they are
// closed before the error comes back, since there is then no scan for the
// caller to close.
func NewMergeJoinScan(lhs query.Scan, rhs *SortScan, lhsField, rhsField string) (*MergeJoinScan, error) {
	ms := &MergeJoinScan{
		lhs:      lhs,
		rhs:      rhs,
		lhsField: lhsField,
		rhsField: rhsField,
	}
	if err := ms.MoveBeforeFirstRecord(); err != nil {
		ms.Close()

		return nil, err
	}

	return ms, nil
}

// MoveBeforeFirstRecord puts both scans back before their first records.
//
// The value last joined on is forgotten too. Kept, it would match the first
// record on the right whenever that holds the same value, and a pair would be
// made of it and a left side that is on no record yet.
func (ms *MergeJoinScan) MoveBeforeFirstRecord() error {
	ms.hasJoinVal = false
	ms.joinPos = nil

	if err := ms.lhs.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	return ms.rhs.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves on to the next pair and reports whether there was
// one.
//
// The next pair is, in turn: the current left record with the next right
// record, if that holds the same value; the next left record with the first
// right record of the value, if the next left record holds it too; and
// otherwise the first pair of the next value both sides hold.
func (ms *MergeJoinScan) MoveToNextRecord() (bool, error) {
	hasMore2, err := ms.rhs.MoveToNextRecord()
	if err != nil {
		return false, err
	}
	if hasMore2 && ms.hasJoinVal {
		val2, err := ms.rhs.GetValue(ms.rhsField)
		if err != nil {
			return false, err
		}
		if val2 == ms.joinVal {
			return true, nil
		}
	}

	hasMore1, err := ms.lhs.MoveToNextRecord()
	if err != nil {
		return false, err
	}
	if hasMore1 && ms.hasJoinVal {
		val1, err := ms.lhs.GetValue(ms.lhsField)
		if err != nil {
			return false, err
		}
		if val1 == ms.joinVal {
			if err := ms.rhs.RestorePosition(ms.joinPos); err != nil {
				return false, err
			}

			return true, nil
		}
	}

	for hasMore1 && hasMore2 {
		val1, err := ms.lhs.GetValue(ms.lhsField)
		if err != nil {
			return false, err
		}
		val2, err := ms.rhs.GetValue(ms.rhsField)
		if err != nil {
			return false, err
		}

		c, err := val1.Compare(val2)
		if err != nil {
			return false, err
		}
		switch {
		case c < 0:
			if hasMore1, err = ms.lhs.MoveToNextRecord(); err != nil {
				return false, err
			}
		case c > 0:
			if hasMore2, err = ms.rhs.MoveToNextRecord(); err != nil {
				return false, err
			}
		default:
			pos, err := ms.rhs.SavePosition()
			if err != nil {
				return false, err
			}
			ms.joinPos = pos
			ms.joinVal = val2
			ms.hasJoinVal = true

			return true, nil
		}
	}

	return false, nil
}

// side is the scan fieldName is read from: the left if it has the field, and
// the right otherwise.
func (ms *MergeJoinScan) side(fieldName string) query.Scan {
	if ms.lhs.HasField(fieldName) {
		return ms.lhs
	}

	return ms.rhs
}

// GetInt reads fieldName of the current pair, from the left record if it has
// the field and from the right one otherwise.
func (ms *MergeJoinScan) GetInt(fieldName string) (int32, error) {
	return ms.side(fieldName).GetInt(fieldName)
}

// GetString reads fieldName of the current pair, from the left record if it
// has the field and from the right one otherwise.
func (ms *MergeJoinScan) GetString(fieldName string) (string, error) {
	return ms.side(fieldName).GetString(fieldName)
}

// GetValue reads fieldName of the current pair, from the left record if it has
// the field and from the right one otherwise.
func (ms *MergeJoinScan) GetValue(fieldName string) (query.Constant, error) {
	return ms.side(fieldName).GetValue(fieldName)
}

// HasField reports whether either side has a field called fieldName.
func (ms *MergeJoinScan) HasField(fieldName string) bool {
	return ms.lhs.HasField(fieldName) || ms.rhs.HasField(fieldName)
}

// Close closes both scans.
func (ms *MergeJoinScan) Close() {
	ms.lhs.Close()
	ms.rhs.Close()
}
