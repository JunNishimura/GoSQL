package materialize

import (
	"fmt"
	"slices"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ query.Scan = (*GroupByScan)(nil)

// GroupByScan returns one record for each group of the records of a scan. The
// scan must be sorted on the group fields. Each record holds the values of
// the group fields and the value of each aggregation function over the group.
//
// Because the input is sorted, the records of a group are adjacent. A group
// ends at the first record with a different group value, and the scan reads
// its input once, whatever the number of groups. With no group fields, all
// records form one group.
type GroupByScan struct {
	s           query.Scan
	groupFields []string
	aggFns      []AggregationFn
	// groupVal is the group value of the current group, or nil before the
	// first group.
	groupVal *GroupValue
	// moreGroups reports whether s is on a record, which is the first record
	// of the next group. It is false after the last record of s.
	moreGroups bool
}

// NewGroupByScan returns a scan of the groups of s, positioned before the
// first group.
//
// s must be sorted on groupFields. The scan takes ownership of s, and Close
// closes it. If the initial positioning fails, NewGroupByScan closes s before
// it returns the error, because the caller then has no scan to close.
func NewGroupByScan(s query.Scan, groupFields []string, aggFns []AggregationFn) (*GroupByScan, error) {
	gs := &GroupByScan{
		s:           s,
		groupFields: groupFields,
		aggFns:      aggFns,
	}
	if err := gs.MoveBeforeFirstRecord(); err != nil {
		gs.Close()

		return nil, err
	}

	return gs, nil
}

// MoveBeforeFirstRecord positions the scan before its first group, with s on
// the first record of that group.
func (gs *GroupByScan) MoveBeforeFirstRecord() error {
	gs.groupVal = nil
	if err := gs.s.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	ok, err := gs.s.MoveToNextRecord()
	if err != nil {
		return err
	}
	gs.moreGroups = ok

	return nil
}

// MoveToNextRecord moves to the next group and reports whether one exists.
//
// It passes the records of the group to the aggregation functions. It stops
// on the first record of the following group, which the next move processes.
func (gs *GroupByScan) MoveToNextRecord() (bool, error) {
	if !gs.moreGroups {
		return false, nil
	}

	groupVal, err := NewGroupValue(gs.s, gs.groupFields)
	if err != nil {
		return false, err
	}
	gs.groupVal = groupVal
	for _, fn := range gs.aggFns {
		if err := fn.ProcessFirst(gs.s); err != nil {
			return false, err
		}
	}

	for {
		ok, err := gs.s.MoveToNextRecord()
		if err != nil {
			return false, err
		}
		gs.moreGroups = ok
		if !ok {
			return true, nil
		}

		next, err := NewGroupValue(gs.s, gs.groupFields)
		if err != nil {
			return false, err
		}
		if !gs.groupVal.Equals(next) {
			return true, nil
		}

		for _, fn := range gs.aggFns {
			if err := fn.ProcessNext(gs.s); err != nil {
				return false, err
			}
		}
	}
}

// GetInt reads fieldName of the current group as an int.
func (gs *GroupByScan) GetInt(fieldName string) (int32, error) {
	val, err := gs.GetValue(fieldName)
	if err != nil {
		return 0, err
	}

	return val.AsInt()
}

// GetString reads fieldName of the current group as a varchar.
func (gs *GroupByScan) GetString(fieldName string) (string, error) {
	val, err := gs.GetValue(fieldName)
	if err != nil {
		return "", err
	}

	return val.AsString()
}

// GetValue reads fieldName of the current group. For a group field it returns
// the group's value. For the name of an aggregation function it returns the
// value of that function.
func (gs *GroupByScan) GetValue(fieldName string) (query.Constant, error) {
	if gs.groupVal == nil {
		return query.Constant{}, fmt.Errorf("read %q of a group by: %w", fieldName, query.ErrNoCurrentRecord)
	}

	if val, ok := gs.groupVal.Value(fieldName); ok {
		return val, nil
	}
	for _, fn := range gs.aggFns {
		if fn.FieldName() == fieldName {
			return fn.Value(), nil
		}
	}

	return query.Constant{}, fmt.Errorf("read %q of a group by: %w", fieldName, recordmanager.ErrFieldNotFound)
}

// HasField reports whether fieldName is a group field or the name of an
// aggregation function.
func (gs *GroupByScan) HasField(fieldName string) bool {
	if slices.Contains(gs.groupFields, fieldName) {
		return true
	}

	return slices.ContainsFunc(gs.aggFns, func(fn AggregationFn) bool {
		return fn.FieldName() == fieldName
	})
}

// Close closes the underlying scan.
func (gs *GroupByScan) Close() {
	gs.s.Close()
}
