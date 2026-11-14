package plan

import (
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The expressions and terms that the select plan cases are built from. In
// newTestFakePlan, "a" has 10 distinct values, "b" has 4 and "c" has 7. A
// case that reads the wrong field, or the larger of two where the smaller is
// expected, therefore gets a wrong result.
var (
	fieldA = query.NewFieldExpression("a")
	fieldB = query.NewFieldExpression("b")
	one    = query.NewConstantExpression(query.NewIntConstant(1))
	two    = query.NewConstantExpression(query.NewIntConstant(2))
)

// newTestFakePlan returns the input plan of the select plan cases: 100
// records in 5 blocks.
func newTestFakePlan() fakePlan {
	return fakePlan{
		blocksAccessed: 5,
		recordsOutput:  100,
		distinctValues: map[string]int{
			"a": 10,
			"b": 4,
			"c": 7,
		},
		schema: recordmanager.NewSchema(),
	}
}

func TestSelectPlanOpen(t *testing.T) {
	t.Run("given a table of 20 records, when a plan selecting id 7 is opened, then the scan reads only the record of id 7", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		pred := query.NewPredicate(
			query.NewTerm(query.NewFieldExpression("id"), query.NewConstantExpression(query.NewIntConstant(7))),
		)
		p := NewSelectPlan(mustNewTablePlan(t, tx, mm), pred)

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer s.Close()

		if got, want := readIDs(t, s), []int32{7}; !slices.Equal(got, want) {
			t.Errorf("the scan read ids %v, want %v", got, want)
		}
	})
}

// A select reads every block of its input, because it must read every record
// to decide which to keep. The predicate changes the number of records that
// the select returns, not the number of blocks that it reads.
func TestSelectPlanBlocksAccessed(t *testing.T) {
	t.Run("given a plan of 5 blocks, when a select on it is asked for the blocks accessed, then they are the 5 of the plan it selects from", func(t *testing.T) {
		pred := query.NewPredicate(query.NewTerm(fieldA, one))
		p := NewSelectPlan(newTestFakePlan(), pred)

		if got, want := p.BlocksAccessed(), 5; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestSelectPlanRecordsOutput(t *testing.T) {
	tests := []struct {
		name string
		pred query.Predicate
		want int
	}{
		{
			name: "given a plan of 100 records and a predicate of no terms, when the records output are asked for, then they are all 100",
			pred: query.NewPredicate(),
			want: 100,
		},
		{
			name: "given a plan of 100 records and a predicate equating a field of 10 distinct values with a constant, when the records output are asked for, then they are 10",
			pred: query.NewPredicate(query.NewTerm(fieldA, one)),
			want: 10,
		},
		{
			name: "given a plan of 100 records and a predicate equating a field of 10 distinct values with one of 4, when the records output are asked for, then they are 10",
			pred: query.NewPredicate(query.NewTerm(fieldA, fieldB)),
			want: 10,
		},
		{
			name: "given a plan of 100 records and a predicate of two unequal constants, when the records output are asked for, then there are none",
			pred: query.NewPredicate(query.NewTerm(one, two)),
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSelectPlan(newTestFakePlan(), tt.pred)

			if got := p.RecordsOutput(); got != tt.want {
				t.Errorf("RecordsOutput() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSelectPlanDistinctValues(t *testing.T) {
	tests := []struct {
		name      string
		pred      query.Predicate
		fieldName string
		want      int
	}{
		{
			name:      "given a predicate of no terms, when the distinct values of a field are asked for, then they are those of the plan it selects from",
			pred:      query.NewPredicate(),
			fieldName: "a",
			want:      10,
		},
		{
			name:      "given a predicate equating a field with a constant, when the distinct values of that field are asked for, then there is 1",
			pred:      query.NewPredicate(query.NewTerm(fieldA, one)),
			fieldName: "a",
			want:      1,
		},
		{
			name:      "given a predicate equating a constant with a field, when the distinct values of that field are asked for, then there is 1",
			pred:      query.NewPredicate(query.NewTerm(one, fieldA)),
			fieldName: "a",
			want:      1,
		},
		// Only values that both fields have remain after a join on them, so
		// the count of the field with more values is reduced to the other's.
		{
			name:      "given a predicate equating a field of 10 distinct values with one of 4, when the distinct values of the first are asked for, then they are 4",
			pred:      query.NewPredicate(query.NewTerm(fieldA, fieldB)),
			fieldName: "a",
			want:      4,
		},
		{
			name:      "given a predicate equating a field of 10 distinct values with one of 4, when the distinct values of the second are asked for, then they are 4",
			pred:      query.NewPredicate(query.NewTerm(fieldA, fieldB)),
			fieldName: "b",
			want:      4,
		},
		{
			name:      "given a predicate on other fields, when the distinct values of a field it does not name are asked for, then they are those of the plan it selects from",
			pred:      query.NewPredicate(query.NewTerm(fieldA, fieldB)),
			fieldName: "c",
			want:      7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSelectPlan(newTestFakePlan(), tt.pred)

			if got := p.DistinctValues(tt.fieldName); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestSelectPlanSchema(t *testing.T) {
	t.Run("given a plan, when a select on it is asked for its schema, then it is the schema of the plan it selects from", func(t *testing.T) {
		input := newTestFakePlan()
		p := NewSelectPlan(input, query.NewPredicate(query.NewTerm(fieldA, one)))

		if got := p.Schema(); got != input.schema {
			t.Errorf("Schema() = %p, want %p", got, input.schema)
		}
	})
}
