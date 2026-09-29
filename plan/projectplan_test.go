package plan

import (
	"errors"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// newTestFakePlanOfTestSchema is a plan of 100 records over 5 blocks, with the
// fields of the test table: "id" of 10 distinct values and "name" of 4.
func newTestFakePlanOfTestSchema(t *testing.T) fakePlan {
	t.Helper()

	return fakePlan{
		blocksAccessed: 5,
		recordsOutput:  100,
		distinctValues: map[string]int{
			"id":   10,
			"name": 4,
		},
		schema: newTestSchema(t),
	}
}

func TestNewProjectPlan(t *testing.T) {
	tests := []struct {
		name       string
		fieldNames []string
		wantErr    error
	}{
		{
			name:       "given a plan of id and name, when a projection onto a field it does not have is made, then it reports ErrFieldNotFound",
			fieldNames: []string{"missing"},
			wantErr:    recordmanager.ErrFieldNotFound,
		},
		{
			name:       "given a plan of id and name, when a projection naming the same field twice is made, then it reports ErrDuplicateField",
			fieldNames: []string{"id", "id"},
			wantErr:    recordmanager.ErrDuplicateField,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewProjectPlan(newTestFakePlanOfTestSchema(t), tt.fieldNames...)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewProjectPlan() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestProjectPlanOpen(t *testing.T) {
	t.Run("given a table of 20 records, when a plan projecting it onto id is opened, then the scan reads every record with id and without name", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p, err := NewProjectPlan(mustNewTablePlan(t, tx, mm), "id")
		if err != nil {
			t.Fatalf("NewProjectPlan() error = %v", err)
		}

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer s.Close()

		if s.HasField("name") {
			t.Error("the scan has field name, want it dropped")
		}

		want := []int32{}
		for i := range testRecordCount {
			want = append(want, int32(i))
		}
		if got := readIDs(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read ids %v, want %v", got, want)
		}
	})
}

// A projection drops fields, not records, and reads what it projects all the
// way through. So every number it reports is the one of the plan underneath.
func TestProjectPlanBlocksAccessed(t *testing.T) {
	t.Run("given a plan of 5 blocks, when a projection of it is asked for the blocks accessed, then they are the 5 of the plan underneath", func(t *testing.T) {
		p := mustNewProjectPlan(t, newTestFakePlanOfTestSchema(t), "id")

		if got, want := p.BlocksAccessed(), 5; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestProjectPlanRecordsOutput(t *testing.T) {
	t.Run("given a plan of 100 records, when a projection of it is asked for the records output, then they are the 100 of the plan underneath", func(t *testing.T) {
		p := mustNewProjectPlan(t, newTestFakePlanOfTestSchema(t), "id")

		if got, want := p.RecordsOutput(), 100; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestProjectPlanDistinctValues(t *testing.T) {
	t.Run("given a plan whose id holds 10 distinct values, when a projection onto id is asked for them, then they are the 10 of the plan underneath", func(t *testing.T) {
		p := mustNewProjectPlan(t, newTestFakePlanOfTestSchema(t), "id")

		if got, want := p.DistinctValues("id"), 10; got != want {
			t.Errorf("DistinctValues(%q) = %d, want %d", "id", got, want)
		}
	})
}

func TestProjectPlanSchema(t *testing.T) {
	t.Run("given a plan of id and name, when a projection onto name and id is asked for its schema, then it holds the two in that order, typed as they were underneath", func(t *testing.T) {
		p := mustNewProjectPlan(t, newTestFakePlanOfTestSchema(t), "name", "id")

		want := recordmanager.NewSchema()
		if err := want.AddStringField("name", testStringFieldLength); err != nil {
			t.Fatalf("AddStringField(%q) error = %v", "name", err)
		}
		if err := want.AddIntField("id"); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", "id", err)
		}

		assertSchema(t, p.Schema(), want)
	})
}

func mustNewProjectPlan(t *testing.T, p Plan, fieldNames ...string) *ProjectPlan {
	t.Helper()

	pp, err := NewProjectPlan(p, fieldNames...)
	if err != nil {
		t.Fatalf("NewProjectPlan(%v) error = %v", fieldNames, err)
	}

	return pp
}
