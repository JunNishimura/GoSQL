package plan

import (
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*SelectPlan)(nil)

// SelectPlan is the plan of keeping the records of another plan that meet a
// predicate: the "where" of a query.
type SelectPlan struct {
	p    Plan
	pred query.Predicate
}

// NewSelectPlan returns the plan of the records of p that meet pred.
func NewSelectPlan(p Plan, pred query.Predicate) *SelectPlan {
	return &SelectPlan{
		p:    p,
		pred: pred,
	}
}

// Open returns a select scan over a scan of the plan underneath.
func (sp *SelectPlan) Open() (query.Scan, error) {
	s, err := sp.p.Open()
	if err != nil {
		return nil, err
	}

	return query.NewSelectScan(s, sp.pred), nil
}

// BlocksAccessed is the blocks of the plan underneath, all of them. A select
// has to look at every record to know which to keep, so what it keeps changes
// how many records come out, not how much is read.
func (sp *SelectPlan) BlocksAccessed() int {
	return sp.p.BlocksAccessed()
}

// RecordsOutput is the records of the plan underneath, cut down by the
// predicate's reduction factor.
func (sp *SelectPlan) RecordsOutput() int {
	return sp.p.RecordsOutput() / reductionFactor(sp.pred, sp.p)
}

// DistinctValues is how many different values fieldName holds among the
// records kept.
//
// A field equated with a constant keeps that one value. A field equated with
// another keeps only the values both hold, which is guessed as however many
// the one of fewer holds. A field the predicate does not pin down keeps
// whatever it held underneath: the predicate drops records, but nothing says
// which values those took with them.
func (sp *SelectPlan) DistinctValues(fieldName string) int {
	if equatesWithConstant(sp.pred, fieldName) {
		return 1
	}

	if other, ok := equatesWithField(sp.pred, fieldName); ok {
		return min(sp.p.DistinctValues(fieldName), sp.p.DistinctValues(other))
	}

	return sp.p.DistinctValues(fieldName)
}

// Schema is the schema of the plan underneath. A select keeps whole records,
// so the fields are the same.
func (sp *SelectPlan) Schema() *recordmanager.Schema {
	return sp.p.Schema()
}

// equatesWithConstant reports whether a term of pred equates fieldName with a
// constant, on either side.
func equatesWithConstant(pred query.Predicate, fieldName string) bool {
	for _, term := range pred.Terms() {
		if isField(term.LHS(), fieldName) && isConstant(term.RHS()) {
			return true
		}
		if isConstant(term.LHS()) && isField(term.RHS(), fieldName) {
			return true
		}
	}

	return false
}

// equatesWithField returns the other field a term of pred equates fieldName
// with, on either side.
func equatesWithField(pred query.Predicate, fieldName string) (string, bool) {
	for _, term := range pred.Terms() {
		lhs, lhsIsField := term.LHS().(query.FieldExpression)
		rhs, rhsIsField := term.RHS().(query.FieldExpression)
		if !lhsIsField || !rhsIsField {
			continue
		}

		if lhs.FieldName() == fieldName {
			return rhs.FieldName(), true
		}
		if rhs.FieldName() == fieldName {
			return lhs.FieldName(), true
		}
	}

	return "", false
}

func isField(e query.Expression, fieldName string) bool {
	field, ok := e.(query.FieldExpression)
	return ok && field.FieldName() == fieldName
}

func isConstant(e query.Expression) bool {
	_, ok := e.(query.ConstantExpression)
	return ok
}
