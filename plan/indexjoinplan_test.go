package plan

import (
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The left side is the other table, of oids from 0, and the right the test
// table, of ids from 0, indexed on id and joined to it on oid equal to id.
func TestIndexJoinPlanOpen(t *testing.T) {
	tests := []struct {
		name       string
		numOther   int
		numRecords int
		// want is the (id, oid) pairs the plan reads, in the order the left
		// side gives them.
		want [][2]int32
	}{
		{
			name:       "given 3 records of oids 0 to 2 and 20 of ids 0 to 19 indexed on id, when the plan joining them on oid equal to id is opened, then the scan reads the three pairs that agree",
			numOther:   3,
			numRecords: 20,
			want:       [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		{
			name:       "given 3 records of oids 0 to 2 and 2 of ids 0 and 1 indexed on id, when the plan joining them on oid equal to id is opened, then the scan reads the two pairs that agree and passes over oid 2",
			numOther:   3,
			numRecords: 2,
			want:       [][2]int32{{0, 0}, {1, 1}},
		},
		{
			name:       "given no records of the other table and 20 of ids 0 to 19 indexed on id, when the plan joining them on oid equal to id is opened, then the scan reads none",
			numOther:   0,
			numRecords: 20,
			want:       [][2]int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			p := newTestIndexJoinPlan(t, tx, tt.numOther, tt.numRecords)

			if got := openAndReadIDPairs(t, p); !slices.Equal(got, tt.want) {
				t.Errorf("the plan read (id, oid) pairs %v, want %v", got, tt.want)
			}
		})
	}
}

// The other table holds 3 records in one block, and the guess at the distinct
// values of oid is 1 + 3/3 = 2. The test table holds 20, and a search of its
// index on id reads the root and a leaf, 2 blocks, and gives back
// 20 / (1 + 20/3) = 2 records.
//
//	records  3 left records * 2 for each                    = 6
//	blocks   1 to read the left + 3 searches * 2 + 6 records = 13
func TestIndexJoinPlanCosts(t *testing.T) {
	t.Run("given 3 records of the other table and 20 of the test table indexed on id, when the plan joining them is costed, then it reads the left once, searches the index for each left record and reads a block per pair", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexJoinPlan(t, tx, 3, 20)

		if got, want := p.RecordsOutput(), 6; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
		if got, want := p.BlocksAccessed(), 13; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})

	// A field of the left side holds what it does on the left, and one of the
	// right what it does in the table: 1 + 20/3 = 7.
	t.Run("given 3 records of the other table and 20 of the test table indexed on id, when the distinct values of the plan joining them are asked for, then each field holds what it does on its own side", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexJoinPlan(t, tx, 3, 20)

		for field, want := range map[string]int{"oid": 2, "id": 7, "name": 7} {
			if got := p.DistinctValues(field); got != want {
				t.Errorf("DistinctValues(%q) = %d, want %d", field, got, want)
			}
		}
	})
}

func TestIndexJoinPlanSchema(t *testing.T) {
	t.Run("given the other table and the test table indexed on id, when the schema of the plan joining them is asked for, then it has the fields of both, the left's first", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := newTestIndexJoinPlan(t, tx, 3, 20)

		schema := p.Schema()
		if schema == nil {
			t.Fatalf("Schema() = nil, want the fields of both sides")
		}
		if got, want := schema.Fields(), []string{"oid", "id", "name"}; !slices.Equal(got, want) {
			t.Errorf("Schema().Fields() = %v, want %v", got, want)
		}
	})
}

// newTestIndexJoinPlan makes the other table of numOther records and the test
// table of numRecords, indexes the test table on id, and returns the plan
// joining the two on oid equal to id.
func newTestIndexJoinPlan(t *testing.T, tx *transaction.Transaction, numOther, numRecords int) *IndexJoinPlan {
	t.Helper()

	mm := newTestMetadataManager(t, tx)
	createOtherTable(t, tx, mm, numOther)
	createTestTable(t, tx, mm, numRecords)
	createIndexesThrough(t, tx, mm, []string{"id"})

	left := mustNewOtherTablePlan(t, tx, mm)
	indexes, err := mm.GetIndexInfo(tx, testTableName)
	if err != nil {
		t.Fatalf("GetIndexInfo(%q) error = %v", testTableName, err)
	}

	p, err := NewIndexJoinPlan(left, mustNewTablePlan(t, tx, mm), indexes["id"], "oid")
	if err != nil {
		t.Fatalf("NewIndexJoinPlan() error = %v", err)
	}
	if p == nil {
		t.Fatalf("NewIndexJoinPlan() = nil, want a plan")
	}

	return p
}

// mustNewOtherTablePlan returns the plan of reading the other table.
func mustNewOtherTablePlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager) *TablePlan {
	t.Helper()

	tp, err := NewTablePlan(tx, otherTableName, mm)
	if err != nil {
		t.Fatalf("NewTablePlan(%q) error = %v", otherTableName, err)
	}

	return tp
}
