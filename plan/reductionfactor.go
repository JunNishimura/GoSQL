package plan

import (
	"math"

	"github.com/JunNishimura/GoSQL/query"
)

// reductionFactor returns an estimate of the factor by which pred reduces the
// number of records when it is applied to the output of p.
//
// The function is here and not on the predicate because it needs data from
// two places: the structure of each term, which the predicate exposes, and
// the number of distinct values of a field, which only a plan knows.
//
// The terms are assumed to filter independently, so the factor of the
// predicate is the product of the factors of its terms. A term that keeps no
// records has the factor math.MaxInt. The product saturates at that value and
// does not overflow, because an overflowed value would make the predicate
// look as if it kept many records.
func reductionFactor(pred query.Predicate, p Plan) int {
	factor := 1
	for _, term := range pred.Terms() {
		factor = saturatingMultiply(factor, termReductionFactor(term, p))
	}

	return factor
}

// termReductionFactor returns the reduction factor of one term.
//
// A field equated with a constant keeps the records with one of the field's
// values. A field equated with another field keeps the records in which the
// two are equal. That fraction is estimated as one over the larger of the two
// distinct value counts. Two constants are either always equal or never
// equal.
func termReductionFactor(term query.Term, p Plan) int {
	lhsName, lhsIsField := fieldNameOf(term.LHS())
	rhsName, rhsIsField := fieldNameOf(term.RHS())

	switch {
	case lhsIsField && rhsIsField:
		return max(p.DistinctValues(lhsName), p.DistinctValues(rhsName))
	case lhsIsField:
		return p.DistinctValues(lhsName)
	case rhsIsField:
		return p.DistinctValues(rhsName)
	}

	lhsConstant := term.LHS().(query.ConstantExpression)
	rhsConstant := term.RHS().(query.ConstantExpression)
	if lhsConstant.Value() == rhsConstant.Value() {
		return 1
	}

	return math.MaxInt
}

// saturatingMultiply returns a * b for positive a and b, or math.MaxInt if
// the product would overflow.
//
// Every factor passed to it is at least 1, because a plan estimates at least
// one distinct value for any field, even for an empty table. The function
// assumes this and does not check it. A factor of 0 would mean that a plan
// broke that guarantee, and silently replacing it would hide the bug.
func saturatingMultiply(a, b int) int {
	if a > math.MaxInt/b {
		return math.MaxInt
	}

	return a * b
}
