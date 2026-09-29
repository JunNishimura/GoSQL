package plan

import (
	"errors"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// otherTableName is a second table for a product to put beside the test table.
// Its one field, "oid", is an int named apart from every field of the test
// table, so that the two can be multiplied without a clash.
const otherTableName = "other"

// newTestLeftPlan and newTestRightPlan are the two sides the product plan
// cases multiply. Every number of one differs from every number of the other,
// so that a case reading the wrong side gives a wrong answer.
//
//	left   100 records over 5 blocks, "a" of 10 distinct values, "b" of 4
//	right   20 records over 3 blocks, "c" of 7 distinct values, "d" of 2
func newTestLeftPlan(t *testing.T) fakePlan {
	t.Helper()

	return fakePlan{
		blocksAccessed: 5,
		recordsOutput:  100,
		distinctValues: map[string]int{
			"a": 10,
			"b": 4,
		},
		schema: newIntSchema(t, "a", "b"),
	}
}

func newTestRightPlan(t *testing.T) fakePlan {
	t.Helper()

	return fakePlan{
		blocksAccessed: 3,
		recordsOutput:  20,
		distinctValues: map[string]int{
			"c": 7,
			"d": 2,
		},
		schema: newIntSchema(t, "c", "d"),
	}
}

func TestNewProductPlan(t *testing.T) {
	t.Run("given two plans that both have a field a, when a product of them is made, then it reports ErrDuplicateField", func(t *testing.T) {
		left := newTestLeftPlan(t)
		right := newTestRightPlan(t)
		right.schema = newIntSchema(t, "a", "c")

		_, err := NewProductPlan(left, right)
		if !errors.Is(err, recordmanager.ErrDuplicateField) {
			t.Errorf("NewProductPlan() error = %v, want %v", err, recordmanager.ErrDuplicateField)
		}
	})
}

func TestProductPlanOpen(t *testing.T) {
	t.Run("given a table of 3 records and one of 2, when their product is opened, then the scan reads every record of the first beside every record of the second", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createOtherTable(t, tx, mm, 2)

		p, err := NewProductPlan(mustNewTablePlan(t, tx, mm), mustNewTablePlanOf(t, tx, mm, otherTableName))
		if err != nil {
			t.Fatalf("NewProductPlan() error = %v", err)
		}

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer s.Close()

		want := [][2]int32{
			{0, 0}, {0, 1},
			{1, 0}, {1, 1},
			{2, 0}, {2, 1},
		}
		if got := readIDPairs(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read (id, oid) pairs %v, want %v", got, want)
		}
	})
}

// The right side is read from start to end once for every record of the left,
// on top of the one read of the left itself.
func TestProductPlanBlocksAccessed(t *testing.T) {
	t.Run("given a left plan of 100 records over 5 blocks and a right plan of 3 blocks, when their product is asked for the blocks accessed, then they are 5 plus 100 times 3", func(t *testing.T) {
		p := mustNewProductPlan(t, newTestLeftPlan(t), newTestRightPlan(t))

		if got, want := p.BlocksAccessed(), 5+100*3; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestProductPlanRecordsOutput(t *testing.T) {
	t.Run("given a left plan of 100 records and a right plan of 20, when their product is asked for the records output, then they are 100 times 20", func(t *testing.T) {
		p := mustNewProductPlan(t, newTestLeftPlan(t), newTestRightPlan(t))

		if got, want := p.RecordsOutput(), 100*20; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

// Every value a field holds on its own side turns up somewhere in the product,
// beside every record of the other side, so a product changes no field's count
// of distinct values. The question is only which side to ask.
func TestProductPlanDistinctValues(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      int
	}{
		{
			name:      "given a left plan whose a holds 10 distinct values, when their product is asked for those of a, then they are the 10 of the left",
			fieldName: "a",
			want:      10,
		},
		{
			name:      "given a right plan whose c holds 7 distinct values, when their product is asked for those of c, then they are the 7 of the right",
			fieldName: "c",
			want:      7,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustNewProductPlan(t, newTestLeftPlan(t), newTestRightPlan(t))

			if got := p.DistinctValues(tt.fieldName); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestProductPlanSchema(t *testing.T) {
	t.Run("given a left plan of a and b and a right plan of c and d, when their product is asked for its schema, then it holds the fields of the left followed by those of the right", func(t *testing.T) {
		p := mustNewProductPlan(t, newTestLeftPlan(t), newTestRightPlan(t))

		assertSchema(t, p.Schema(), newIntSchema(t, "a", "b", "c", "d"))
	})
}

func mustNewProductPlan(t *testing.T, p1, p2 Plan) *ProductPlan {
	t.Helper()

	p, err := NewProductPlan(p1, p2)
	if err != nil {
		t.Fatalf("NewProductPlan() error = %v", err)
	}

	return p
}

func mustNewTablePlanOf(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, tableName string) *TablePlan {
	t.Helper()

	p, err := NewTablePlan(tx, tableName, mm)
	if err != nil {
		t.Fatalf("NewTablePlan(%q) error = %v", tableName, err)
	}

	return p
}

// newIntSchema builds a schema of int fields of the given names, in order.
func newIntSchema(t *testing.T, fieldNames ...string) *recordmanager.Schema {
	t.Helper()

	schema := recordmanager.NewSchema()
	for _, fieldName := range fieldNames {
		if err := schema.AddIntField(fieldName); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", fieldName, err)
		}
	}

	return schema
}

// createOtherTable makes the other table and writes count records into it,
// the i-th with an oid of i.
func createOtherTable(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, count int) {
	t.Helper()

	if err := mm.CreateTable(tx, otherTableName, newIntSchema(t, "oid")); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", otherTableName, err)
	}

	layout, err := mm.GetLayout(tx, otherTableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", otherTableName, err)
	}

	ts, err := query.NewTableScan(tx, otherTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", otherTableName, err)
	}
	defer ts.Close()

	for i := range count {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("oid", int32(i)); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
	}
}

// readIDPairs reads the id and the oid of every record a scan gives back, in
// the order it gives them.
func readIDPairs(t *testing.T, s query.Scan) [][2]int32 {
	t.Helper()

	if err := s.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	pairs := [][2]int32{}
	for {
		hasNext, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return pairs
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		oid, err := s.GetInt("oid")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "oid", err)
		}
		pairs = append(pairs, [2]int32{id, oid})
	}
}
