package query

import (
	"errors"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The record that every expression test reads. The two fields have different
// types, and the int differs from every literal in these tests. An expression
// that read the wrong source therefore returns a wrong value, and cannot
// return the right one by chance.
const (
	testRecordID   = 42
	testRecordName = "alice"
)

// newTestScanOverOneRecord opens a scan of a table with one record at slot 0,
// and positions the scan at slot.
//
// With slot 0, the scan is on that record. With beforeFirstSlot, the scan is
// on no record. In that state, an expression that reads the scan behaves
// differently from one that does not.
func newTestScanOverOneRecord(t *testing.T, slot int) *TableScan {
	t.Helper()

	ts := newTestTableScanAt(t, 0)

	if err := ts.SetInt("id", testRecordID); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	if err := ts.SetString("name", testRecordName); err != nil {
		t.Fatalf("SetString() error = %v", err)
	}
	ts.currentSlot = slot

	return ts
}

func TestFieldExpressionEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		slot      int
		want      Constant
		wantErr   error
	}{
		{
			name:      "given a scan on a record, when an expression naming its int field is evaluated, then it is that field as an int constant",
			fieldName: "id",
			slot:      0,
			want:      NewIntConstant(testRecordID),
		},
		{
			name:      "given a scan on a record, when an expression naming its varchar field is evaluated, then it is that field as a varchar constant",
			fieldName: "name",
			slot:      0,
			want:      NewStringConstant(testRecordName),
		},
		// The expression adds no check of its own. An error from the scan
		// must therefore reach the caller, and must not be replaced by a
		// value.
		{
			name:      "given a field the table does not have, when the expression is evaluated, then ErrFieldNotFound reaches the caller",
			fieldName: "missing",
			slot:      0,
			wantErr:   recordmanager.ErrFieldNotFound,
		},
		{
			name:      "given a scan that is on no record, when the expression is evaluated, then ErrNoCurrentRecord reaches the caller",
			fieldName: "id",
			slot:      beforeFirstSlot,
			wantErr:   ErrNoCurrentRecord,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestScanOverOneRecord(t, tt.slot)

			got, err := NewFieldExpression(tt.fieldName).Evaluate(ts)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Evaluate() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Evaluate() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestConstantExpressionEvaluate(t *testing.T) {
	tests := []struct {
		name string
		val  Constant
	}{
		{
			name: "given an int constant, when the expression is evaluated against a record, then it is the constant rather than the record's int field",
			val:  NewIntConstant(7),
		},
		{
			name: "given a varchar constant, when the expression is evaluated against a record, then it is the constant rather than the record's varchar field",
			val:  NewStringConstant("bob"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestScanOverOneRecord(t, 0)

			got, err := NewConstantExpression(tt.val).Evaluate(ts)
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got != tt.val {
				t.Errorf("Evaluate() = %s, want %s", got, tt.val)
			}
		})
	}

	// A literal has the same value for every record, so its evaluation must not
	// read the scan. A scan on no record shows this: a field expression fails
	// there, and a constant expression must not.
	t.Run("given a scan that is on no record, when a constant expression is evaluated, then it is still the constant it holds", func(t *testing.T) {
		ts := newTestScanOverOneRecord(t, beforeFirstSlot)

		want := NewIntConstant(7)

		got, err := NewConstantExpression(want).Evaluate(ts)
		if err != nil {
			t.Fatalf("Evaluate() error = %v", err)
		}
		if got != want {
			t.Errorf("Evaluate() = %s, want %s", got, want)
		}
	})
}

func TestFieldExpressionString(t *testing.T) {
	t.Run("it is the field name on its own, so it reads as the column it names", func(t *testing.T) {
		const fieldName = "name"

		if got := NewFieldExpression(fieldName).String(); got != fieldName {
			t.Errorf("String() = %s, want %s", got, fieldName)
		}
	})
}

// A printed predicate must show which side of a term is a column and which is
// a value, and the two can have the same spelling. The constant keeps its
// quotes and the field name has none, which distinguishes them.
func TestConstantExpressionString(t *testing.T) {
	tests := []struct {
		name string
		val  Constant
		want string
	}{
		{
			name: "given a varchar constant whose text is the name of a field, it is quoted, so it does not read as that field",
			val:  NewStringConstant("name"),
			want: `'name'`,
		},
		{
			name: "given an int constant, it is the number on its own",
			val:  NewIntConstant(testRecordID),
			want: "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewConstantExpression(tt.val).String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestFieldExpressionFieldName(t *testing.T) {
	t.Run("it is the name of the field the expression was made from", func(t *testing.T) {
		if got, want := NewFieldExpression("id").FieldName(), "id"; got != want {
			t.Errorf("FieldName() = %q, want %q", got, want)
		}
	})
}

func TestConstantExpressionValue(t *testing.T) {
	t.Run("it is the constant the expression was made from", func(t *testing.T) {
		val := NewIntConstant(testRecordID)

		if got := NewConstantExpression(val).Value(); got != val {
			t.Errorf("Value() = %v, want %v", got, val)
		}
	})
}

func TestFieldExpressionAppliesTo(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a schema with an id field, when an expression naming id is checked against it, then it applies",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a schema with an id field, when an expression naming a field it does not have is checked against it, then it does not apply",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := newTestLayout(t).Schema()

			if got := NewFieldExpression(tt.fieldName).AppliesTo(schema); got != tt.want {
				t.Errorf("AppliesTo() = %t, want %t", got, tt.want)
			}
		})
	}
}

// A literal reads nothing from a record, so it can be evaluated against any
// schema, including an empty one.
func TestConstantExpressionAppliesTo(t *testing.T) {
	t.Run("given a schema with no fields, when a constant expression is checked against it, then it applies", func(t *testing.T) {
		schema := recordmanager.NewSchema()

		if got := NewConstantExpression(NewIntConstant(7)).AppliesTo(schema); !got {
			t.Errorf("AppliesTo() = %t, want true", got)
		}
	})
}
