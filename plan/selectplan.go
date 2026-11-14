package plan

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*SelectPlan)(nil)

// SelectPlan is the plan that keeps the records of another plan that satisfy
// a predicate: the "where" clause of a query.
type SelectPlan struct {
	p    Plan
	pred query.Predicate
}

// NewSelectPlan returns the plan for the records of p that satisfy pred.
func NewSelectPlan(p Plan, pred query.Predicate) *SelectPlan {
	return &SelectPlan{
		p:    p,
		pred: pred,
	}
}

// Open returns a select scan over a scan of the underlying plan.
func (sp *SelectPlan) Open() (query.Scan, error) {
	s, err := sp.p.Open()
	if err != nil {
		return nil, err
	}

	return query.NewSelectScan(s, sp.pred), nil
}

// BlocksAccessed returns the blocks of the underlying plan, unchanged. A
// select must read every record to decide which to keep. The predicate
// therefore changes the number of output records, not the number of blocks
// read.
func (sp *SelectPlan) BlocksAccessed() int {
	return sp.p.BlocksAccessed()
}

// RecordsOutput returns the records of the underlying plan divided by the
// reduction factor of the predicate.
func (sp *SelectPlan) RecordsOutput() int {
	return sp.p.RecordsOutput() / reductionFactor(sp.pred, sp.p)
}

// DistinctValues returns the estimated number of distinct values of fieldName
// among the kept records.
//
// A field equated with a constant has one value. A field equated with another
// field keeps only the values that both fields have, which is estimated as
// the smaller of the two counts. A field that the predicate does not
// constrain keeps the count of the underlying plan. The predicate drops
// records, but nothing indicates which values are lost with them.
func (sp *SelectPlan) DistinctValues(fieldName string) int {
	if equatesWithConstant(sp.pred, fieldName) {
		return 1
	}

	if other, ok := equatesWithField(sp.pred, fieldName); ok {
		return min(sp.p.DistinctValues(fieldName), sp.p.DistinctValues(other))
	}

	return sp.p.DistinctValues(fieldName)
}

// Schema returns the schema of the underlying plan. A select keeps whole
// records, so the fields are the same.
func (sp *SelectPlan) Schema() *recordmanager.Schema {
	return sp.p.Schema()
}
