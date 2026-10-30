package materialize

import (
	"github.com/JunNishimura/GoSQL/query"
)

// RecordComparator puts records in order by a list of fields: by the first,
// then by the second among records the first does not tell apart, and so on.
//
// It is what a sort orders its runs by and merges them by, and what a merge
// join steps its two sides along by.
type RecordComparator struct {
	fieldNames []string
}

// NewRecordComparator returns a comparator ordering records by fieldNames, in
// the order they are named.
func NewRecordComparator(fieldNames ...string) *RecordComparator {
	return &RecordComparator{fieldNames: fieldNames}
}

// Compare reports whether the record s1 is on comes before the one s2 is on,
// after it or with it, as -1, +1 or 0.
//
// Fields not named to the comparator are not looked at, so two records equal
// in every named field compare as 0 however much they differ elsewhere.
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
