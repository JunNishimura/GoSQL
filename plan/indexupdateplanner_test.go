package plan

import (
	"errors"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestIndexUpdatePlannerExecuteInsert(t *testing.T) {
	tests := []struct {
		name string
		// indexed is the fields of the test table an index is on.
		indexed []string
		sql     string
		// want is the records each search finds, by the field searched and
		// the value searched for.
		want map[string]map[query.Constant][]testRow
	}{
		{
			name:    "given a table indexed on id, when a record is inserted, then 1 record is reported and the index finds it by its id",
			indexed: []string{"id"},
			sql:     "insert into test (id, name) values (5, 'e')",
			want: map[string]map[query.Constant][]testRow{
				"id": {query.NewIntConstant(5): {{id: 5, name: "e"}}},
			},
		},
		{
			name:    "given a table indexed on name, when a record is inserted, then 1 record is reported and the index finds it by its name",
			indexed: []string{"name"},
			sql:     "insert into test (id, name) values (5, 'e')",
			want: map[string]map[query.Constant][]testRow{
				"name": {query.NewStringConstant("e"): {{id: 5, name: "e"}}},
			},
		},
		{
			name:    "given a table indexed on both its fields, when a record is inserted, then 1 record is reported and each index finds it",
			indexed: []string{"id", "name"},
			sql:     "insert into test (id, name) values (5, 'e')",
			want: map[string]map[query.Constant][]testRow{
				"id":   {query.NewIntConstant(5): {{id: 5, name: "e"}}},
				"name": {query.NewStringConstant("e"): {{id: 5, name: "e"}}},
			},
		},
		// The record holds the empty string in the field it was not given, and
		// a search for that has to find it like any other value.
		{
			name:    "given a table indexed on name, when a record is inserted naming only id, then 1 record is reported and the index finds it by the empty string its name was left at",
			indexed: []string{"name"},
			sql:     "insert into test (id) values (5)",
			want: map[string]map[query.Constant][]testRow{
				"name": {query.NewStringConstant(""): {{id: 5, name: ""}}},
			},
		},
		{
			name:    "given a table with no index, when a record is inserted, then 1 record is reported",
			indexed: nil,
			sql:     "insert into test (id, name) values (5, 'e')",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, 0)
			createTestIndexes(t, tx, mm, tt.indexed)

			count, err := NewIndexUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteInsert() error = %v", err)
			}

			if count != 1 {
				t.Errorf("ExecuteInsert() = %d, want 1", count)
			}
			assertIndexedRows(t, tx, mm, tt.want)
		})
	}

	t.Run("given no table of the name, when a record is inserted into it, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewIndexUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, "insert into missing (id) values (5)"), tx)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("ExecuteInsert() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}
	})

	t.Run("given a table indexed on id, which is an int, when a record is inserted with a varchar for id, then it reports ErrFieldTypeMismatch", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 0)
		createTestIndexes(t, tx, mm, []string{"id"})

		_, err := NewIndexUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, "insert into test (id) values ('x')"), tx)
		if !errors.Is(err, recordmanager.ErrFieldTypeMismatch) {
			t.Errorf("ExecuteInsert() error = %v, want %v", err, recordmanager.ErrFieldTypeMismatch)
		}
	})
}

// createTestIndexes registers an index on each of fields of the test table,
// named after the field.
func createTestIndexes(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, fields []string) {
	t.Helper()

	for _, field := range fields {
		if err := mm.CreateIndex(tx, field+"_index", testTableName, field); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", field, err)
		}
	}
}

// assertIndexedRows checks, for each field and value of want, that searching
// the index on that field for that value finds the records want gives, in any
// order.
func assertIndexedRows(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, want map[string]map[query.Constant][]testRow) {
	t.Helper()

	for field, byValue := range want {
		for value, rows := range byValue {
			got := indexedRows(t, tx, mm, field, value)
			if !slices.Equal(sortedRows(got), sortedRows(rows)) {
				t.Errorf("the index on %s finds %v for %s, want %v in any order", field, got, value, rows)
			}
		}
	}
}

// indexedRows searches the index on field of the test table for value, and
// reads the record each entry it finds points to.
//
// The records are read by moving a scan of the table to the record id the
// entry holds, so what comes back is what the record id points to rather than
// what the index was filed under: an entry pointing to the wrong record reads
// back as that record.
func indexedRows(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, field string, value query.Constant) []testRow {
	t.Helper()

	indexes, err := mm.GetIndexInfo(tx, testTableName)
	if err != nil {
		t.Fatalf("GetIndexInfo(%q) error = %v", testTableName, err)
	}
	info, ok := indexes[field]
	if !ok {
		t.Fatalf("the test table has no index on %s", field)
	}

	idx, err := info.Open()
	if err != nil {
		t.Fatalf("Open() of the index on %s error = %v", field, err)
	}
	defer idx.Close()

	layout, err := mm.GetLayout(tx, testTableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", testTableName, err)
	}
	ts, err := query.NewTableScan(tx, testTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", testTableName, err)
	}
	defer ts.Close()

	if err := idx.MoveBeforeFirstMatch(value); err != nil {
		t.Fatalf("MoveBeforeFirstMatch(%s) error = %v", value, err)
	}

	rows := []testRow{}
	for {
		ok, err := idx.MoveToNextMatch()
		if err != nil {
			t.Fatalf("MoveToNextMatch() error = %v", err)
		}
		if !ok {
			return rows
		}

		rid, err := idx.DataRecordID()
		if err != nil {
			t.Fatalf("DataRecordID() error = %v", err)
		}
		if err := ts.MoveToRecordID(rid); err != nil {
			t.Fatalf("MoveToRecordID(%s) error = %v", rid, err)
		}
		id, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		name, err := ts.GetString("name")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "name", err)
		}
		rows = append(rows, testRow{id: id, name: name})
	}
}

// sortedRows is rows in order of id, then of name, so that two lists of the
// same records compare equal whatever order they were read in.
func sortedRows(rows []testRow) []testRow {
	return slices.SortedFunc(slices.Values(rows), func(a, b testRow) int {
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
