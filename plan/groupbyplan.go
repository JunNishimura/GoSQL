package plan

import (
	"fmt"
	"slices"

	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*GroupByPlan)(nil)

// GroupByPlan is the plan of reading a record for each group of the records
// of another plan, with the group's values in the fields grouped on and the
// value of each aggregation function over the group.
//
// It sorts the records on the fields grouped on first, which puts the records
// of a group next to each other, so the groups are read off the sort in one
// pass.
type GroupByPlan struct {
	sorted      *SortPlan
	groupFields []string
	aggFns      []materialize.AggregationFn
	schema      *recordmanager.Schema
}

// NewGroupByPlan returns the plan of grouping the records of src on
// groupFields, with aggFns worked out over each group.
//
// The schema is built here, which is where a field src does not have, to
// group on or to aggregate, is refused, as a projection refuses one.
func NewGroupByPlan(tx *transaction.Transaction, src Plan, groupFields []string, aggFns []materialize.AggregationFn) (*GroupByPlan, error) {
	schema := recordmanager.NewSchema()
	for _, fieldName := range groupFields {
		if err := schema.Add(fieldName, src.Schema()); err != nil {
			return nil, fmt.Errorf("group on %v: %w", groupFields, err)
		}
	}
	for _, fn := range aggFns {
		if err := fn.AddFieldTo(schema, src.Schema()); err != nil {
			return nil, fmt.Errorf("aggregate into %s: %w", fn.FieldName(), err)
		}
	}

	return &GroupByPlan{
		sorted:      NewSortPlan(tx, src, groupFields...),
		groupFields: groupFields,
		aggFns:      aggFns,
		schema:      schema,
	}, nil
}

// Open sorts the records of src on the fields grouped on and returns a scan of
// their groups, before the first.
func (gp *GroupByPlan) Open() (query.Scan, error) {
	s, err := gp.sorted.Open()
	if err != nil {
		return nil, err
	}

	return materialize.NewGroupByScan(s, gp.groupFields, gp.aggFns)
}

// BlocksAccessed is the blocks of reading the sorted records once, which the
// scan does to find the groups. Sorting them is left out, as SortPlan leaves
// it out.
func (gp *GroupByPlan) BlocksAccessed() int {
	return gp.sorted.BlocksAccessed()
}

// RecordsOutput is a guess at how many groups there are: as many as there are
// combinations of the values the fields grouped on hold, taking every value of
// one field to occur with every value of the others.
func (gp *GroupByPlan) RecordsOutput() int {
	groups := 1
	for _, fieldName := range gp.groupFields {
		groups *= gp.sorted.DistinctValues(fieldName)
	}

	return groups
}

// DistinctValues is a guess at how many different values fieldName holds in
// the groups: those of src for a field grouped on, and one for each group for
// the value of an aggregation function, taking no two groups to share it.
func (gp *GroupByPlan) DistinctValues(fieldName string) int {
	if slices.Contains(gp.groupFields, fieldName) {
		return gp.sorted.DistinctValues(fieldName)
	}

	return gp.RecordsOutput()
}

// Schema is the fields grouped on followed by those of the aggregation
// functions.
func (gp *GroupByPlan) Schema() *recordmanager.Schema {
	return gp.schema
}
