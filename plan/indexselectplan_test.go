package plan

import (
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestIndexSelectPlanOpen(t *testing.T) {
	tests := []struct {
		name  string
		value query.Constant
		want  []int32
	}{
		{
			name:  "given a table of ids 0 to 9 indexed on id, when the plan of the records filed under 3 is opened, then the scan reads the record of id 3",
			value: query.NewIntConstant(3),
			want:  []int32{3},
		},
		{
			name:  "given a table of ids 0 to 9 indexed on id, when the plan of the records filed under 20 is opened, then the scan reads none",
			value: query.NewIntConstant(20),
			want:  []int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			p := newTestIndexSelectPlan(t, tx, tt.value)

			s, err := p.Open()
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if s == nil {
				t.Fatalf("Open() = nil, want a scan")
			}
			defer s.Close()

			if got := readIDs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("the scan reads ids %v, want %v", got, tt.want)
			}
		})
	}
}

// The test table holds 10 records, and the guess at the distinct values of a
// field of it is 1 + 10/3 = 4, so a search for one value gives back 10/4 = 2
// of them.
//
// The index on id is a B-tree whose records are 16 bytes, so 50 go in a block
// of 800 and the 10 take one. A search reads the root and the leaf, 2 blocks,
// and then one block of the table for each record it gives back, 2 more.
func TestIndexSelectPlanCosts(t *testing.T) {
	t.Run("given a table of 10 records indexed on id, when the plan of one value is costed, then it reads the index and a block per record, and gives back a value's share of the records", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexSelectPlan(t, tx, query.NewIntConstant(3))

		if got, want := p.RecordsOutput(), 2; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
		if got, want := p.BlocksAccessed(), 4; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})

	t.Run("given a table of 10 records indexed on id, when the distinct values of the plan of one value are asked for, then id holds one and name holds what it does in the table", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexSelectPlan(t, tx, query.NewIntConstant(3))

		for field, want := range map[string]int{"id": 1, "name": 4} {
			if got := p.DistinctValues(field); got != want {
				t.Errorf("DistinctValues(%q) = %d, want %d", field, got, want)
			}
		}
	})
}

func TestIndexSelectPlanSchema(t *testing.T) {
	t.Run("given a table indexed on id, when the schema of the plan of one value is asked for, then it is the table's", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexSelectPlan(t, tx, query.NewIntConstant(3))

		schema := p.Schema()
		if schema == nil {
			t.Fatalf("Schema() = nil, want the table's schema")
		}
		assertSchema(t, schema, newTestSchema(t))
	})
}

// newTestIndexSelectPlan makes the test table of 10 records, indexes it on id
// through an index update planner so that the index holds them, and returns
// the plan of the records filed under value.
func newTestIndexSelectPlan(t *testing.T, tx *transaction.Transaction, value query.Constant) *IndexSelectPlan {
	t.Helper()

	mm := newTestMetadataManager(t, tx)
	createTestTable(t, tx, mm, 10)
	if _, err := NewIndexUpdatePlanner(mm).ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index id_index on test (id)"), tx); err != nil {
		t.Fatalf("ExecuteCreateIndex() error = %v", err)
	}

	indexes, err := mm.GetIndexInfo(tx, testTableName)
	if err != nil {
		t.Fatalf("GetIndexInfo(%q) error = %v", testTableName, err)
	}

	return NewIndexSelectPlan(mustNewTablePlan(t, tx, mm), indexes["id"], value)
}
