package plan

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

func TestNewGroupByPlan(t *testing.T) {
	tests := []struct {
		name        string
		groupFields []string
		aggFns      []materialize.AggregationFn
	}{
		{
			name:        "given a plan of id and name, when it is grouped on a field it does not have, then it reports ErrFieldNotFound",
			groupFields: []string{"missing"},
			aggFns:      []materialize.AggregationFn{materialize.NewCountFn("name")},
		},
		{
			name:        "given a plan of id and name, when the max of a field it does not have is asked for, then it reports ErrFieldNotFound",
			groupFields: []string{"id"},
			aggFns:      []materialize.AggregationFn{materialize.NewMaxFn("missing")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewGroupByPlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t), tt.groupFields, tt.aggFns)
			if !errors.Is(err, recordmanager.ErrFieldNotFound) {
				t.Errorf("NewGroupByPlan() error = %v, want %v", err, recordmanager.ErrFieldNotFound)
			}
		})
	}
}

func TestGroupByPlanOpen(t *testing.T) {
	tests := []struct {
		name        string
		ids         []int32
		groupFields []string
		want        []string
	}{
		{
			name:        "given a table of ids out of order, some repeated, when it is grouped on id, then each id comes back once, in order, with the count and the largest name of its records",
			ids:         []int32{3, 1, 3, 2, 1, 3},
			groupFields: []string{"id"},
			want:        []string{"1:2:name1", "2:1:name2", "3:3:name3"},
		},
		{
			name:        "given a table of ids out of order, when it is grouped on no fields, then all its records come back as one group",
			ids:         []int32{3, 1, 3, 2, 1, 3},
			groupFields: []string{},
			want:        []string{"-:6:name3"},
		},
		{
			name:        "given an empty table, when it is grouped on id, then no groups come back",
			ids:         []int32{},
			groupFields: []string{"id"},
			want:        []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTableOfIDs(t, tx, mm, tt.ids)

			p := mustNewGroupByPlan(t, mustNewTablePlan(t, tx, mm), tt.groupFields)
			s, err := p.Open()
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if s == nil {
				t.Fatal("Open() = nil, want a scan")
			}
			defer s.Close()

			if got := readGroupsOfIDs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("the scan read groups %v, want %v", got, tt.want)
			}
		})
	}
}

// A group by reads the sort of its input once, so it costs what the sort does.
// The test schema fits 15 records to a block.
func TestGroupByPlanBlocksAccessed(t *testing.T) {
	t.Run("given a plan of 16 records, when its group by is asked for the blocks accessed, then they are the 2 blocks of its sort", func(t *testing.T) {
		src := newTestFakePlanOfTestSchema(t)
		src.recordsOutput = 16

		p := mustNewGroupByPlan(t, src, []string{"id"})
		if got, want := p.BlocksAccessed(), 2; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

// The fake plan's id holds 10 distinct values and its name 4.
func TestGroupByPlanRecordsOutput(t *testing.T) {
	tests := []struct {
		name        string
		groupFields []string
		want        int
	}{
		{
			name:        "given a plan whose id holds 10 distinct values, when it is grouped on id, then there are 10 groups",
			groupFields: []string{"id"},
			want:        10,
		},
		{
			name:        "given a plan whose id holds 10 distinct values and name 4, when it is grouped on both, then there are 40 groups",
			groupFields: []string{"id", "name"},
			want:        40,
		},
		{
			name:        "given a plan of 100 records, when it is grouped on no fields, then there is 1 group",
			groupFields: []string{},
			want:        1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustNewGroupByPlan(t, newTestFakePlanOfTestSchema(t), tt.groupFields)

			if got := p.RecordsOutput(); got != tt.want {
				t.Errorf("RecordsOutput() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGroupByPlanDistinctValues(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      int
	}{
		{
			name:      "given a plan whose id holds 10 distinct values grouped on id, when the group by is asked for those of id, then they are the 10 of the plan underneath",
			fieldName: "id",
			want:      10,
		},
		{
			name:      "given a plan whose id holds 10 distinct values grouped on id, when the group by is asked for those of countofname, then there are as many as the 10 groups",
			fieldName: "countofname",
			want:      10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustNewGroupByPlan(t, newTestFakePlanOfTestSchema(t), []string{"id"})

			if got := p.DistinctValues(tt.fieldName); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestGroupByPlanSchema(t *testing.T) {
	t.Run("given a plan of an int id and a varchar name grouped on id with a count and a max of name, when it is asked for its schema, then it is id, countofname as an int and maxofname as a varchar like name", func(t *testing.T) {
		p := mustNewGroupByPlan(t, newTestFakePlanOfTestSchema(t), []string{"id"})
		if p.Schema() == nil {
			t.Fatal("Schema() = nil, want a schema")
		}

		want := recordmanager.NewSchema()
		if err := want.AddIntField("id"); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", "id", err)
		}
		if err := want.AddIntField("countofname"); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", "countofname", err)
		}
		if err := want.AddStringField("maxofname", testStringFieldLength); err != nil {
			t.Fatalf("AddStringField(%q) error = %v", "maxofname", err)
		}

		assertSchema(t, p.Schema(), want)
	})
}

// mustNewGroupByPlan groups src on groupFields with a count and a max of name.
func mustNewGroupByPlan(t *testing.T, src Plan, groupFields []string) *GroupByPlan {
	t.Helper()

	aggFns := []materialize.AggregationFn{materialize.NewCountFn("name"), materialize.NewMaxFn("name")}
	p, err := NewGroupByPlan(newTestTransaction(t), src, groupFields, aggFns)
	if err != nil {
		t.Fatalf("NewGroupByPlan() error = %v", err)
	}
	if p == nil {
		t.Fatal("NewGroupByPlan() = nil, want a plan")
	}

	return p
}

// readGroupsOfIDs reads every group a scan of mustNewGroupByPlan gives back from
// its start as "id:count:max", with "-" for the id of a scan that does not group
// on it.
func readGroupsOfIDs(t *testing.T, s query.Scan) []string {
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

		id := "-"
		if s.HasField("id") {
			v, err := s.GetInt("id")
			if err != nil {
				t.Fatalf("GetInt(%q) error = %v", "id", err)
			}
			id = fmt.Sprint(v)
		}
		count, err := s.GetInt("countofname")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "countofname", err)
		}
		maxName, err := s.GetString("maxofname")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "maxofname", err)
		}
		got = append(got, fmt.Sprintf("%s:%d:%s", id, count, maxName))
	}
}
