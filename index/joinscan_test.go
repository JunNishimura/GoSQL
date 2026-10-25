package index

import (
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const testLeftTableName = "test_left"

// joinedRecord is one pair a join scan gives back: the left record's lid and
// lname, and the right record's id and name.
type joinedRecord struct {
	lid   int32
	lname string
	id    int32
	name  string
}

// The right table is the one newTestIndexedTable makes, indexed on id, and the
// left one a table of lid and lname, joined to it on lid equal to id.
func TestJoinScan(t *testing.T) {
	tests := []struct {
		name  string
		left  []int32
		right []testRecord
		want  []joinedRecord
	}{
		{
			name:  "given left lids 1 and 3 and right ids 0 to 4, when the join is read, then each left record comes back with the right record of its lid",
			left:  []int32{1, 3},
			right: testRecords(0, 1, 2, 3, 4),
			want: []joinedRecord{
				{lid: 1, lname: "left1", id: 1, name: "name1"},
				{lid: 3, lname: "left3", id: 3, name: "name3"},
			},
		},
		{
			name:  "given left lids 3, 9 and 1 and right ids 0 to 4, when the join is read, then the left record of 9, which no right record matches, is passed over",
			left:  []int32{3, 9, 1},
			right: testRecords(0, 1, 2, 3, 4),
			want: []joinedRecord{
				{lid: 3, lname: "left3", id: 3, name: "name3"},
				{lid: 1, lname: "left1", id: 1, name: "name1"},
			},
		},
		{
			name: "given a left lid of 2 and two right records of id 2, when the join is read, then the left record comes back with each of them",
			left: []int32{2},
			right: []testRecord{
				{id: 2, name: "a"},
				{id: 1, name: "b"},
				{id: 2, name: "c"},
			},
			want: []joinedRecord{
				{lid: 2, lname: "left2", id: 2, name: "a"},
				{lid: 2, lname: "left2", id: 2, name: "c"},
			},
		},
		{
			name:  "given left lids 8 and 9 and right ids 0 to 4, when the join is read, then nothing comes back",
			left:  []int32{8, 9},
			right: testRecords(0, 1, 2, 3, 4),
			want:  []joinedRecord{},
		},
		{
			name:  "given an empty left table and right ids 0 to 4, when the join is read, then nothing comes back",
			left:  nil,
			right: testRecords(0, 1, 2, 3, 4),
			want:  []joinedRecord{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			s := newTestJoinScan(t, tx, tt.left, tt.right)
			defer s.Close()

			if got := readJoinedRecords(t, s); !slices.Equal(sortedJoinedRecords(got), sortedJoinedRecords(tt.want)) {
				t.Errorf("pairs = %v, want %v", got, tt.want)
			}
		})
	}

	// The order across left records is the left's own; only the order of the
	// right records that match one left record is the index's to choose.
	t.Run("given left lids 3 and 1 and right ids 0 to 4, when the join is read, then the pair of 3 comes before the pair of 1", func(t *testing.T) {
		tx := newTestTransaction(t)
		s := newTestJoinScan(t, tx, []int32{3, 1}, testRecords(0, 1, 2, 3, 4))
		defer s.Close()

		got := readJoinedRecords(t, s)
		lids := make([]int32, len(got))
		for i, r := range got {
			lids[i] = r.lid
		}
		if want := []int32{3, 1}; !slices.Equal(lids, want) {
			t.Errorf("the pairs come back in the order of lids %v, want %v", lids, want)
		}
	})

	t.Run("given a join scan read to the end, when it is moved before its first record and read again, then the same pairs come back", func(t *testing.T) {
		tx := newTestTransaction(t)
		s := newTestJoinScan(t, tx, []int32{1, 3}, testRecords(0, 1, 2, 3, 4))
		defer s.Close()

		readJoinedRecords(t, s)
		if err := s.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []joinedRecord{
			{lid: 1, lname: "left1", id: 1, name: "name1"},
			{lid: 3, lname: "left3", id: 3, name: "name3"},
		}
		if got := readJoinedRecords(t, s); !slices.Equal(got, want) {
			t.Errorf("pairs read again = %v, want %v", got, want)
		}
	})

	t.Run("given a join scan of a left table of lid and lname and a right one of id and name, when it is asked about its fields, then it has all four and no other", func(t *testing.T) {
		tx := newTestTransaction(t)
		s := newTestJoinScan(t, tx, []int32{1}, testRecords(1))
		defer s.Close()

		for field, want := range map[string]bool{"lid": true, "lname": true, "id": true, "name": true, "missing": false} {
			if got := s.HasField(field); got != want {
				t.Errorf("HasField(%q) = %t, want %t", field, got, want)
			}
		}
	})
}

// newTestJoinScan writes a left table of a record per lid, named "left"
// followed by the lid, and the right table of right records indexed on id, and
// returns a join scan of the two on lid equal to id.
func newTestJoinScan(t *testing.T, tx *transaction.Transaction, left []int32, right []testRecord) *JoinScan {
	t.Helper()

	rhs, idx := newTestIndexedTable(t, tx, right)

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("lid"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "lid", err)
	}
	if err := schema.AddStringField("lname", 10); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "lname", err)
	}
	lhs, err := query.NewTableScan(tx, testLeftTableName, recordmanager.NewLayout(schema))
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", testLeftTableName, err)
	}
	for _, lid := range left {
		if err := lhs.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := lhs.SetInt("lid", lid); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := lhs.SetString("lname", fmt.Sprintf("left%d", lid)); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
	}

	s, err := NewJoinScan(lhs, idx, "lid", rhs)
	if err != nil {
		t.Fatalf("NewJoinScan() error = %v", err)
	}

	return s
}

// readJoinedRecords reads every pair s gives back from where it is, in the
// order it gives them.
func readJoinedRecords(t *testing.T, s query.Scan) []joinedRecord {
	t.Helper()

	records := []joinedRecord{}
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return records
		}

		var r joinedRecord
		if r.lid, err = s.GetInt("lid"); err != nil {
			t.Fatalf("GetInt(%q) error = %v", "lid", err)
		}
		if r.lname, err = s.GetString("lname"); err != nil {
			t.Fatalf("GetString(%q) error = %v", "lname", err)
		}
		if r.id, err = s.GetInt("id"); err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		if r.name, err = s.GetString("name"); err != nil {
			t.Fatalf("GetString(%q) error = %v", "name", err)
		}
		records = append(records, r)
	}
}

// sortedJoinedRecords is records in order of lid, then of the right record's
// name.
func sortedJoinedRecords(records []joinedRecord) []joinedRecord {
	return slices.SortedFunc(slices.Values(records), func(a, b joinedRecord) int {
		if a.lid != b.lid {
			return int(a.lid - b.lid)
		}
		if a.name < b.name {
			return -1
		}
		if a.name > b.name {
			return 1
		}

		return 0
	})
}
