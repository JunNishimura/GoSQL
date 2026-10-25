package plan

import (
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestConstantEquatedWith(t *testing.T) {
	fieldEqualsConstant := func(fieldName string, c query.Constant) query.Term {
		return query.NewTerm(query.NewFieldExpression(fieldName), query.NewConstantExpression(c))
	}

	tests := []struct {
		name      string
		pred      query.Predicate
		fieldName string
		want      query.Constant
		wantOK    bool
	}{
		{
			name:      "given a predicate of id = 7, when the constant id is equated with is asked for, then it is 7",
			pred:      query.NewPredicate(fieldEqualsConstant("id", query.NewIntConstant(7))),
			fieldName: "id",
			want:      query.NewIntConstant(7),
			wantOK:    true,
		},
		{
			name:      "given a predicate of 7 = id, with the constant on the left, when the constant id is equated with is asked for, then it is 7",
			pred:      query.NewPredicate(query.NewTerm(query.NewConstantExpression(query.NewIntConstant(7)), query.NewFieldExpression("id"))),
			fieldName: "id",
			want:      query.NewIntConstant(7),
			wantOK:    true,
		},
		{
			name:      "given a predicate of name = 'x' and id = 7, when the constant id is equated with is asked for, then it is 7, from the second term",
			pred:      query.NewPredicate(fieldEqualsConstant("name", query.NewStringConstant("x")), fieldEqualsConstant("id", query.NewIntConstant(7))),
			fieldName: "id",
			want:      query.NewIntConstant(7),
			wantOK:    true,
		},
		{
			name:      "given a predicate of id = 7 and id = 8, when the constant id is equated with is asked for, then it is 7, from the first term",
			pred:      query.NewPredicate(fieldEqualsConstant("id", query.NewIntConstant(7)), fieldEqualsConstant("id", query.NewIntConstant(8))),
			fieldName: "id",
			want:      query.NewIntConstant(7),
			wantOK:    true,
		},
		{
			name:      "given a predicate of id = other, which equates id with a field, when the constant id is equated with is asked for, then there is none",
			pred:      query.NewPredicate(query.NewTerm(query.NewFieldExpression("id"), query.NewFieldExpression("other"))),
			fieldName: "id",
			wantOK:    false,
		},
		{
			name:      "given a predicate of name = 'x', when the constant id is equated with is asked for, then there is none",
			pred:      query.NewPredicate(fieldEqualsConstant("name", query.NewStringConstant("x"))),
			fieldName: "id",
			wantOK:    false,
		},
		{
			name:      "given a predicate of no terms, when the constant id is equated with is asked for, then there is none",
			pred:      query.NewPredicate(),
			fieldName: "id",
			wantOK:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := constantEquatedWith(tt.pred, tt.fieldName)
			if ok != tt.wantOK {
				t.Fatalf("constantEquatedWith(%s, %q) ok = %t, want %t", tt.pred, tt.fieldName, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("constantEquatedWith(%s, %q) = %s, want %s", tt.pred, tt.fieldName, got, tt.want)
			}
		})
	}
}
