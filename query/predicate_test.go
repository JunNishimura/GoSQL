package query

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The terms that the predicate cases are built from. They are evaluated
// against the record that newTestScanOverOneRecord writes: id 42 and name
// "alice".
var (
	termThatHolds       = NewTerm(NewFieldExpression("id"), NewConstantExpression(NewIntConstant(testRecordID)))
	termThatAlsoHolds   = NewTerm(NewFieldExpression("name"), NewConstantExpression(NewStringConstant(testRecordName)))
	termThatDoesNotHold = NewTerm(NewFieldExpression("id"), NewConstantExpression(NewIntConstant(testRecordID+1)))
	termOnAMissingField = NewTerm(NewFieldExpression("missing"), NewConstantExpression(NewIntConstant(testRecordID)))
)

func TestNewPredicate(t *testing.T) {
	t.Run("given a predicate made from a slice of terms, when that slice is written to afterwards, then the predicate is unchanged", func(t *testing.T) {
		terms := []Term{termThatHolds}

		pred := NewPredicate(terms...)
		terms[0] = termThatDoesNotHold

		if got, want := pred.String(), termThatHolds.String(); got != want {
			t.Errorf("String() = %s, want %s: the predicate kept the caller's slice rather than the terms in it", got, want)
		}
	})
}

