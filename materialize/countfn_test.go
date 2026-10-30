package materialize

import (
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestCountFnFieldName(t *testing.T) {
	t.Run("given a count made for id, its field name is countofid", func(t *testing.T) {
		if got, want := NewCountFn("id").FieldName(), "countofid"; got != want {
			t.Errorf("FieldName() = %q, want %q", got, want)
		}
	})
}

func TestCountFnValue(t *testing.T) {
	tests := []struct {
		name string
		// groups are the number of records of each group processed, in turn.
		// The value is asked for after the last.
		groups []int
		want   int32
	}{
		{
			name:   "given a group of one record, when its count is asked for, then it is 1",
			groups: []int{1},
			want:   1,
		},
		{
			name:   "given a group of three records, when its count is asked for, then it is 3",
			groups: []int{3},
			want:   3,
		},
		{
			name:   "given a group of three records followed by one of two, when the count is asked for, then it is the 2 of the second",
			groups: []int{3, 2},
			want:   2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewCountFn("id")
			s := fakeScan{"id": query.NewIntConstant(1)}

			for _, size := range tt.groups {
				if err := f.ProcessFirst(s); err != nil {
					t.Fatalf("ProcessFirst() error = %v", err)
				}
				for range size - 1 {
					if err := f.ProcessNext(s); err != nil {
						t.Fatalf("ProcessNext() error = %v", err)
					}
				}
			}

			if got, want := f.Value(), query.NewIntConstant(tt.want); got != want {
				t.Errorf("Value() = %s, want %s", got, want)
			}
		})
	}
}
