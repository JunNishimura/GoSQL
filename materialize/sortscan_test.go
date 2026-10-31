package materialize

import (
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestSortScan(t *testing.T) {
	tests := []struct {
		name string
		runs [][]int32
		want []int32
	}{
		{
			name: "given one run sorted by id, when it is read, then its records come back in its order",
			runs: [][]int32{{1, 2, 3}},
			want: []int32{1, 2, 3},
		},
		{
			name: "given two runs sorted by id, when they are read, then their records come back merged in order of id",
			runs: [][]int32{{1, 4, 5}, {2, 3, 6}},
			want: []int32{1, 2, 3, 4, 5, 6},
		},
		{
			name: "given two runs sorted by id, when the first runs out before the second, then the rest of the second follows",
			runs: [][]int32{{1, 2}, {3, 4, 5}},
			want: []int32{1, 2, 3, 4, 5},
		},
		{
			name: "given two runs sorted by id that share an id, when they are read, then both records of it come back",
			runs: [][]int32{{1, 2}, {2, 3}},
			want: []int32{1, 2, 2, 3},
		},
		{
			name: "given two runs sorted by id of which the first is empty, when they are read, then the records of the second come back",
			runs: [][]int32{{}, {1, 2}},
			want: []int32{1, 2},
		},
		{
			name: "given one empty run, when it is read, then no records come back",
			runs: [][]int32{{}},
			want: []int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewSortScan(t, tx, tt.runs...)
			defer s.Close()

			if got := readIDs(t, s); !slices.Equal(got, tt.want) {
				t.Errorf("read ids %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("given two runs sorted by id, when the scan is read to the end and moved before its first record, then it reads every record again in order", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewSortScan(t, tx, []int32{1, 3}, []int32{2, 4})
		defer s.Close()

		readIDs(t, s)

		want := []int32{1, 2, 3, 4}
		if got := readIDs(t, s); !slices.Equal(got, want) {
			t.Errorf("read ids %v, want %v", got, want)
		}
	})

	t.Run("given two runs sorted by id, when they are read, then each record's name comes back with its id", func(t *testing.T) {
		tx := newTestTransaction(t)

		s := mustNewSortScan(t, tx, []int32{1, 3}, []int32{2})
		defer s.Close()

		want := []string{"1:name1", "2:name2", "3:name3"}
		if got := readIDsAndNames(t, s); !slices.Equal(got, want) {
			t.Errorf("read %v, want %v", got, want)
		}
	})
}

func TestNewSortScan(t *testing.T) {
	tests := []struct {
		name string
		runs int
	}{
		{
			name: "given no runs, it reports an error",
			runs: 0,
		},
		{
			name: "given three runs, it reports an error",
			runs: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			runs := []*TempTable{}
			for range tt.runs {
				runs = append(runs, newRun(t, tx, 1))
			}

			s, err := NewSortScan(runs, NewRecordComparator("id"))
			if err == nil {
				s.Close()
				t.Errorf("NewSortScan() of %d runs error = nil, want an error", tt.runs)
			}
		})
	}
}

func TestSortScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given runs of id and name, when it is asked for id before any record is read, then it has it",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given runs of id and name, when it is asked for a field the runs do not have, then it does not have it",
			fieldName: "missing",
			want:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewSortScan(t, tx, []int32{1}, []int32{2})
			defer s.Close()

			if got := s.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %v, want %v", tt.fieldName, got, tt.want)
			}
		})
	}
}

// newRun makes a temp table of the test schema holding a record for each id,
// in the order given, whose name is "name" followed by the id.
func newRun(t *testing.T, tx *transaction.Transaction, ids ...int32) *TempTable {
	t.Helper()

	tt := NewTempTable(tx, newTestSchema(t))
	s, err := tt.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	for _, id := range ids {
		if err := s.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := s.SetInt("id", id); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := s.SetString("name", fmt.Sprintf("name%d", id)); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}

	return tt
}

