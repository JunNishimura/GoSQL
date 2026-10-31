package materialize

import (
	"fmt"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// leftRecord is a record of the left side of the merge join tests: an int
// "did" and a varchar "dname". The right side is runs of id and name.
type leftRecord struct {
	did   int32
	dname string
}

func TestMergeJoinScan(t *testing.T) {
	tests := []struct {
		name  string
		left  []leftRecord
		right [][]int32
		want  []string
	}{
		{
			name:  "given sides that each hold every id once, when they are joined on did and id, then each record pairs with the one of its id",
			left:  []leftRecord{{1, "a"}, {2, "b"}, {3, "c"}},
			right: [][]int32{{1, 2, 3}},
			want:  []string{"1a:1", "2b:2", "3c:3"},
		},
		{
			name:  "given sides that share only some ids, when they are joined on did and id, then only the records of the shared ids pair",
			left:  []leftRecord{{1, "a"}, {3, "b"}, {5, "c"}},
			right: [][]int32{{2, 3, 4, 5}},
			want:  []string{"3b:3", "5c:5"},
		},
		{
			name:  "given two records on each side of one id, when they are joined on did and id, then each on the left pairs with both on the right",
			left:  []leftRecord{{1, "a"}, {1, "b"}},
			right: [][]int32{{1, 1}},
			want:  []string{"1a:1", "1a:1", "1b:1", "1b:1"},
		},
		{
			name:  "given two records on the left of an id that is the last on the right, when they are joined on did and id, then both pair with it",
			left:  []leftRecord{{2, "a"}, {2, "b"}},
			right: [][]int32{{1, 2}},
			want:  []string{"2a:2", "2b:2"},
		},
		{
			name:  "given two records on each side of one id followed by one of the next, when they are joined on did and id, then the pairs of both ids come back",
			left:  []leftRecord{{1, "a"}, {1, "b"}, {2, "c"}},
			right: [][]int32{{1, 1, 2}},
			want:  []string{"1a:1", "1a:1", "1b:1", "1b:1", "2c:2"},
		},
		{
			name:  "given a right side of two runs that both hold an id, when two records on the left of it are joined, then each pairs with the records of both runs",
			left:  []leftRecord{{3, "a"}, {3, "b"}},
			right: [][]int32{{1, 3}, {2, 3}},
			want:  []string{"3a:3", "3a:3", "3b:3", "3b:3"},
		},
		{
			name:  "given an empty left side, when it is joined on did and id, then no pairs come back",
			left:  []leftRecord{},
			right: [][]int32{{1, 2}},
			want:  []string{},
		},
		{
			name:  "given an empty right side, when it is joined on did and id, then no pairs come back",
			left:  []leftRecord{{1, "a"}},
			right: [][]int32{{}},
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewMergeJoinScan(t, tx, tt.left, tt.right...)
			defer s.Close()

			if got := readPairs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("read pairs %v, want %v", got, tt.want)
			}
		})
	}

	// The scan remembers the join value of the pairs it last read. Moving back
	// to the start has to forget it, or the first record on the right, holding
	// that same value, would be taken to pair with a left side not yet on any
	// record.
	t.Run("given sides of a single id, when the scan is read to the end and moved before its first record, then it reads the same pairs again", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewMergeJoinScan(t, tx, []leftRecord{{1, "a"}, {1, "b"}}, []int32{1})
		defer s.Close()

		readPairs(t, s)

		want := []string{"1a:1", "1b:1"}
		if got := readPairs(t, s); !slices.Equal(got, want) {
			t.Errorf("read pairs %v, want %v", got, want)
		}
	})
}

func TestMergeJoinScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a join of did and dname with id and name, when it is asked for did of the left, then it has it",
			fieldName: "did",
			want:      true,
		},
		{
			name:      "given a join of did and dname with id and name, when it is asked for name of the right, then it has it",
			fieldName: "name",
			want:      true,
		},
		{
			name:      "given a join of did and dname with id and name, when it is asked for a field neither side has, then it does not have it",
			fieldName: "missing",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewMergeJoinScan(t, tx, []leftRecord{{1, "a"}}, []int32{1})
			defer s.Close()

			if got := s.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

// newLeftTable makes a temp table of did and dname holding the records, in the
// order given.
func newLeftTable(t *testing.T, tx *transaction.Transaction, records []leftRecord) *TempTable {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("did"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "did", err)
	}
	if err := schema.AddStringField("dname", testStringFieldLength); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "dname", err)
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
		if err := s.SetInt("did", r.did); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := s.SetString("dname", r.dname); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}

	return tt
}

// mustNewMergeJoinScan joins a table of the left records with a sort scan of
// runs of the right ids, on did and id.
func mustNewMergeJoinScan(t *testing.T, tx *transaction.Transaction, left []leftRecord, right ...[]int32) *MergeJoinScan {
	t.Helper()

	lhs, err := newLeftTable(t, tx, left).Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	rhs := mustNewSortScan(t, tx, right...)

	s, err := NewMergeJoinScan(lhs, rhs, "did", "id")
	if err != nil {
		lhs.Close()
		rhs.Close()
		t.Fatalf("NewMergeJoinScan() error = %v", err)
	}
	if s == nil {
		lhs.Close()
		rhs.Close()
		t.Fatal("NewMergeJoinScan() = nil, want a scan")
	}

	return s
}

// readPairs reads every pair a scan of mustNewMergeJoinScan gives back from its
// start as "did dname:id".
func readPairs(t *testing.T, s *MergeJoinScan) []string {
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

		did, err := s.GetInt("did")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "did", err)
		}
		dname, err := s.GetString("dname")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "dname", err)
		}
		id, err := s.GetValue("id")
		if err != nil {
			t.Fatalf("GetValue(%q) error = %v", "id", err)
		}
		got = append(got, fmt.Sprintf("%d%s:%s", did, dname, id))
	}
}
