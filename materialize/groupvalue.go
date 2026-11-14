package materialize

import (
	"slices"

	"github.com/JunNishimura/GoSQL/query"
)

// GroupValue holds the values of the group fields of one record. These values
// distinguish the records of one group from those of the next.
//
// It stores two slices and not a map, so that two group values can be
// compared with slices.Equal. A Constant supports ==, and a map does not.
type GroupValue struct {
	fieldNames []string
	vals       []query.Constant
}

// NewGroupValue reads the values of fieldNames from the current record of s.
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

// Value returns the value of fieldName. It returns false if fieldName is not
// a group field.
func (gv *GroupValue) Value(fieldName string) (query.Constant, bool) {
	i := slices.Index(gv.fieldNames, fieldName)
	if i < 0 {
		return query.Constant{}, false
	}

	return gv.vals[i], true
}

// Equals reports whether other has the same value as gv for every field.
//
// Only the values are compared. Both are assumed to have the same fields in
// the same order, as two group values of one group by do.
func (gv *GroupValue) Equals(other *GroupValue) bool {
	return slices.Equal(gv.vals, other.vals)
}
