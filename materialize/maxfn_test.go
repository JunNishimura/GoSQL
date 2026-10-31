package materialize

import (
	"errors"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestMaxFnFieldName(t *testing.T) {
	t.Run("given a max made for id, its field name is maxofid", func(t *testing.T) {
		if got, want := NewMaxFn("id").FieldName(), "maxofid"; got != want {
			t.Errorf("FieldName() = %q, want %q", got, want)
		}
	})
}

func TestMaxFnValue(t *testing.T) {
	tests := []struct {
		name string
		// groups are the values of "v" in the records of each group processed,
		// in turn. The value is asked for after the last.
		groups [][]query.Constant
		want   query.Constant
	}{
		{
			name:   "given a group of one record, when its max is asked for, then it is the value of that record",
			groups: [][]query.Constant{{query.NewIntConstant(4)}},
			want:   query.NewIntConstant(4),
		},
		{
			name:   "given a group of ints whose largest is in the middle, when its max is asked for, then it is the largest",
			groups: [][]query.Constant{{query.NewIntConstant(3), query.NewIntConstant(7), query.NewIntConstant(5)}},
			want:   query.NewIntConstant(7),
		},
		{
			name:   "given a group of varchars, when its max is asked for, then it is the last of them in text order",
			groups: [][]query.Constant{{query.NewStringConstant("b"), query.NewStringConstant("c"), query.NewStringConstant("a")}},
			want:   query.NewStringConstant("c"),
		},
		{
			name: "given a group of a large value followed by one of smaller values, when the max is asked for, then it is the largest of the second",
			groups: [][]query.Constant{
				{query.NewIntConstant(9)},
				{query.NewIntConstant(1), query.NewIntConstant(2)},
			},
			want: query.NewIntConstant(2),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMaxFn("v")

			for _, group := range tt.groups {
				if err := f.ProcessFirst(fakeScan{"v": group[0]}); err != nil {
					t.Fatalf("ProcessFirst() error = %v", err)
				}
				for _, val := range group[1:] {
					if err := f.ProcessNext(fakeScan{"v": val}); err != nil {
						t.Fatalf("ProcessNext() error = %v", err)
					}
				}
			}

			if got := f.Value(); got != tt.want {
				t.Errorf("Value() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMaxFnProcessNext(t *testing.T) {
	t.Run("given a group whose first record holds an int, when a record holding a varchar is processed, then it reports ErrConstantTypeMismatch", func(t *testing.T) {
		f := NewMaxFn("v")
		if err := f.ProcessFirst(fakeScan{"v": query.NewIntConstant(1)}); err != nil {
			t.Fatalf("ProcessFirst() error = %v", err)
		}

		err := f.ProcessNext(fakeScan{"v": query.NewStringConstant("1")})
		if !errors.Is(err, query.ErrConstantTypeMismatch) {
			t.Errorf("ProcessNext() error = %v, want %v", err, query.ErrConstantTypeMismatch)
		}
	})
}
