package materialize

import (
	"errors"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestRecordComparatorCompare(t *testing.T) {
	tests := []struct {
		name       string
		fieldNames []string
		s1         fakeScan
		s2         fakeScan
		want       int
	}{
		{
			name:       "given records ordered by id then name, when their ids differ, then the smaller id comes first whatever their names",
			fieldNames: []string{"id", "name"},
			s1:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("b")},
			s2:         fakeScan{"id": query.NewIntConstant(2), "name": query.NewStringConstant("a")},
			want:       -1,
		},
		{
			name:       "given records ordered by id then name, when their ids are equal, then the names decide",
			fieldNames: []string{"id", "name"},
			s1:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("b")},
			s2:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a")},
			want:       1,
		},
		{
			name:       "given records ordered by name then id, when their names differ, then the names decide whatever their ids",
			fieldNames: []string{"name", "id"},
			s1:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("b")},
			s2:         fakeScan{"id": query.NewIntConstant(2), "name": query.NewStringConstant("a")},
			want:       1,
		},
		{
			name:       "given records ordered by id then name, when both fields are equal, then neither comes first",
			fieldNames: []string{"id", "name"},
			s1:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a")},
			s2:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a")},
			want:       0,
		},
		{
			name:       "given records ordered by id only, when they differ only in name, then neither comes first",
			fieldNames: []string{"id"},
			s1:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("b")},
			s2:         fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a")},
			want:       0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRecordComparator(tt.fieldNames...).Compare(tt.s1, tt.s2)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Compare() = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("given records ordered by id, when one holds an int and the other a varchar in it, then it reports ErrConstantTypeMismatch", func(t *testing.T) {
		s1 := fakeScan{"id": query.NewIntConstant(1)}
		s2 := fakeScan{"id": query.NewStringConstant("1")}

		_, err := NewRecordComparator("id").Compare(s1, s2)
		if !errors.Is(err, query.ErrConstantTypeMismatch) {
			t.Errorf("Compare() error = %v, want %v", err, query.ErrConstantTypeMismatch)
		}
	})
}
