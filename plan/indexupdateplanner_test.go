package plan

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/index"
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

// Each case starts from the 5 records testRows gives, filed in the table's
// indexes, and every index is searched afterwards for every one of their
// values: a delete that took out one entry too many, or left another pointing
// to the wrong record, shows as a value finding something other than its own
// record.
func TestIndexUpdatePlannerExecuteDelete(t *testing.T) {
	tests := []struct {
		name      string
		indexed   []string
		sql       string
		wantCount int
		want      map[string]map[query.Constant][]testRow
	}{
		{
			name:      "given a table of 5 records indexed on id, when the record of id 2 is deleted, then 1 record is reported and the index finds each of the others but not that one",
			indexed:   []string{"id"},
			sql:       "delete from test where id = 2",
			wantCount: 1,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0): {{id: 0, name: "name0"}},
					query.NewIntConstant(1): {{id: 1, name: "name1"}},
					query.NewIntConstant(2): {},
					query.NewIntConstant(3): {{id: 3, name: "name3"}},
					query.NewIntConstant(4): {{id: 4, name: "name4"}},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on name, when the record of id 2 is deleted, then 1 record is reported and the index finds each of the others but not that one",
			indexed:   []string{"name"},
			sql:       "delete from test where id = 2",
			wantCount: 1,
			want: map[string]map[query.Constant][]testRow{
				"name": {
					query.NewStringConstant("name0"): {{id: 0, name: "name0"}},
					query.NewStringConstant("name1"): {{id: 1, name: "name1"}},
					query.NewStringConstant("name2"): {},
					query.NewStringConstant("name3"): {{id: 3, name: "name3"}},
					query.NewStringConstant("name4"): {{id: 4, name: "name4"}},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on both its fields, when every record is deleted, then 5 records are reported and neither index finds any of them",
			indexed:   []string{"id", "name"},
			sql:       "delete from test",
			wantCount: 5,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0): {},
					query.NewIntConstant(1): {},
					query.NewIntConstant(2): {},
					query.NewIntConstant(3): {},
					query.NewIntConstant(4): {},
				},
				"name": {
					query.NewStringConstant("name0"): {},
					query.NewStringConstant("name1"): {},
					query.NewStringConstant("name2"): {},
					query.NewStringConstant("name3"): {},
					query.NewStringConstant("name4"): {},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on id, when the records the predicate keeps are none, then 0 records are reported and the index still finds each of them",
			indexed:   []string{"id"},
			sql:       "delete from test where id = 100",
			wantCount: 0,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0): {{id: 0, name: "name0"}},
					query.NewIntConstant(1): {{id: 1, name: "name1"}},
					query.NewIntConstant(2): {{id: 2, name: "name2"}},
					query.NewIntConstant(3): {{id: 3, name: "name3"}},
					query.NewIntConstant(4): {{id: 4, name: "name4"}},
				},
			},
		},
		{
			name:      "given a table of 5 records with no index, when the record of id 2 is deleted, then 1 record is reported",
			indexed:   nil,
			sql:       "delete from test where id = 2",
			wantCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, 0)
			createTestIndexes(t, tx, mm, tt.indexed)
			up := NewIndexUpdatePlanner(mm)
			insertTestRowsThrough(t, tx, up, testRows(5))

			count, err := up.ExecuteDelete(mustParseUpdate[parse.DeleteData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteDelete() error = %v", err)
			}

			if count != tt.wantCount {
				t.Errorf("ExecuteDelete() = %d, want %d", count, tt.wantCount)
			}
			assertIndexedRows(t, tx, mm, tt.want)
		})
	}

	// The index is registered after the records are written, and is not made
	// to hold them, so it is behind the table: that is what an index left out
	// of a change looks like.
	t.Run("given a table of 5 records indexed on id by an index that holds none of them, when the record of id 2 is deleted, then it reports ErrEntryNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 5)
		createTestIndexes(t, tx, mm, []string{"id"})

		_, err := NewIndexUpdatePlanner(mm).ExecuteDelete(mustParseUpdate[parse.DeleteData](t, "delete from test where id = 2"), tx)
		if !errors.Is(err, index.ErrEntryNotFound) {
			t.Errorf("ExecuteDelete() error = %v, want %v", err, index.ErrEntryNotFound)
		}
	})
}

