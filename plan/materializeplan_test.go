package plan

import (
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

func TestMaterializePlanOpen(t *testing.T) {
	t.Run("given a table of 20 records, when its materialization is opened, then the scan reads every record with its id and name", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		s, err := NewMaterializePlan(tx, mustNewTablePlan(t, tx, mm)).Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if s == nil {
			t.Fatal("Open() = nil, want a scan")
		}
		defer s.Close()

		want := []string{}
		for i := range testRecordCount {
			want = append(want, fmt.Sprintf("%d:name%d", i, i))
		}
		if got := readIDsAndNames(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read %v, want %v", got, want)
		}
	})

	t.Run("given a table of 20 records, when its materialization is opened and the table's records are then deleted, then the scan still reads all 20", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		tp := mustNewTablePlan(t, tx, mm)

		s, err := NewMaterializePlan(tx, tp).Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if s == nil {
			t.Fatal("Open() = nil, want a scan")
		}
		defer s.Close()

		deleteAllRecords(t, tp)

		if got := readIDs(t, s); len(got) != testRecordCount {
			t.Errorf("the scan read %d records, want %d", len(got), testRecordCount)
		}
	})
}

// The test schema's slot is 52 bytes: the in-use flag (4), id (4) and name, a
// varchar of 10 (4 + 10*4). A block of 800 bytes holds 15 of them.
func TestMaterializePlanBlocksAccessed(t *testing.T) {
	tests := []struct {
		name    string
		records int
		want    int
	}{
		{
			name:    "given a plan of no records, when its materialization is asked for the blocks accessed, then there are none",
			records: 0,
			want:    0,
		},
		{
			name:    "given a plan of 15 records, when its materialization is asked for the blocks accessed, then they fill one block",
			records: 15,
			want:    1,
		},
		{
			name:    "given a plan of 16 records, when its materialization is asked for the blocks accessed, then the last record takes a second block",
			records: 16,
			want:    2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := newTestFakePlanOfTestSchema(t)
			src.recordsOutput = tt.records

			p := NewMaterializePlan(newTestTransaction(t), src)
			if got := p.BlocksAccessed(); got != tt.want {
				t.Errorf("BlocksAccessed() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMaterializePlanRecordsOutput(t *testing.T) {
	t.Run("given a plan of 100 records, when its materialization is asked for the records output, then they are the 100 of the plan underneath", func(t *testing.T) {
		p := NewMaterializePlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t))

		if got, want := p.RecordsOutput(), 100; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestMaterializePlanDistinctValues(t *testing.T) {
	t.Run("given a plan whose id holds 10 distinct values, when its materialization is asked for them, then they are the 10 of the plan underneath", func(t *testing.T) {
		p := NewMaterializePlan(newTestTransaction(t), newTestFakePlanOfTestSchema(t))

		if got, want := p.DistinctValues("id"), 10; got != want {
			t.Errorf("DistinctValues(%q) = %d, want %d", "id", got, want)
		}
	})
}

func TestMaterializePlanSchema(t *testing.T) {
	t.Run("given a plan of id and name, when its materialization is asked for its schema, then it is the schema of the plan underneath", func(t *testing.T) {
		src := newTestFakePlanOfTestSchema(t)
		p := NewMaterializePlan(newTestTransaction(t), src)

		if got := p.Schema(); got != src.schema {
			t.Errorf("Schema() = %p, want %p", got, src.schema)
		}
	})
}

// readIDsAndNames reads every record a scan gives back as "id:name", in the
// order it gives them.
func readIDsAndNames(t *testing.T, s query.Scan) []string {
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

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		name, err := s.GetString("name")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "name", err)
		}
		got = append(got, fmt.Sprintf("%d:%s", id, name))
	}
}

// deleteAllRecords deletes every record of the table tp reads.
func deleteAllRecords(t *testing.T, tp *TablePlan) {
	t.Helper()

	s, err := tp.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	us, ok := s.(query.UpdateScan)
	if !ok {
		t.Fatalf("the table's scan is a %T, want a query.UpdateScan", s)
	}
	for {
		ok, err := us.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return
		}
		if err := us.DeleteCurrentRecord(); err != nil {
			t.Fatalf("DeleteCurrentRecord() error = %v", err)
		}
	}
}
