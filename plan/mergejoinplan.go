package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*MergeJoinPlan)(nil)

// MergeJoinPlan is the plan of joining the records of two plans on a field of
// each by sorting both on their field and merging them.
//
// It gives back the same pairs as a select over the product of the two on a
// predicate equating the fields, reading each side once rather than the right
// once for every record of the left.
type MergeJoinPlan struct {
	p1     *SortPlan
	p2     *SortPlan
	field1 string
	field2 string
	schema *recordmanager.Schema
}

// NewMergeJoinPlan returns the plan of joining the records of p1 and p2 whose
// field1 and field2 hold the same value.
//
// It fails as a product would if the two share a field name, and if either
// side lacks the field it is joined on.
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

// Open sorts both plans on their field and returns a scan of the pairs that
// join, before the first.
//
// The left side is closed again if the right cannot be opened, since no scan
// comes back for the caller to close.
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

// BlocksAccessed is the blocks of reading both sorted plans once.
//
// Sorting them is left out, as SortPlan leaves it out, so this is less than
// what opening the plan costs: a merge join looks cheaper here than it is next
// to a plan that sorts nothing.
func (mp *MergeJoinPlan) BlocksAccessed() int {
	return mp.p1.BlocksAccessed() + mp.p2.BlocksAccessed()
}

// RecordsOutput is a guess at how many pairs join: every pair of the product,
// over the number of values of the join field of the side with more of them,
// taking the values of the other side to be among them and spread evenly.
func (mp *MergeJoinPlan) RecordsOutput() int {
	maxVals := max(mp.p1.DistinctValues(mp.field1), mp.p2.DistinctValues(mp.field2))

	return mp.p1.RecordsOutput() * mp.p2.RecordsOutput() / maxVals
}

// DistinctValues is a guess at how many different values fieldName holds in
// the pairs, taken to be what it holds on its own side.
func (mp *MergeJoinPlan) DistinctValues(fieldName string) int {
	if mp.p1.Schema().HasField(fieldName) {
		return mp.p1.DistinctValues(fieldName)
	}

	return mp.p2.DistinctValues(fieldName)
}

// Schema is the fields of both sides, p1's first.
func (mp *MergeJoinPlan) Schema() *recordmanager.Schema {
	return mp.schema
}