// As for a delete, each case starts from the 5 records testRows gives, filed in
// the table's indexes, and every index is searched afterwards for every value
// the records held before and hold after. The records are read through the
// record ids the entries hold, so a record that kept its entry reads back with
// whatever it was changed to.
func TestIndexUpdatePlannerExecuteModify(t *testing.T) {
	tests := []struct {
		name      string
		indexed   []string
		sql       string
		wantCount int
		want      map[string]map[query.Constant][]testRow
	}{
		{
			name:      "given a table of 5 records indexed on name, when the name of id 2 is set to 'x', then 1 record is reported and the index finds it under 'x' and no longer under its old name",
			indexed:   []string{"name"},
			sql:       "update test set name = 'x' where id = 2",
			wantCount: 1,
			want: map[string]map[query.Constant][]testRow{
				"name": {
					query.NewStringConstant("name0"): {{id: 0, name: "name0"}},
					query.NewStringConstant("name1"): {{id: 1, name: "name1"}},
					query.NewStringConstant("name2"): {},
					query.NewStringConstant("name3"): {{id: 3, name: "name3"}},
					query.NewStringConstant("name4"): {{id: 4, name: "name4"}},
					query.NewStringConstant("x"):     {{id: 2, name: "x"}},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on id, when the id of 2 is set to 10, then 1 record is reported and the index finds it under 10 and no longer under 2",
			indexed:   []string{"id"},
			sql:       "update test set id = 10 where id = 2",
			wantCount: 1,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0):  {{id: 0, name: "name0"}},
					query.NewIntConstant(1):  {{id: 1, name: "name1"}},
					query.NewIntConstant(2):  {},
					query.NewIntConstant(3):  {{id: 3, name: "name3"}},
					query.NewIntConstant(4):  {{id: 4, name: "name4"}},
					query.NewIntConstant(10): {{id: 10, name: "name2"}},
				},
			},
		},
		// The index is on a field the statement does not change, so it is left
		// as it was, and its entry for 2 reads back the record as changed.
		{
			name:      "given a table of 5 records indexed on id, when the name of id 2 is set to 'x', then 1 record is reported and the index still finds each record under its id",
			indexed:   []string{"id"},
			sql:       "update test set name = 'x' where id = 2",
			wantCount: 1,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0): {{id: 0, name: "name0"}},
					query.NewIntConstant(1): {{id: 1, name: "name1"}},
					query.NewIntConstant(2): {{id: 2, name: "x"}},
					query.NewIntConstant(3): {{id: 3, name: "name3"}},
					query.NewIntConstant(4): {{id: 4, name: "name4"}},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on both its fields, when every name is set to 'x', then 5 records are reported, the index on name finds them all under 'x' and none under their old names, and the index on id still finds each under its id",
			indexed:   []string{"id", "name"},
			sql:       "update test set name = 'x'",
			wantCount: 5,
			want: map[string]map[query.Constant][]testRow{
				"id": {
					query.NewIntConstant(0): {{id: 0, name: "x"}},
					query.NewIntConstant(1): {{id: 1, name: "x"}},
					query.NewIntConstant(2): {{id: 2, name: "x"}},
					query.NewIntConstant(3): {{id: 3, name: "x"}},
					query.NewIntConstant(4): {{id: 4, name: "x"}},
				},
				"name": {
					query.NewStringConstant("name0"): {},
					query.NewStringConstant("name1"): {},
					query.NewStringConstant("name2"): {},
					query.NewStringConstant("name3"): {},
					query.NewStringConstant("name4"): {},
					query.NewStringConstant("x"): {
						{id: 0, name: "x"},
						{id: 1, name: "x"},
						{id: 2, name: "x"},
						{id: 3, name: "x"},
						{id: 4, name: "x"},
					},
				},
			},
		},
		{
			name:      "given a table of 5 records indexed on name, when the records the predicate keeps are none, then 0 records are reported and the index still finds each of them under its name",
			indexed:   []string{"name"},
			sql:       "update test set name = 'x' where id = 100",
			wantCount: 0,
			want: map[string]map[query.Constant][]testRow{
				"name": {
					query.NewStringConstant("name0"): {{id: 0, name: "name0"}},
					query.NewStringConstant("name1"): {{id: 1, name: "name1"}},
					query.NewStringConstant("name2"): {{id: 2, name: "name2"}},
					query.NewStringConstant("name3"): {{id: 3, name: "name3"}},
					query.NewStringConstant("name4"): {{id: 4, name: "name4"}},
					query.NewStringConstant("x"):     {},
				},
			},
		},
		{
			name:      "given a table of 5 records with no index, when the name of id 2 is set to 'x', then 1 record is reported",
			indexed:   nil,
			sql:       "update test set name = 'x' where id = 2",
			wantCount: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, 0)
			createTestIndexes(t, tx, mm, tt.indexed)
			up := NewIndexUpdatePlanner(mm)
			insertTestRowsThrough(t, tx, up, testRows(5))

			count, err := up.ExecuteModify(mustParseUpdate[parse.ModifyData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteModify() error = %v", err)
			}

			if count != tt.wantCount {
				t.Errorf("ExecuteModify() = %d, want %d", count, tt.wantCount)
			}
			assertIndexedRows(t, tx, mm, tt.want)
		})
	}

	// As for a delete, the index is registered after the records are written
	// and holds none of them.
	t.Run("given a table of 5 records indexed on name by an index that holds none of them, when the name of id 2 is set to 'x', then it reports ErrEntryNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 5)
		createTestIndexes(t, tx, mm, []string{"name"})

		_, err := NewIndexUpdatePlanner(mm).ExecuteModify(mustParseUpdate[parse.ModifyData](t, "update test set name = 'x' where id = 2"), tx)
		if !errors.Is(err, index.ErrEntryNotFound) {
			t.Errorf("ExecuteModify() error = %v, want %v", err, index.ErrEntryNotFound)
		}
	})
}

