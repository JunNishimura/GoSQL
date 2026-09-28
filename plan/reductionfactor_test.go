package plan

import (
	"math"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// fakePlan is a plan that answers only how many distinct values its fields
// hold, which is all a reduction factor asks of one. Its other answers are
// zero, and no case below reads them.
type fakePlan struct {
	distinctValues map[string]int
}

func (fp fakePlan) Open() (query.Scan, error)           { return nil, nil }
func (fp fakePlan) BlocksAccessed() int                 { return 0 }
func (fp fakePlan) RecordsOutput() int                  { return 0 }
func (fp fakePlan) DistinctValues(fieldName string) int { return fp.distinctValues[fieldName] }
func (fp fakePlan) Schema() *recordmanager.Schema       { return nil }

func TestReductionFactor(t *testing.T) {
	// "a" holds 10 distinct values and "b" holds 4. They differ so that a case
	// reading the wrong field, or the smaller of two, gives a wrong answer.
	p := fakePlan{
		distinctValues: map[string]int{
			"a": 10,
			"b": 4,
		},
	}

	field := func(name string) query.Expression {
		return query.NewFieldExpression(name)
	}
	constant := func(val query.Constant) query.Expression {
		return query.NewConstantExpression(val)
	}

	tests := []struct {
		name string
		pred query.Predicate
		want int
	}{
		{
			name: "given a predicate of no terms, when its reduction factor is worked out, then it is 1, since it keeps every record",
			pred: query.NewPredicate(),
			want: 1,
		},
		{
			name: "given a predicate of a field equated with a constant, when its reduction factor is worked out, then it is the distinct values of the field",
			pred: query.NewPredicate(
				query.NewTerm(field("a"), constant(query.NewIntConstant(1))),
			),
			want: 10,
		},
		{
			name: "given a predicate of a constant equated with a field, when its reduction factor is worked out, then it is the distinct values of the field",
			pred: query.NewPredicate(
				query.NewTerm(constant(query.NewIntConstant(1)), field("a")),
			),
			want: 10,
		},
		{
			name: "given a predicate of a field of more distinct values equated with one of fewer, when its reduction factor is worked out, then it is the larger of the two",
			pred: query.NewPredicate(
				query.NewTerm(field("a"), field("b")),
			),
			want: 10,
		},
		{
			name: "given a predicate of a field of fewer distinct values equated with one of more, when its reduction factor is worked out, then it is the larger of the two",
			pred: query.NewPredicate(
				query.NewTerm(field("b"), field("a")),
			),
			want: 10,
		},
		{
			name: "given a predicate of two equal constants, when its reduction factor is worked out, then it is 1, since it keeps every record",
			pred: query.NewPredicate(
				query.NewTerm(constant(query.NewIntConstant(1)), constant(query.NewIntConstant(1))),
			),
			want: 1,
		},
		{
			name: "given a predicate of two unequal constants, when its reduction factor is worked out, then it is math.MaxInt, since it keeps no record",
			pred: query.NewPredicate(
				query.NewTerm(constant(query.NewIntConstant(1)), constant(query.NewIntConstant(2))),
			),
			want: math.MaxInt,
		},
		{
			name: "given a predicate of an int constant equated with a varchar spelling it, when its reduction factor is worked out, then it is math.MaxInt, since constants of two kinds are never equal",
			pred: query.NewPredicate(
				query.NewTerm(constant(query.NewIntConstant(1)), constant(query.NewStringConstant("1"))),
			),
			want: math.MaxInt,
		},
		{
			name: "given a predicate of two terms on fields, when its reduction factor is worked out, then it is the product of the two terms' factors",
			pred: query.NewPredicate(
				query.NewTerm(field("a"), constant(query.NewIntConstant(1))),
				query.NewTerm(field("b"), constant(query.NewIntConstant(1))),
			),
			want: 40,
		},
		// Multiplying math.MaxInt by anything above 1 overflows, and an
		// overflowed factor could come out small or negative, costing a plan
		// that keeps nothing as one that keeps a lot.
		{
			name: "given a predicate of two unequal constants and a term on a field, when its reduction factor is worked out, then it stays at math.MaxInt rather than overflowing",
			pred: query.NewPredicate(
				query.NewTerm(constant(query.NewIntConstant(1)), constant(query.NewIntConstant(2))),
				query.NewTerm(field("a"), constant(query.NewIntConstant(1))),
			),
			want: math.MaxInt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reductionFactor(tt.pred, p); got != tt.want {
				t.Errorf("reductionFactor(%s) = %d, want %d", tt.pred, got, tt.want)
			}
		})
	}
}