// mustNewSortScan opens a sort scan by id over a run for each list of ids.
func mustNewSortScan(t *testing.T, tx *transaction.Transaction, runIDs ...[]int32) *SortScan {
	t.Helper()

	runs := []*TempTable{}
	for _, ids := range runIDs {
		runs = append(runs, newRun(t, tx, ids...))
	}

	s, err := NewSortScan(runs, NewRecordComparator("id"))
	if err != nil {
		t.Fatalf("NewSortScan() error = %v", err)
	}
	if s == nil {
		t.Fatal("NewSortScan() = nil, want a scan")
	}

	return s
}

// readIDs reads the id of every record a scan gives back from its start, in
// the order it gives them.
func readIDs(t *testing.T, s query.Scan) []int32 {
	t.Helper()

	if err := s.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	ids := []int32{}
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return ids
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		ids = append(ids, id)
	}
}

// readIDsAndNames reads every record a scan gives back from its start as
// "id:name", in the order it gives them.
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

func TestSortScanRestorePosition(t *testing.T) {
	tests := []struct {
		name string
		runs [][]int32
		// read is how many records are read before the position is saved.
		read int
		// wantCurrent is the id of the record the scan is on once put back.
		wantCurrent int32
		// wantRest is the ids read after it, once put back.
		wantRest []int32
	}{
		{
			name:        "given two runs read to the middle, when the position is saved, the scan read to the end and put back, then it is on the saved record and reads the rest again",
			runs:        [][]int32{{1, 3, 5}, {2, 4, 6}},
			read:        3,
			wantCurrent: 3,
			wantRest:    []int32{4, 5, 6},
		},
		{
			name:        "given two runs, when the position is saved before both run out and the scan is put back after, then it reads the records it had read past again",
			runs:        [][]int32{{1, 2}, {3, 4}},
			read:        2,
			wantCurrent: 2,
			wantRest:    []int32{3, 4},
		},
		{
			name:        "given two runs, when the position is saved on a record of the first and put back after reading on in the second, then it is on the record of the first",
			runs:        [][]int32{{1, 3}, {2, 4}},
			read:        1,
			wantCurrent: 1,
			wantRest:    []int32{2, 3, 4},
		},
		{
			name:        "given two runs, when the position is saved after the first has run out, then the scan put back does not read the first again",
			runs:        [][]int32{{1}, {2, 3}},
			read:        2,
			wantCurrent: 2,
			wantRest:    []int32{3},
		},
		{
			name:        "given an empty run and another, when the position is saved and the scan put back, then it reads the rest of the other",
			runs:        [][]int32{{}, {1, 2}},
			read:        1,
			wantCurrent: 1,
			wantRest:    []int32{2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)

			s := mustNewSortScan(t, tx, tt.runs...)
			defer s.Close()

			for range tt.read {
				if ok, err := s.MoveToNextRecord(); err != nil || !ok {
					t.Fatalf("MoveToNextRecord() = %v, %v, want true, nil", ok, err)
				}
			}
			pos, err := s.SavePosition()
			if err != nil {
				t.Fatalf("SavePosition() error = %v", err)
			}
			readRestOfIDs(t, s)

			if err := s.RestorePosition(pos); err != nil {
				t.Fatalf("RestorePosition() error = %v", err)
			}

			got, err := s.GetInt("id")
			if err != nil {
				t.Fatalf("GetInt(%q) error = %v", "id", err)
			}
			if got != tt.wantCurrent {
				t.Errorf("the scan is on id %d, want %d", got, tt.wantCurrent)
			}
			if rest := readRestOfIDs(t, s); !slices.Equal(rest, tt.wantRest) {
				t.Errorf("the scan read on %v, want %v", rest, tt.wantRest)
			}
		})
	}
}

// readRestOfIDs reads the id of every record a scan gives back from where it
// is, without moving it back to its start.
func readRestOfIDs(t *testing.T, s query.Scan) []int32 {
	t.Helper()

	ids := []int32{}
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return ids
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		ids = append(ids, id)
	}
}
