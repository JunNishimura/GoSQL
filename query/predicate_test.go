package query

import (
	"errors"
	"fmt"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The terms the predicate cases are built from, read against the record
// newTestScanOverOneRecord writes: id 42 and name "alice".
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
		// A predicate of no terms has nothing to read, so it must answer
		// without going to the scan at all.
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
		// Reading stops at the first term that fails, so a term after one that
		// has already settled the answer is never evaluated. The unreadable
		// term here stands for the field reads that are saved: which fields a
		// query names is settled before it runs, so a term that could not be
		// read is not a fault being hidden.
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
		// A predicate of no terms adds no condition, so conjoining leaves the
		// other side answering as it did on its own. Both answers are here
		// because either one alone would also be given by a conjunction that
		// had thrown a side away.
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

	// A predicate is a value, so conjoining builds another one rather than
	// growing the one it was called on. Conjoining twice onto the same
	// predicate is what tells that apart from an implementation that appends
	// into terms the two results share.
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
		// A predicate of no terms is the absence of a where clause, and a
		// caller printing a query writes no "where" for it. Anything else here
		// would be SQL nobody asked for.
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
			want: `id = 42 and name = "alice"`,
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
