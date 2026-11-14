package query

import (
	"errors"
	"strconv"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

func TestTermIsSatisfied(t *testing.T) {
	tests := []struct {
		name    string
		lhs     Expression
		rhs     Expression
		slot    int
		want    bool
		wantErr error
	}{
		{
			name: "given a term of a field and a constant equal to what the record holds there, when it is tested, then it is satisfied",
			lhs:  NewFieldExpression("id"),
			rhs:  NewConstantExpression(NewIntConstant(testRecordID)),
			slot: 0,
			want: true,
		},
		{
			name: "given a term of a field and a constant unequal to what the record holds there, when it is tested, then it is not satisfied",
			lhs:  NewFieldExpression("id"),
			rhs:  NewConstantExpression(NewIntConstant(testRecordID + 1)),
			slot: 0,
			want: false,
		},
		{
			name: "given a term of a varchar field and the text the record holds there, when it is tested, then it is satisfied",
			lhs:  NewFieldExpression("name"),
			rhs:  NewConstantExpression(NewStringConstant(testRecordName)),
			slot: 0,
			want: true,
		},
		{
			name: "given a term of one field against itself, when it is tested, then it is satisfied",
			lhs:  NewFieldExpression("id"),
			rhs:  NewFieldExpression("id"),
			slot: 0,
			want: true,
		},
		// Values of the two types are never equal. A term that joins two
		// tables on fields of different types matches nothing. It does not
		// match where the text spells the number.
		{
			name: "given a term of an int field and a varchar field of the same record, when it is tested, then it is not satisfied",
			lhs:  NewFieldExpression("id"),
			rhs:  NewFieldExpression("name"),
			slot: 0,
			want: false,
		},
		{
			name: "given a term of an int field and a varchar constant spelling the same digits, when it is tested, then it is not satisfied",
			lhs:  NewFieldExpression("id"),
			rhs:  NewConstantExpression(NewStringConstant(strconv.Itoa(testRecordID))),
			slot: 0,
			want: false,
		},
		// Neither side reads the scan, so a scan on no record must not prevent
		// the evaluation of the term.
		{
			name: "given a term of two equal constants and a scan that is on no record, when it is tested, then it is satisfied",
			lhs:  NewConstantExpression(NewIntConstant(7)),
			rhs:  NewConstantExpression(NewIntConstant(7)),
			slot: beforeFirstSlot,
			want: true,
		},
		// Both sides must be evaluated, so an error from either side must be
		// returned, and the other side must not decide the result.
		{
			name:    "given a term whose left side names a field the table does not have, when it is tested, then ErrFieldNotFound reaches the caller",
			lhs:     NewFieldExpression("missing"),
			rhs:     NewConstantExpression(NewIntConstant(testRecordID)),
			slot:    0,
			wantErr: recordmanager.ErrFieldNotFound,
		},
		{
			name:    "given a term whose right side names a field the table does not have, when it is tested, then ErrFieldNotFound reaches the caller",
			lhs:     NewFieldExpression("id"),
			rhs:     NewFieldExpression("missing"),
			slot:    0,
			wantErr: recordmanager.ErrFieldNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestScanOverOneRecord(t, tt.slot)

			got, err := NewTerm(tt.lhs, tt.rhs).IsSatisfied(ts)

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

func TestTermString(t *testing.T) {
	tests := []struct {
		name string
		term Term
		want string
	}{
		{
			name: "given a term of a field and an int constant, it is the two sides with an equals sign between them",
			term: NewTerm(NewFieldExpression("id"), NewConstantExpression(NewIntConstant(testRecordID))),
			want: "id = 42",
		},
		// Without its quotes, the right side would look like a second field,
		// and "name = alice" is a different term from this one.
		{
			name: "given a term of a field and a varchar constant, the constant keeps its quotes, so the two sides do not read as two fields",
			term: NewTerm(NewFieldExpression("name"), NewConstantExpression(NewStringConstant(testRecordName))),
			want: `name = 'alice'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.term.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestTermLHS(t *testing.T) {
	t.Run("it is the left-hand expression the term was made from", func(t *testing.T) {
		lhs := NewFieldExpression("id")
		rhs := NewConstantExpression(NewIntConstant(testRecordID))

		if got := NewTerm(lhs, rhs).LHS(); got != lhs {
			t.Errorf("LHS() = %v, want %v", got, lhs)
		}
	})
}

func TestTermRHS(t *testing.T) {
	t.Run("it is the right-hand expression the term was made from", func(t *testing.T) {
		lhs := NewFieldExpression("id")
		rhs := NewConstantExpression(NewIntConstant(testRecordID))

		if got := NewTerm(lhs, rhs).RHS(); got != rhs {
			t.Errorf("RHS() = %v, want %v", got, rhs)
		}
	})
}

func TestTermAppliesTo(t *testing.T) {
	tests := []struct {
		name string
		lhs  Expression
		rhs  Expression
		want bool
	}{
		{
			name: "given a schema with id and name fields, when a term of id and a constant is checked against it, then it applies",
			lhs:  NewFieldExpression("id"),
			rhs:  NewConstantExpression(NewIntConstant(testRecordID)),
			want: true,
		},
		{
			name: "given a schema with id and name fields, when a term of id and name is checked against it, then it applies",
			lhs:  NewFieldExpression("id"),
			rhs:  NewFieldExpression("name"),
			want: true,
		},
		{
			name: "given a schema with id and name fields, when a term of two constants is checked against it, then it applies",
			lhs:  NewConstantExpression(NewIntConstant(1)),
			rhs:  NewConstantExpression(NewIntConstant(1)),
			want: true,
		},
		// Each of the two cases below omits one side from the schema. A term
		// that checked only one of its sides would get one of them wrong.
		{
			name: "given a schema with id and name fields, when a term of id and a field it does not have is checked against it, then it does not apply",
			lhs:  NewFieldExpression("id"),
			rhs:  NewFieldExpression("missing"),
			want: false,
		},
		{
			name: "given a schema with id and name fields, when a term of a field it does not have and id is checked against it, then it does not apply",
			lhs:  NewFieldExpression("missing"),
			rhs:  NewFieldExpression("id"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := newTestLayout(t).Schema()

			if got := NewTerm(tt.lhs, tt.rhs).AppliesTo(schema); got != tt.want {
				t.Errorf("AppliesTo() = %t, want %t", got, tt.want)
			}
		})
	}
}
