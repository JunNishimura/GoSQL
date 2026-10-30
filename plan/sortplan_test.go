package plan

import (
	"fmt"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestSortPlanOpen(t *testing.T) {
	tests := []struct {
		name string
		ids  []int32
		want []int32
	}{
		{
			name: "given a table already in order of id, when it is sorted by id, then its records come back in that order",
			ids:  []int32{1, 2, 3, 4, 5},
			want: []int32{1, 2, 3, 4, 5},
		},
		{
			name: "given a table in reverse order of id, when it is sorted by id, then its records come back in order of id",
			ids:  []int32{20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
			want: []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
		},
		{
			name: "given a table of ids out of order, some of them repeated, when it is sorted by id, then every record comes back in order of id",
			ids:  []int32{3, 1, 4, 1, 5, 9, 2, 6, 5, 3, 5},
			want: []int32{1, 1, 2, 3, 3, 4, 5, 5, 5, 6, 9},
		},
		{
			name: "given an empty table, when it is sorted by id, then no records come back",
			ids:  []int32{},
			want: []int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTableOfIDs(t, tx, mm, tt.ids)

			s := mustOpenSortPlan(t, NewSortPlan(tx, mustNewTablePlan(t, tx, mm), "id"))
			defer s.Close()

			if got := readIDs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("the scan read ids %v, want %v", got, tt.want)
			}
		})
	}

	// The names are "name" followed by the id, so as strings "name10" comes
	// before "name2", and an order of names is not an order of ids.
	t.Run("given a table of 20 records in order of id, when it is sorted by name, then they come back in order of name as text, each with its id", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		s := mustOpenSortPlan(t, NewSortPlan(tx, mustNewTablePlan(t, tx, mm), "name"))
		defer s.Close()

		want := []string{}
		for _, id := range []int{0, 1, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 2, 3, 4, 5, 6, 7, 8, 9} {
			want = append(want, fmt.Sprintf("%d:name%d", id, id))
		}
		if got := readIDsAndNames(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read %v, want %v", got, want)
		}
	})
}

// A sort reads its records from temp tables laid out like a materialization's,
// so reading them costs what reading a materialization does. The test schema
// fits 15 records to a block.
func TestSortPlanBlocksAccessed(t *testing.T) {
	t.Run("given a plan of 16 records, when its sort is asked for the blocks accessed, then they are the 2 blocks of its materialization", func(t *testing.T) {
		src := newTestFakePlanOfTestSchema(t)
		src.recordsOutput = 16

		p := NewSortPlan(newTestTransaction(t), src, "id")
		if got, want := p.BlocksAccessed(), 2; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestSortPlanRecordsOutput(t *testing.T) {
	t.Run("given a plan of 100 records, when its sort is asked for the records output, then they are the 100 of the plan underneath", func(t *testing.T) {
		p := NewSortPlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t), "id")

		if got, want := p.RecordsOutput(), 100; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestSortPlanDistinctValues(t *testing.T) {
	t.Run("given a plan whose id holds 10 distinct values, when its sort is asked for them, then they are the 10 of the plan underneath", func(t *testing.T) {
		p := NewSortPlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t), "id")

		if got, want := p.DistinctValues("id"), 10; got != want {
			t.Errorf("DistinctValues(%q) = %d, want %d", "id", got, want)
		}
	})
}

func TestSortPlanSchema(t *testing.T) {
	t.Run("given a plan of id and name, when its sort is asked for its schema, then it is the schema of the plan underneath", func(t *testing.T) {
		src := newTestFakePlanOfTestSchema(t)
		p := NewSortPlan(newTestTransaction(t), src, "id")

		if got := p.Schema(); got != src.schema {
			t.Errorf("Schema() = %p, want %p", got, src.schema)
		}
	})
}

// createTestTableOfIDs makes the test table and writes a record for each id,
// in the order given, whose name is "name" followed by the id.
func createTestTableOfIDs(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, ids []int32) {
	t.Helper()

	if err := mm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", testTableName, err)
	}

	layout, err := mm.GetLayout(tx, testTableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", testTableName, err)
	}

	ts, err := query.NewTableScan(tx, testTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", testTableName, err)
	}
	defer ts.Close()

	for _, id := range ids {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("id", id); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := ts.SetString("name", fmt.Sprintf("name%d", id)); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}
}

// mustOpenSortPlan opens a sort plan, failing the test if it gives no scan.
func mustOpenSortPlan(t *testing.T, p *SortPlan) query.Scan {
	t.Helper()

	s, err := p.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if s == nil {
		t.Fatal("Open() = nil, want a scan")
	}

	return s
}
