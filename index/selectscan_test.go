package index

import (
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const testTableName = "test_table"

// testRecord is one record of the test table, read back out.
type testRecord struct {
	id   int32
	name string
}

func TestSelectScan(t *testing.T) {
	tests := []struct {
		name    string
		records []testRecord
		value   query.Constant
		want    []testRecord
	}{
		{
			name:    "given a table of ids 0 to 4 indexed on id, when the records filed under 3 are read, then the one record of id 3 comes back",
			records: testRecords(0, 1, 2, 3, 4),
			value:   query.NewIntConstant(3),
			want:    []testRecord{{id: 3, name: "name3"}},
		},
		{
			name:    "given a table of ids 0 to 4 indexed on id, when the records filed under 0, the first, are read, then the one record of id 0 comes back",
			records: testRecords(0, 1, 2, 3, 4),
			value:   query.NewIntConstant(0),
			want:    []testRecord{{id: 0, name: "name0"}},
		},
		{
			name:    "given a table of ids 0 to 4 indexed on id, when the records filed under 4, the last, are read, then the one record of id 4 comes back",
			records: testRecords(0, 1, 2, 3, 4),
			value:   query.NewIntConstant(4),
			want:    []testRecord{{id: 4, name: "name4"}},
		},
		{
			name: "given a table where two records have id 5 among others, indexed on id, when the records filed under 5 are read, then both come back",
			records: []testRecord{
				{id: 4, name: "a"},
				{id: 5, name: "b"},
				{id: 6, name: "c"},
				{id: 5, name: "d"},
			},
			value: query.NewIntConstant(5),
			want:  []testRecord{{id: 5, name: "b"}, {id: 5, name: "d"}},
		},
		{
			name:    "given a table of ids 0 to 4 indexed on id, when the records filed under 9 are read, then none come back",
			records: testRecords(0, 1, 2, 3, 4),
			value:   query.NewIntConstant(9),
			want:    []testRecord{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			s := newTestSelectScan(t, tx, tt.records, tt.value)
			defer s.Close()

			if got := readTestRecords(t, s); !slices.Equal(sortedTestRecords(got), sortedTestRecords(tt.want)) {
				t.Errorf("records = %v, want %v in any order", got, tt.want)
			}
		})
	}

	t.Run("given a select scan read to the end, when it is moved before its first record and read again, then the same records come back", func(t *testing.T) {
		tx := newTestTransaction(t)
		s := newTestSelectScan(t, tx, testRecords(0, 1, 2, 3, 4), query.NewIntConstant(3))
		defer s.Close()

		readTestRecords(t, s)
		if err := s.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got, want := readTestRecords(t, s), []testRecord{{id: 3, name: "name3"}}; !slices.Equal(got, want) {
			t.Errorf("records read again = %v, want %v", got, want)
		}
	})

	t.Run("given a select scan over a table of an id and a name, when it is asked about its fields, then it has id and name and no other", func(t *testing.T) {
		tx := newTestTransaction(t)
		s := newTestSelectScan(t, tx, testRecords(0), query.NewIntConstant(0))
		defer s.Close()

		for field, want := range map[string]bool{"id": true, "name": true, "missing": false} {
			if got := s.HasField(field); got != want {
				t.Errorf("HasField(%q) = %t, want %t", field, got, want)
			}
		}
	})
}

// testRecords is a record for each of ids, whose name is "name" followed by the
// id.
func testRecords(ids ...int32) []testRecord {
	records := make([]testRecord, len(ids))
	for i, id := range ids {
		records[i] = testRecord{id: id, name: fmt.Sprintf("name%d", id)}
	}

	return records
}

// newTestSelectScan writes records into a table of an int id and a varchar
// name, files each in a B-tree index on id, and returns a select scan of the
// records filed under value.
func newTestSelectScan(t *testing.T, tx *transaction.Transaction, records []testRecord, value query.Constant) *SelectScan {
	t.Helper()

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField("id"); err != nil {
		t.Fatalf("AddIntField(%q) error = %v", "id", err)
	}
	if err := schema.AddStringField("name", 10); err != nil {
		t.Fatalf("AddStringField(%q) error = %v", "name", err)
	}
	layout := recordmanager.NewLayout(schema)

	idx, err := NewBTreeIndex(tx, testIndexName, newTestIndexLayout(t))
	if err != nil {
		t.Fatalf("NewBTreeIndex() error = %v", err)
	}

	ts, err := query.NewTableScan(tx, testTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}
	for _, r := range records {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("id", r.id); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := ts.SetString("name", r.name); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}
		rid, err := ts.CurrentRecordID()
		if err != nil {
			t.Fatalf("CurrentRecordID() error = %v", err)
		}
		if err := idx.Insert(query.NewIntConstant(r.id), rid); err != nil {
			t.Fatalf("Insert(%d, %s) error = %v", r.id, rid, err)
		}
	}

	s, err := NewSelectScan(ts, idx, value)
	if err != nil {
		t.Fatalf("NewSelectScan() error = %v", err)
	}

	return s
}

// readTestRecords reads every record s gives back from where it is, in the
// order it gives them.
func readTestRecords(t *testing.T, s query.Scan) []testRecord {
	t.Helper()

	records := []testRecord{}
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return records
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		name, err := s.GetString("name")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "name", err)
		}
		records = append(records, testRecord{id: id, name: name})
	}
}

// sortedTestRecords is records in order of id, then of name.
func sortedTestRecords(records []testRecord) []testRecord {
	return slices.SortedFunc(slices.Values(records), func(a, b testRecord) int {
		if a.id != b.id {
			return int(a.id - b.id)
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