func TestIndexUpdatePlannerExecuteCreateTable(t *testing.T) {
	t.Run("when a table of an int and a varchar is created, then 0 records are reported and the catalogs hold its schema", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		count, err := NewIndexUpdatePlanner(mm).ExecuteCreateTable(mustParseUpdate[parse.CreateTableData](t, "create table test (id int, name varchar(10))"), tx)
		if err != nil {
			t.Fatalf("ExecuteCreateTable() error = %v", err)
		}

		if count != 0 {
			t.Errorf("ExecuteCreateTable() = %d, want 0", count)
		}

		layout, err := mm.GetLayout(tx, testTableName)
		if err != nil {
			t.Fatalf("GetLayout(%q) error = %v", testTableName, err)
		}
		assertSchema(t, layout.Schema(), newTestSchema(t))
	})
}

func TestIndexUpdatePlannerExecuteCreateView(t *testing.T) {
	t.Run("given a table, when a view over it is created, then 0 records are reported and the catalogs hold its definition", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 0)

		count, err := NewIndexUpdatePlanner(mm).ExecuteCreateView(mustParseUpdate[parse.CreateViewData](t, "create view v as select id from test where id = 3"), tx)
		if err != nil {
			t.Fatalf("ExecuteCreateView() error = %v", err)
		}

		if count != 0 {
			t.Errorf("ExecuteCreateView() = %d, want 0", count)
		}

		definition, ok, err := mm.GetViewDefinition(tx, "v")
		if err != nil {
			t.Fatalf("GetViewDefinition(%q) error = %v", "v", err)
		}
		if !ok {
			t.Fatalf("GetViewDefinition(%q) found no view", "v")
		}
		if want := "select id from test where id = 3"; definition != want {
			t.Errorf("GetViewDefinition(%q) = %q, want %q", "v", definition, want)
		}
	})
}

