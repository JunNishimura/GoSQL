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

func TestNewMergeJoinPlan(t *testing.T) {
	tests := []struct {
		name    string
		p2      Plan
		field1  string
		field2  string
		wantErr error
	}{
		{
			name:    "given two plans that both have id and name, when they are merge joined, then it reports ErrDuplicateField",
			p2:      newTestFakePlanOfTestSchema(t),
			field1:  "id",
			field2:  "id",
			wantErr: recordmanager.ErrDuplicateField,
		},
		{
			name:    "given a plan of id and name and one of oid, when they are merge joined on a field the first does not have, then it reports ErrFieldNotFound",
			p2:      newFakePlanOfOID(t),
			field1:  "missing",
			field2:  "oid",
			wantErr: recordmanager.ErrFieldNotFound,
		},
		{
			name:    "given a plan of id and name and one of oid, when they are merge joined on a field the second does not have, then it reports ErrFieldNotFound",
			p2:      newFakePlanOfOID(t),
			field1:  "id",
			field2:  "missing",
			wantErr: recordmanager.ErrFieldNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewMergeJoinPlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t), tt.p2, tt.field1, tt.field2)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewMergeJoinPlan() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeJoinPlanOpen(t *testing.T) {
	tests := []struct {
		name string
		ids  []int32
		oids []int32
		want [][2]int32
	}{
		{
			name: "given tables each holding every id once out of order, when they are merge joined on id and oid, then each id pairs with its oid in order of id",
			ids:  []int32{3, 1, 2},
			oids: []int32{2, 3, 1},
			want: [][2]int32{{1, 1}, {2, 2}, {3, 3}},
		},
		{
			name: "given tables each holding an id twice out of order, when they are merge joined on id and oid, then each of the two pairs with both",
			ids:  []int32{2, 1, 2},
			oids: []int32{2, 3, 2},
			want: [][2]int32{{2, 2}, {2, 2}, {2, 2}, {2, 2}},
		},
		{
			name: "given tables that share no id, when they are merge joined on id and oid, then no pairs come back",
			ids:  []int32{1, 2},
			oids: []int32{3, 4},
			want: [][2]int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTableOfIDs(t, tx, mm, tt.ids)
			createOtherTableOfOIDs(t, tx, mm, tt.oids)

			p, err := NewMergeJoinPlan(tx, mustNewTablePlan(t, tx, mm), mustNewTablePlanOf(t, tx, mm, otherTableName), "id", "oid")
			if err != nil {
				t.Fatalf("NewMergeJoinPlan() error = %v", err)
			}
			if p == nil {
				t.Fatal("NewMergeJoinPlan() = nil, want a plan")
			}

			s, err := p.Open()
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			if s == nil {
				t.Fatal("Open() = nil, want a scan")
			}
			defer s.Close()

			if got := readIDPairs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("the scan read pairs %v, want %v", got, tt.want)
			}
		})
	}
}

// A merge join reads each sorted side once, so it costs what the two sorts do.
// The test schema fits 15 records to a block, and the schema of oid alone, a
// slot of 8 bytes, 100.
func TestMergeJoinPlanBlocksAccessed(t *testing.T) {
	t.Run("given a plan of 16 records of id and name and one of 150 of oid, when their merge join is asked for the blocks accessed, then they are the 2 blocks of each sort", func(t *testing.T) {
		p1 := newTestFakePlanOfTestSchema(t)
		p1.recordsOutput = 16
		p2 := newFakePlanOfOID(t)
		p2.recordsOutput = 150

		p := mustNewMergeJoinPlan(t, p1, p2)
		if got, want := p.BlocksAccessed(), 4; got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestMergeJoinPlanRecordsOutput(t *testing.T) {
	t.Run("given a plan of 100 records whose id holds 10 values and one of 50 whose oid holds 20, when their merge join is asked for the records output, then they are 100 times 50 over the larger 20", func(t *testing.T) {
		p := mustNewMergeJoinPlan(t, newTestFakePlanOfTestSchema(t), newFakePlanOfOID(t))

		if got, want := p.RecordsOutput(), 250; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestMergeJoinPlanDistinctValues(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      int
	}{
		{
			name:      "given a plan whose id holds 10 values joined with one whose oid holds 20, when the join is asked for those of id, then they are the 10 of the first",
			fieldName: "id",
			want:      10,
		},
		{
			name:      "given a plan whose id holds 10 values joined with one whose oid holds 20, when the join is asked for those of oid, then they are the 20 of the second",
			fieldName: "oid",
			want:      20,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := mustNewMergeJoinPlan(t, newTestFakePlanOfTestSchema(t), newFakePlanOfOID(t))

			if got := p.DistinctValues(tt.fieldName); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMergeJoinPlanSchema(t *testing.T) {
	t.Run("given a plan of id and name and one of oid, when their merge join is asked for its schema, then it is id, name and oid", func(t *testing.T) {
		p := mustNewMergeJoinPlan(t, newTestFakePlanOfTestSchema(t), newFakePlanOfOID(t))
		if p.Schema() == nil {
			t.Fatal("Schema() = nil, want a schema")
		}

		want := newTestSchema(t)
		if err := want.AddIntField("oid"); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", "oid", err)
		}

		assertSchema(t, p.Schema(), want)
	})
}

// newFakePlanOfOID is a plan of 50 records over 1 block of a single int field,
// "oid", holding 20 distinct values.
func newFakePlanOfOID(t *testing.T) fakePlan {
	t.Helper()

	return fakePlan{
		blocksAccessed: 1,
		recordsOutput:  50,
		distinctValues: map[string]int{"oid": 20},
		schema:         newIntSchema(t, "oid"),
	}
}

// mustNewMergeJoinPlan merge joins p1 and p2 on id and oid.
func mustNewMergeJoinPlan(t *testing.T, p1, p2 Plan) *MergeJoinPlan {
	t.Helper()

	p, err := NewMergeJoinPlan(newTestTransaction(t), p1, p2, "id", "oid")
	if err != nil {
		t.Fatalf("NewMergeJoinPlan() error = %v", err)
	}
	if p == nil {
		t.Fatal("NewMergeJoinPlan() = nil, want a plan")
	}

	return p
}

// createOtherTableOfOIDs makes the other table and writes a record for each
// oid, in the order given.
func createOtherTableOfOIDs(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, oids []int32) {
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

	for _, oid := range oids {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("oid", oid); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
	}
}
