package materialize

import (
	"fmt"
	"slices"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ query.Scan = (*GroupByScan)(nil)

// GroupByScan reads a record for each group of the records of a scan sorted
// on the fields it groups on, holding the group's values in those fields and
// the value of each aggregation function over the group.
//
// The sort is what makes a group a stretch of records next to each other, so
// a group ends at the first record whose group value differs, and the scan
// reads its input once however many groups there are. Grouping on no fields
// makes every record one group.
type GroupByScan struct {
	s           query.Scan
	groupFields []string
	aggFns      []AggregationFn
	// groupVal is the group value of the group the scan is on, or nil before
	// the first.
	groupVal *GroupValue
	// moreGroups says whether s is on a record, the first of the next group,
	// rather than past its last.
	moreGroups bool
}

// NewGroupByScan returns a scan of the groups of s, before the first.
//
// s has to be sorted on groupFields. The scan owns s from here on, and Close
// closes it; if the first move fails, s is closed before the error comes back,
// since there is then no scan for the caller to close.
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

// MoveBeforeFirstRecord puts the scan back before its first group, with s on
// the first record of it.
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

// MoveToNextRecord moves on to the next group and reports whether there was
// one.
//
// It reads the records of the group through the aggregation functions, and
// stops on the first record of the group after, which is left for the next
// move.
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

// GetValue reads fieldName of the current group: the group's value if it is a
// field grouped on, and an aggregation function's value if it is that
// function's name.
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

// HasField reports whether fieldName is a field grouped on or the name of an
// aggregation function's value.
func (gs *GroupByScan) HasField(fieldName string) bool {
	if slices.Contains(gs.groupFields, fieldName) {
		return true
	}

	return slices.ContainsFunc(gs.aggFns, func(fn AggregationFn) bool {
		return fn.FieldName() == fieldName
	})
}

// Close closes the scan of the records grouped.
func (gs *GroupByScan) Close() {
	gs.s.Close()
}
