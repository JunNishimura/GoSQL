package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
)

// RecordComparator orders records by a list of fields: by the first field,
// then by the second field for records with equal first fields, and so on.
//
// A sort uses it to order and merge its runs, and a merge join uses it to
// advance its two sides.
type RecordComparator struct {
	fieldNames []string
}

// NewRecordComparator returns a comparator that orders records by fieldNames,
// in the order given.
func NewRecordComparator(fieldNames ...string) *RecordComparator {
	return &RecordComparator{fieldNames: fieldNames}
}

// Compare returns -1, 0 or +1 if the current record of s1 is ordered before,
// equal to or after the current record of s2.
//
// Fields that are not in the comparator's list are ignored. Two records with
// equal values in every listed field therefore compare as 0, whatever their
// other fields hold.
func (rc *RecordComparator) Compare(s1, s2 query.Scan) (int, error) {
	for _, fieldName := range rc.fieldNames {
		val1, err := s1.GetValue(fieldName)
		if err != nil {
			return 0, err
		}
		val2, err := s2.GetValue(fieldName)
		if err != nil {
			return 0, err
		}

		c, err := val1.Compare(val2)
		if err != nil {
			return 0, err
		}
		if c != 0 {
			return c, nil
		}
	}

	return 0, nil
}