// The records are written by createTestTable, past every planner, so the only
// way an index can come to hold them is by being filled from the table when it
// is made.
func TestIndexUpdatePlannerExecuteCreateIndex(t *testing.T) {
	tests := []struct {
		name    string
		numRows int
		sql     string
		field   string
		want    map[query.Constant][]testRow
	}{
		{
			name:    "given a table of 5 records, when an index on id is created, then 0 records are reported and the index finds each record under its id",
			numRows: 5,
			sql:     "create index idx on test (id)",
			field:   "id",
			want: map[query.Constant][]testRow{
				query.NewIntConstant(0): {{id: 0, name: "name0"}},
				query.NewIntConstant(1): {{id: 1, name: "name1"}},
				query.NewIntConstant(2): {{id: 2, name: "name2"}},
				query.NewIntConstant(3): {{id: 3, name: "name3"}},
				query.NewIntConstant(4): {{id: 4, name: "name4"}},
			},
		},
		{
			name:    "given a table of 5 records, when an index on name is created, then 0 records are reported and the index finds each record under its name",
			numRows: 5,
			sql:     "create index idx on test (name)",
			field:   "name",
			want: map[query.Constant][]testRow{
				query.NewStringConstant("name0"): {{id: 0, name: "name0"}},
				query.NewStringConstant("name1"): {{id: 1, name: "name1"}},
				query.NewStringConstant("name2"): {{id: 2, name: "name2"}},
				query.NewStringConstant("name3"): {{id: 3, name: "name3"}},
				query.NewStringConstant("name4"): {{id: 4, name: "name4"}},
			},
		},
		{
			name:    "given an empty table, when an index on id is created, then 0 records are reported and the index finds nothing",
			numRows: 0,
			sql:     "create index idx on test (id)",
			field:   "id",
			want: map[query.Constant][]testRow{
				query.NewIntConstant(0): {},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, tt.numRows)

			count, err := NewIndexUpdatePlanner(mm).ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteCreateIndex() error = %v", err)
			}

			if count != 0 {
				t.Errorf("ExecuteCreateIndex() = %d, want 0", count)
			}
			assertIndexedRows(t, tx, mm, map[string]map[query.Constant][]testRow{tt.field: tt.want})
		})
	}

	t.Run("given a table indexed on id, when another index on id is created, then it reports ErrFieldAlreadyIndexed", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 5)
		up := NewIndexUpdatePlanner(mm)

		if _, err := up.ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index idx on test (id)"), tx); err != nil {
			t.Fatalf("ExecuteCreateIndex(idx) error = %v", err)
		}

		_, err := up.ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index idx2 on test (id)"), tx)
		if !errors.Is(err, metadatamanager.ErrFieldAlreadyIndexed) {
			t.Errorf("ExecuteCreateIndex(idx2) error = %v, want %v", err, metadatamanager.ErrFieldAlreadyIndexed)
		}
	})

	// An index the catalog holds for a table or a field that is not there
	// could never be described, and every planner reading that table would
	// fail asking for it, so nothing is written for one.
	t.Run("given no table of the name, when an index on it is created, then it reports ErrTableNotFound and the catalog holds no index for it", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewIndexUpdatePlanner(mm).ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index idx on missing (id)"), tx)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("ExecuteCreateIndex() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}

		assertNoIndexes(t, tx, mm, "missing")
	})

	t.Run("given a table with no field of the name, when an index on it is created, then it reports ErrFieldNotFound and the catalog holds no index for the table", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 5)

		_, err := NewIndexUpdatePlanner(mm).ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index idx on test (missing)"), tx)
		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("ExecuteCreateIndex() error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}

		assertNoIndexes(t, tx, mm, testTableName)
	})
}

// assertNoIndexes checks that the catalog holds no index for tableName, which
// asking for its indexes without an error also shows the catalog can still
// answer for.
func assertNoIndexes(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, tableName string) {
	t.Helper()

	indexes, err := mm.GetIndexInfo(tx, tableName)
	if err != nil {
		t.Fatalf("GetIndexInfo(%q) error = %v", tableName, err)
	}
	if len(indexes) != 0 {
		t.Errorf("GetIndexInfo(%q) has indexes on %d fields, want none", tableName, len(indexes))
	}
}

// insertTestRowsThrough writes rows into the test table through up, so that
// they are filed in whatever indexes the table has.
func insertTestRowsThrough(t *testing.T, tx *transaction.Transaction, up *IndexUpdatePlanner, rows []testRow) {
	t.Helper()

	for _, row := range rows {
		sql := fmt.Sprintf("insert into test (id, name) values (%d, '%s')", row.id, row.name)
		if _, err := up.ExecuteInsert(mustParseUpdate[parse.InsertData](t, sql), tx); err != nil {
			t.Fatalf("ExecuteInsert(%q) error = %v", sql, err)
		}
	}
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
