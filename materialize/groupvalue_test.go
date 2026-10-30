package materialize

import (
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestGroupValueValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		wantVal   query.Constant
		wantOK    bool
	}{
		{
			name:      "given a group value of id and name read from a record, when id is asked for, then it is the id of the record",
			fieldName: "id",
			wantVal:   query.NewIntConstant(1),
			wantOK:    true,
		},
		{
			name:      "given a group value of id and name read from a record, when name is asked for, then it is the name of the record",
			fieldName: "name",
			wantVal:   query.NewStringConstant("a"),
			wantOK:    true,
		},
		{
			name:      "given a group value of id and name read from a record that also has dept, when dept is asked for, then it is not held",
			fieldName: "dept",
			wantVal:   query.Constant{},
			wantOK:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := fakeScan{
				"id":   query.NewIntConstant(1),
				"name": query.NewStringConstant("a"),
				"dept": query.NewIntConstant(10),
			}
			gv := mustNewGroupValue(t, s, "id", "name")

			gotVal, gotOK := gv.Value(tt.fieldName)
			if gotOK != tt.wantOK {
				t.Fatalf("Value(%q) ok = %v, want %v", tt.fieldName, gotOK, tt.wantOK)
			}
			if gotVal != tt.wantVal {
				t.Errorf("Value(%q) = %s, want %s", tt.fieldName, gotVal, tt.wantVal)
			}
		})
	}
}

func TestGroupValueEquals(t *testing.T) {
	tests := []struct {
		name string
		s1   fakeScan
		s2   fakeScan
		want bool
	}{
		{
			name: "given group values of id and name, when both records hold the same id and name, then they are equal",
			s1:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a"), "dept": query.NewIntConstant(10)},
			s2:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a"), "dept": query.NewIntConstant(10)},
			want: true,
		},
		{
			name: "given group values of id and name, when the records differ only in name, then they are not equal",
			s1:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a"), "dept": query.NewIntConstant(10)},
			s2:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("b"), "dept": query.NewIntConstant(10)},
			want: false,
		},
		{
			name: "given group values of id and name, when the records differ only in dept, which is not grouped on, then they are equal",
			s1:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a"), "dept": query.NewIntConstant(10)},
			s2:   fakeScan{"id": query.NewIntConstant(1), "name": query.NewStringConstant("a"), "dept": query.NewIntConstant(20)},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gv1 := mustNewGroupValue(t, tt.s1, "id", "name")
			gv2 := mustNewGroupValue(t, tt.s2, "id", "name")

			if got := gv1.Equals(gv2); got != tt.want {
				t.Errorf("Equals() = %v, want %v", got, tt.want)
			}
		})
	}
}

func mustNewGroupValue(t *testing.T, s query.Scan, fieldNames ...string) *GroupValue {
	t.Helper()

	gv, err := NewGroupValue(s, fieldNames)
	if err != nil {
		t.Fatalf("NewGroupValue() error = %v", err)
	}

	return gv
}
