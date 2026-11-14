package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
)

var _ query.Scan = (*MergeJoinScan)(nil)

// MergeJoinScan returns the pairs of a record of one scan and a record of a
// sort scan that have the same value in their join fields. Both scans must be
// sorted on their join field.
//
// The two scans advance together: the scan with the smaller value moves
// forward until both have the same value. Records with a value that only one
// side has are therefore read once. The right records for a value are read
// again for each left record with that value. For this, the right scan is
// restored to the first of these records. It must therefore be a sort scan,
// which supports restoring a position.
type MergeJoinScan struct {
	lhs      query.Scan
	rhs      *SortScan
	lhsField string
	rhsField string
	// joinVal is the join value of the current pair. hasJoinVal reports
	// whether it is set. It is not set before the first pair.
	joinVal    query.Constant
	hasJoinVal bool
	// joinPos is the position of rhs at its first record with joinVal.
	joinPos *SortPosition
}

// NewMergeJoinScan returns a scan of the pairs of a record of lhs and a
// record of rhs for which lhsField and rhsField have the same value. The scan
// is positioned before the first pair.
//
// lhs must be sorted on lhsField, and rhs on rhsField. The scan takes
// ownership of both, and Close closes them. If the initial positioning fails,
// NewMergeJoinScan closes them before it returns the error, because the
// caller then has no scan to close.
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

// MoveBeforeFirstRecord positions both scans before their first records.
//
// It also clears the last join value. Otherwise that value would match the
// first right record whenever the record has the same value. The scan would
// then return a pair while the left side is on no record yet.
func (ms *MergeJoinScan) MoveBeforeFirstRecord() error {
	ms.hasJoinVal = false
	ms.joinPos = nil

	if err := ms.lhs.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	return ms.rhs.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves to the next pair and reports whether one exists.
//
// The next pair is the first that applies of these: the current left record
// with the next right record, if that record has the same value. The next
// left record with the first right record for the value, if the next left
// record also has the value. Otherwise, the first pair for the next value
// that both sides have.
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

// side returns the scan that fieldName is read from: the left scan if it has
// the field, and the right scan otherwise.
func (ms *MergeJoinScan) side(fieldName string) query.Scan {
	if ms.lhs.HasField(fieldName) {
		return ms.lhs
	}

	return ms.rhs
}

// GetInt reads fieldName of the current pair. It reads from the left record
// if that record has the field, and from the right record otherwise.
func (ms *MergeJoinScan) GetInt(fieldName string) (int32, error) {
	return ms.side(fieldName).GetInt(fieldName)
}

// GetString reads fieldName of the current pair. It reads from the left
// record if that record has the field, and from the right record otherwise.
func (ms *MergeJoinScan) GetString(fieldName string) (string, error) {
	return ms.side(fieldName).GetString(fieldName)
}

// GetValue reads fieldName of the current pair. It reads from the left record
// if that record has the field, and from the right record otherwise.
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
