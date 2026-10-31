package materialize

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// deptRecord is a record of the table the group by tests group: an int "dept"
// and a varchar "name".
type deptRecord struct {
	dept int32
	name string
}

func TestGroupByScan(t *testing.T) {
	tests := []struct {
		name        string
		records     []deptRecord
		groupFields []string
		want        []string
	}{
		{
			name: "given records sorted by dept, when they are grouped by dept, then each dept comes back once with the count and the largest name of its records",
			records: []deptRecord{
				{10, "b"}, {10, "c"}, {10, "a"},
				{20, "x"},
				{30, "m"}, {30, "n"},
			},
			groupFields: []string{"dept"},
			want:        []string{"10:3:'c'", "20:1:'x'", "30:2:'n'"},
		},
		{
			name: "given records all of one dept, when they are grouped by dept, then they come back as one group",
			records: []deptRecord{
				{10, "b"}, {10, "a"},
			},
			groupFields: []string{"dept"},
			want:        []string{"10:2:'b'"},
		},
		{
			name:        "given no records, when they are grouped by dept, then no groups come back",
			records:     []deptRecord{},
			groupFields: []string{"dept"},
			want:        []string{},
		},
		{
			name: "given records of several depts, when they are grouped on no fields, then they all come back as one group",
			records: []deptRecord{
				{10, "b"}, {20, "c"}, {30, "a"},
			},
			groupFields: []string{},
			want:        []string{"-:3:'c'"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewGroupByScan(t, tx, tt.records, tt.groupFields)
			defer s.Close()

			if got := readGroups(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("read groups %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("given records of two depts grouped by dept, when the scan is read to the end and moved before its first record, then it reads both groups again", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewGroupByScan(t, tx, []deptRecord{{10, "a"}, {10, "b"}, {20, "c"}}, []string{"dept"})
		defer s.Close()

		readGroups(t, s)

		want := []string{"10:2:'b'", "20:1:'c'"}
		if got := readGroups(t, s); !slices.Equal(got, want) {
			t.Errorf("read groups %v, want %v", got, want)
		}
	})
}

func TestGroupByScanGetValue(t *testing.T) {
	t.Run("given a scan grouped by dept, when a field it neither groups on nor aggregates is read, then it reports ErrFieldNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewGroupByScan(t, tx, []deptRecord{{10, "a"}}, []string{"dept"})
		defer s.Close()

		if _, err := s.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}

		_, err := s.GetValue("name")
		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("GetValue(%q) error = %v, want %v", "name", err, recordmanager.ErrFieldNotFound)
		}
	})

	t.Run("given a scan grouped by dept, when dept is read before the first move, then it reports ErrNoCurrentRecord", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewGroupByScan(t, tx, []deptRecord{{10, "a"}}, []string{"dept"})
		defer s.Close()

		_, err := s.GetValue("dept")
		if !errors.Is(err, query.ErrNoCurrentRecord) {
			t.Errorf("GetValue(%q) error = %v, want %v", "dept", err, query.ErrNoCurrentRecord)
		}
	})
}

func TestGroupByScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a scan grouping by dept with a count and a max of name, when it is asked for dept, then it has it",
			fieldName: "dept",
			want:      true,
		},
		{
			name:      "given a scan grouping by dept with a count and a max of name, when it is asked for countofname, then it has it",
			fieldName: "countofname",
			want:      true,
		},
		{
			name:      "given a scan grouping by dept with a count and a max of name, when it is asked for maxofname, then it has it",
			fieldName: "maxofname",
			want:      true,
		},
		{
			name:      "given a scan grouping by dept with a count and a max of name, when it is asked for name, then it does not have it",
			fieldName: "name",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewGroupByScan(t, tx, []deptRecord{{10, "a"}}, []string{"dept"})
			defer s.Close()

			if got := s.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

// newDeptTable makes a temp table of dept and name holding the records, in the
// order given.
func newDeptTable(t *testing.T, tx *transaction.Transaction, records []deptRecord) *TempTable {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("dept"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "dept", err)
	}
	if err := schema.AddStringField("name", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "name", err)
	}

	tt := NewTempTable(tx, schema)
	s, err := tt.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	for _, r := range records {
		if err := s.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := s.SetInt("dept", r.dept); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := s.SetString("name", r.name); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}

	return tt
}

// mustNewGroupByScan opens a group by over a table of the records, on
// groupFields, with a count and a max of name.
func mustNewGroupByScan(t *testing.T, tx *transaction.Transaction, records []deptRecord, groupFields []string) *GroupByScan {
	t.Helper()

	src, err := newDeptTable(t, tx, records).Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	aggFns := []AggregationFn{NewCountFn("name"), NewMaxFn("name")}
	s, err := NewGroupByScan(src, groupFields, aggFns)
	if err != nil {
		src.Close()
		t.Fatalf("NewGroupByScan() error = %v", err)
	}
	if s == nil {
		src.Close()
		t.Fatal("NewGroupByScan() = nil, want a scan")
	}

	return s
}

// readGroups reads every group a scan of mustNewGroupByScan gives back from its
// start as "dept:count:max", with "-" for the dept of a scan that does not
// group on it.
func readGroups(t *testing.T, s *GroupByScan) []string {
	t.Helper()

	if err := s.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	got := []string{}
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return got
		}

		dept := "-"
		if s.HasField("dept") {
			d, err := s.GetInt("dept")
			if err != nil {
				t.Fatalf("GetInt(%q) error = %v", "dept", err)
			}
			dept = fmt.Sprint(d)
		}
		count, err := s.GetInt("countofname")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "countofname", err)
		}
		max, err := s.GetString("maxofname")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "maxofname", err)
		}
		got = append(got, fmt.Sprintf("%s:%d:'%s'", dept, count, max))
	}
}
