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

// GroupByPlan is the plan that returns one record for each group of the
// records of another plan. Each record holds the values of the group fields
// and the value of each aggregation function over the group.
//
// It first sorts the records on the group fields, which makes the records of
// a group adjacent. The groups are then read from the sorted records in one
// pass.
type GroupByPlan struct {
	sorted      *SortPlan
	groupFields []string
	aggFns      []materialize.AggregationFn
	schema      *recordmanager.Schema
}

// NewGroupByPlan returns the plan that groups the records of src on
// groupFields and computes aggFns over each group.
//
// The schema is built here. A group field or an aggregated field that src
// does not have is therefore rejected here, as in a projection.
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

// Open sorts the records of src on the group fields and returns a scan of the
// groups, positioned before the first group.
func (gp *GroupByPlan) Open() (query.Scan, error) {
	s, err := gp.sorted.Open()
	if err != nil {
		return nil, err
	}

	return materialize.NewGroupByScan(s, gp.groupFields, gp.aggFns)
}

// BlocksAccessed returns the blocks of one read of the sorted records, which
// the scan needs to find the groups. The cost of the sort is not included, as
// in SortPlan.
func (gp *GroupByPlan) BlocksAccessed() int {
	return gp.sorted.BlocksAccessed()
}

// RecordsOutput returns an estimate of the number of groups. It is the number
// of combinations of the values of the group fields, on the assumption that
// every value of one field occurs with every value of the others.
func (gp *GroupByPlan) RecordsOutput() int {
	groups := 1
	for _, fieldName := range gp.groupFields {
		groups *= gp.sorted.DistinctValues(fieldName)
	}

	return groups
}

// DistinctValues returns an estimate of the number of distinct values of
// fieldName in the groups. For a group field it is the count of src. For the
// value of an aggregation function it is the number of groups, on the
// assumption that no two groups have the same value.
func (gp *GroupByPlan) DistinctValues(fieldName string) int {
	if slices.Contains(gp.groupFields, fieldName) {
		return gp.sorted.DistinctValues(fieldName)
	}

	return gp.RecordsOutput()
}

// Schema returns the group fields followed by the fields of the aggregation
// functions.
func (gp *GroupByPlan) Schema() *recordmanager.Schema {
	return gp.schema
}