func TestPredicateIsSatisfied(t *testing.T) {
	tests := []struct {
		name    string
		pred    Predicate
		slot    int
		want    bool
		wantErr error
	}{
		{
			name: "given a predicate of no terms, when it is tested, then it is satisfied, so a query with no where clause keeps every record",
			pred: NewPredicate(),
			slot: 0,
			want: true,
		},
		// A predicate with no terms has nothing to evaluate, so it must
		// return its result without reading the scan.
		{
			name: "given a predicate of no terms and a scan that is on no record, when it is tested, then it is still satisfied",
			pred: NewPredicate(),
			slot: beforeFirstSlot,
			want: true,
		},
		{
			name: "given a predicate of one term that holds, when it is tested, then it is satisfied",
			pred: NewPredicate(termThatHolds),
			slot: 0,
			want: true,
		},
		{
			name: "given a predicate of one term that does not hold, when it is tested, then it is not satisfied",
			pred: NewPredicate(termThatDoesNotHold),
			slot: 0,
			want: false,
		},
		{
			name: "given a predicate of two terms that both hold, when it is tested, then it is satisfied",
			pred: NewPredicate(termThatHolds, termThatAlsoHolds),
			slot: 0,
			want: true,
		},
		{
			name: "given a predicate of two terms whose second does not hold, when it is tested, then it is not satisfied",
			pred: NewPredicate(termThatHolds, termThatDoesNotHold),
			slot: 0,
			want: false,
		},
		{
			name:    "given a predicate whose term names a field the table does not have, when it is tested, then ErrFieldNotFound reaches the caller",
			pred:    NewPredicate(termOnAMissingField),
			slot:    0,
			wantErr: recordmanager.ErrFieldNotFound,
		},
		// Evaluation stops at the first term that fails, so a term after it is
		// never evaluated. The unreadable term here represents the field reads
		// that are saved. The fields of a query are validated before it runs,
		// so skipping a term that could not be read does not hide a real
		// error.
		{
			name: "given a predicate whose first term does not hold and whose second names a field the table does not have, when it is tested, then it is not satisfied rather than reporting the missing field",
			pred: NewPredicate(termThatDoesNotHold, termOnAMissingField),
			slot: 0,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestScanOverOneRecord(t, tt.slot)

			got, err := tt.pred.IsSatisfied(ts)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("IsSatisfied() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("IsSatisfied() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsSatisfied() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestPredicateConjoinWith(t *testing.T) {
	tests := []struct {
		name  string
		left  Predicate
		right Predicate
		want  bool
	}{
		{
			name:  "given two predicates whose terms all hold, when they are conjoined, then the result is satisfied",
			left:  NewPredicate(termThatHolds),
			right: NewPredicate(termThatAlsoHolds),
			want:  true,
		},
		{
			name:  "given two predicates the second of whose terms does not hold, when they are conjoined, then the result is not satisfied",
			left:  NewPredicate(termThatHolds),
			right: NewPredicate(termThatDoesNotHold),
			want:  false,
		},
		{
			name:  "given two predicates the first of whose terms does not hold, when they are conjoined, then the result is not satisfied",
			left:  NewPredicate(termThatDoesNotHold),
			right: NewPredicate(termThatAlsoHolds),
			want:  false,
		},
		// A predicate with no terms adds no condition, so the conjunction
		// gives the same results as the other side alone. Both results are
		// checked, because a conjunction that dropped one side would also
		// produce either result alone.
		{
			name:  "given a predicate of no terms, when a predicate of one term that does not hold is conjoined onto it, then the result is not satisfied",
			left:  NewPredicate(),
			right: NewPredicate(termThatDoesNotHold),
			want:  false,
		},
		{
			name:  "given a predicate of no terms, when a predicate of one term that holds is conjoined onto it, then the result is satisfied",
			left:  NewPredicate(),
			right: NewPredicate(termThatHolds),
			want:  true,
		},
		{
			name:  "given a predicate of one term that holds, when a predicate of no terms is conjoined onto it, then the result is still satisfied",
			left:  NewPredicate(termThatHolds),
			right: NewPredicate(),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestScanOverOneRecord(t, 0)

			got, err := tt.left.ConjoinWith(tt.right).IsSatisfied(ts)
			if err != nil {
				t.Fatalf("IsSatisfied() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("IsSatisfied() = %t, want %t", got, tt.want)
			}
		})
	}

	// A predicate is a value, so ConjoinWith creates a new predicate and does
	// not modify its receiver. Conjoining the same predicate twice
	// distinguishes this from an implementation that appends to terms that
	// both results share.
	t.Run("given one predicate conjoined with two others in turn, then neither result holds the other's terms and the predicate conjoined onto is unchanged", func(t *testing.T) {
		base := NewPredicate(termThatHolds)

		first := base.ConjoinWith(NewPredicate(termThatAlsoHolds))
		second := base.ConjoinWith(NewPredicate(termThatDoesNotHold))

		if got, want := first.String(), fmt.Sprintf("%s and %s", termThatHolds, termThatAlsoHolds); got != want {
			t.Errorf("the first conjunction = %s, want %s: building the second wrote over its terms", got, want)
		}
		if got, want := second.String(), fmt.Sprintf("%s and %s", termThatHolds, termThatDoesNotHold); got != want {
			t.Errorf("the second conjunction = %s, want %s", got, want)
		}
		if got, want := base.String(), termThatHolds.String(); got != want {
			t.Errorf("the predicate conjoined onto = %s, want %s: conjoining changed it", got, want)
		}
	})
}

func TestPredicateString(t *testing.T) {
	tests := []struct {
		name string
		pred Predicate
		want string
	}{
		// A predicate with no terms represents a missing where clause, and
		// code that prints a query omits "where" for it. Any other output
		// here would be SQL that nobody wrote.
		{
			name: "given a predicate of no terms, it is empty, since there is no condition to write",
			pred: NewPredicate(),
			want: "",
		},
		{
			name: "given a predicate of one term, it is that term on its own",
			pred: NewPredicate(termThatHolds),
			want: "id = 42",
		},
		{
			name: "given a predicate of two terms, they are written in the order they were given with and between them",
			pred: NewPredicate(termThatHolds, termThatAlsoHolds),
			want: `id = 42 and name = 'alice'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pred.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestPredicateTerms(t *testing.T) {
	t.Run("given a predicate of two terms, when its terms are asked for, then they are the two it was made from, in order", func(t *testing.T) {
		pred := NewPredicate(termThatHolds, termThatAlsoHolds)

		want := []Term{termThatHolds, termThatAlsoHolds}
		if got := pred.Terms(); !slices.Equal(got, want) {
			t.Errorf("Terms() = %v, want %v", got, want)
		}
	})

	t.Run("given a predicate of no terms, when its terms are asked for, then there are none", func(t *testing.T) {
		if got := NewPredicate().Terms(); len(got) != 0 {
			t.Errorf("Terms() = %v, want none", got)
		}
	})

	// A planner splits a predicate through Terms, so the returned slice must
	// not be the slice that the predicate itself reads.
	t.Run("given a predicate, when the slice of its terms is written to, then the predicate is unchanged", func(t *testing.T) {
		pred := NewPredicate(termThatHolds)

		pred.Terms()[0] = termThatDoesNotHold

		if got, want := pred.String(), termThatHolds.String(); got != want {
			t.Errorf("String() = %s, want %s: the predicate handed out the slice it reads from", got, want)
		}
	})
}

func TestPredicateSelectSubPred(t *testing.T) {
	tests := []struct {
		name      string
		pred      Predicate
		wantTerms []Term
		wantOK    bool
	}{
		{
			name:      "given a schema with id and name fields, when the sub-predicate of a predicate whose terms all name its fields is taken, then it is every term, in order",
			pred:      NewPredicate(termThatHolds, termThatAlsoHolds),
			wantTerms: []Term{termThatHolds, termThatAlsoHolds},
			wantOK:    true,
		},
		// The term that the schema cannot evaluate is between the two that it
		// can. Its removal must keep the order of the other two.
		{
			name:      "given a schema with id and name fields, when the sub-predicate of a predicate with a term naming a field it does not have is taken, then that term is left out and the rest keep their order",
			pred:      NewPredicate(termThatHolds, termOnAMissingField, termThatAlsoHolds),
			wantTerms: []Term{termThatHolds, termThatAlsoHolds},
			wantOK:    true,
		},
		// "No terms to test" is distinguished from a predicate with no terms,
		// which holds for every record. A planner must treat it as "no
		// select", not as a select that keeps every record.
		{
			name:   "given a schema with id and name fields, when the sub-predicate of a predicate whose only term names a field it does not have is taken, then there is none",
			pred:   NewPredicate(termOnAMissingField),
			wantOK: false,
		},
		{
			name:   "given a schema with id and name fields, when the sub-predicate of a predicate of no terms is taken, then there is none",
			pred:   NewPredicate(),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := newTestLayout(t).Schema()

			got, ok := tt.pred.SelectSubPred(schema)
			if ok != tt.wantOK {
				t.Fatalf("SelectSubPred() ok = %t, want %t", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !slices.Equal(got.Terms(), tt.wantTerms) {
				t.Errorf("SelectSubPred() = %s, want %s", got, NewPredicate(tt.wantTerms...))
			}
		})
	}
}

// newTestDeptSchema builds the schema of a table that is joined to the table
// of newTestLayout. A join term equates "dept_id" with "id". "dept_name"
// exists so that the schema has more than the join field.
func newTestDeptSchema(t *testing.T) *recordmanager.Schema {
	t.Helper()

	s := recordmanager.NewSchema()
	if err := s.AddIntField("dept_id"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "dept_id", err)
	}
	if err := s.AddStringField("dept_name", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "dept_name", err)
	}

	return s
}

func TestPredicateJoinSubPred(t *testing.T) {
	var (
		joinTerm          = NewTerm(NewFieldExpression("id"), NewFieldExpression("dept_id"))
		reversedJoinTerm  = NewTerm(NewFieldExpression("dept_name"), NewFieldExpression("name"))
		deptTerm          = NewTerm(NewFieldExpression("dept_id"), NewConstantExpression(NewIntConstant(1)))
		termOnAThirdTable = NewTerm(NewFieldExpression("id"), NewFieldExpression("missing"))
	)

	tests := []struct {
		name      string
		pred      Predicate
		wantTerms []Term
		wantOK    bool
	}{
		{
			name:      "given a schema with id and name fields and one with dept_id and dept_name, when the sub-predicate of a predicate equating id with dept_id is taken, then it is that term",
			pred:      NewPredicate(joinTerm),
			wantTerms: []Term{joinTerm},
			wantOK:    true,
		},
		// One schema alone can evaluate each of the other terms here. They
		// are tested while each table is read, and do not belong to the join.
		{
			name:      "given a schema with id and name fields and one with dept_id and dept_name, when the sub-predicate of a predicate mixing join terms with terms on one schema is taken, then it is the join terms in order, with a field of the second schema on either side",
			pred:      NewPredicate(termThatHolds, joinTerm, deptTerm, reversedJoinTerm),
			wantTerms: []Term{joinTerm, reversedJoinTerm},
			wantOK:    true,
		},
		{
			name:   "given a schema with id and name fields and one with dept_id and dept_name, when the sub-predicate of a predicate whose terms each name fields of one schema is taken, then there is none",
			pred:   NewPredicate(termThatHolds, deptTerm),
			wantOK: false,
		},
		// Neither schema can evaluate this term alone, and the two joined
		// cannot either, because one of its fields is in neither. The term
		// belongs to a join with a third table.
		{
			name:   "given a schema with id and name fields and one with dept_id and dept_name, when the sub-predicate of a predicate equating id with a field neither has is taken, then there is none",
			pred:   NewPredicate(termOnAThirdTable),
			wantOK: false,
		},
		{
			name:   "given a schema with id and name fields and one with dept_id and dept_name, when the sub-predicate of a predicate of no terms is taken, then there is none",
			pred:   NewPredicate(),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema1 := newTestLayout(t).Schema()
			schema2 := newTestDeptSchema(t)

			got, ok := tt.pred.JoinSubPred(schema1, schema2)
			if ok != tt.wantOK {
				t.Fatalf("JoinSubPred() ok = %t, want %t", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if !slices.Equal(got.Terms(), tt.wantTerms) {
				t.Errorf("JoinSubPred() = %s, want %s", got, NewPredicate(tt.wantTerms...))
			}
		})
	}
}
