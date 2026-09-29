package plan

import (
	"math"

	"github.com/JunNishimura/GoSQL/query"
)

// reductionFactor is a guess at how many times fewer records pred keeps than
// it is given, when it is applied to the output of p.
//
// It lives here rather than on the predicate because it needs two things kept
// in two places: the shape of each term, which the predicate hands out, and
// how many distinct values a field holds, which only a plan knows.
//
// The terms are taken to filter independently, so the factor of the predicate
// is the product of theirs. A term that keeps nothing is math.MaxInt, and the
// product stops there rather than overflowing into a number that would cost
// the predicate as one that keeps a lot.
func reductionFactor(pred query.Predicate, p Plan) int {
	factor := 1
	for _, term := range pred.Terms() {
		factor = saturatingMultiply(factor, termReductionFactor(term, p))
	}

	return factor
}

// termReductionFactor is the reduction factor of one term.
//
// A field equated with a constant keeps one of the field's values. A field
// equated with another field keeps the records where the two agree, which is
// guessed as one in however many values the larger of the two holds. Two
// constants either always agree or never do.
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

// saturatingMultiply is a * b for positive a and b, or math.MaxInt where that
// would overflow.
//
// Every factor it is given is at least 1, since a plan guesses at least one
// distinct value for any field, even of an empty table. That is taken rather
// than checked: a factor of 0 would mean a plan broke that promise, and
// quietly treating it as some other number would hide where.
func saturatingMultiply(a, b int) int {
	if a > math.MaxInt/b {
		return math.MaxInt
	}

	return a * b
}
