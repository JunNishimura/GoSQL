package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*MergeJoinPlan)(nil)

// MergeJoinPlan is the plan that joins the records of two plans on one field
// of each. It sorts both plans on their join field and merges them.
//
// It returns the same pairs as a select over the product of the two plans
// with a predicate that equates the fields. It reads each side once, where a
// product reads the right side once for every left record.
type MergeJoinPlan struct {
	p1     *SortPlan
	p2     *SortPlan
	field1 string
	field2 string
	schema *recordmanager.Schema
}

// NewMergeJoinPlan returns the plan that joins the records of p1 and p2 for
// which field1 and field2 have the same value.
//
// It fails, like a product, if the two plans share a field name. It also
// fails if either side does not have its join field.
func NewMergeJoinPlan(tx *transaction.Transaction, p1, p2 Plan, field1, field2 string) (*MergeJoinPlan, error) {
	if !p1.Schema().HasField(field1) {
		return nil, fmt.Errorf("merge join on %q of the left side: %w", field1, recordmanager.ErrFieldNotFound)
	}
	if !p2.Schema().HasField(field2) {
		return nil, fmt.Errorf("merge join on %q of the right side: %w", field2, recordmanager.ErrFieldNotFound)
	}

	schema := recordmanager.NewSchema()
	if err := schema.AddAll(p1.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the left side of a merge join: %w", err)
	}
	if err := schema.AddAll(p2.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the right side of a merge join: %w", err)
	}

	return &MergeJoinPlan{
		p1:     NewSortPlan(tx, p1, field1),
		p2:     NewSortPlan(tx, p2, field2),
		field1: field1,
		field2: field2,
		schema: schema,
	}, nil
}

// Open sorts both plans on their join field and returns a scan of the joined
// pairs, positioned before the first pair.
//
// If the right side cannot be opened, the left side is closed, because no
// scan is returned that the caller could close.
func (mp *MergeJoinPlan) Open() (query.Scan, error) {
	lhs, err := mp.p1.Open()
	if err != nil {
		return nil, err
	}

	rhs, err := mp.p2.openSortScan()
	if err != nil {
		lhs.Close()

		return nil, err
	}

	return materialize.NewMergeJoinScan(lhs, rhs, mp.field1, mp.field2)
}

// BlocksAccessed returns the blocks of one read of both sorted plans.
//
// The cost of the sorts is not included, as in SortPlan. The result is
// therefore lower than the cost of opening the plan, and a merge join looks
// cheaper here than it is when compared with a plan that sorts nothing.
func (mp *MergeJoinPlan) BlocksAccessed() int {
	return mp.p1.BlocksAccessed() + mp.p2.BlocksAccessed()
}

// RecordsOutput returns an estimate of the number of joined pairs. It is the
// number of pairs of the product divided by the larger of the two distinct
// value counts of the join fields. This assumes that the values of the other
// side are among those values and are evenly distributed.
func (mp *MergeJoinPlan) RecordsOutput() int {
	maxVals := max(mp.p1.DistinctValues(mp.field1), mp.p2.DistinctValues(mp.field2))

	return mp.p1.RecordsOutput() * mp.p2.RecordsOutput() / maxVals
}

// DistinctValues returns an estimate of the number of distinct values of
// fieldName in the pairs. It is assumed to equal the count on the field's own
// side.
func (mp *MergeJoinPlan) DistinctValues(fieldName string) int {
	if mp.p1.Schema().HasField(fieldName) {
		return mp.p1.DistinctValues(fieldName)
	}

	return mp.p2.DistinctValues(fieldName)
}

// Schema returns the fields of both sides, with those of p1 first.
func (mp *MergeJoinPlan) Schema() *recordmanager.Schema {
	return mp.schema
}
