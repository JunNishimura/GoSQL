package materialize

import (
	"slices"

	"github.com/JunNishimura/GoSQL/query"
)

// GroupValue is the values a record holds in the fields a group by groups on,
// which is what tells the records of one group from those of the next.
//
// It is kept as two slices rather than a map so that two can be compared with
// slices.Equal: a Constant compares with ==, and a map does not.
type GroupValue struct {
	fieldNames []string
	vals       []query.Constant
}

// NewGroupValue reads the values the record s is on holds in fieldNames.
func NewGroupValue(s query.Scan, fieldNames []string) (*GroupValue, error) {
	vals := make([]query.Constant, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		val, err := s.GetValue(fieldName)
		if err != nil {
			return nil, err
		}
		vals = append(vals, val)
	}

	return &GroupValue{
		fieldNames: fieldNames,
		vals:       vals,
	}, nil
}

// Value is the value held in fieldName, and false if fieldName is not one of
// the fields grouped on.
func (gv *GroupValue) Value(fieldName string) (query.Constant, bool) {
	i := slices.Index(gv.fieldNames, fieldName)
	if i < 0 {
		return query.Constant{}, false
	}

	return gv.vals[i], true
}

// Equals reports whether other holds the same value as gv in every field.
//
// Only the values are compared. The two are taken to be of the same fields in
// the same order, as two group values of one group by are.
func (gv *GroupValue) Equals(other *GroupValue) bool {
	return slices.Equal(gv.vals, other.vals)
}
