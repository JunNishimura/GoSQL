package query

import (
	"errors"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The record every expression test reads from. The two fields differ in kind,
// and the int is not the number any literal in these tests holds, so an
// expression that went to the wrong place comes back with the wrong value
// rather than with the right one by chance.
const (
	testRecordID   = 42
	testRecordName = "alice"
)

// newTestScanOverOneRecord opens a scan over a table holding one record,
// written at slot 0, and leaves the scan at slot.
//
// Passing 0 puts the scan on that record. Passing beforeFirstSlot puts it on
// none, which is where an expression that reads the scan and one that does not
// come apart.
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
		// The expression adds no checking of its own, so what the scan refuses
		// has to reach the caller rather than come back as a value of some
		// kind.
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

	// A literal is the same value wherever it is read, so evaluating one must
	// not go to the scan at all. A scan that is on no record is where that
	// shows: a field expression fails there, and this one cannot.
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

// A printed predicate has to say which side of a term is a column and which is
// a value, and the two can be spelled the same. The constant keeps its own
// quoting, and the field name has none, so the pair is what tells them apart.
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
