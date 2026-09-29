package plan

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// testRow is one record of the test table, read back out.
type testRow struct {
	id   int32
	name string
}

// testRows are the records createTestTable writes, the i-th with id i and name
// "name" followed by i, for the ids from 0 up to count.
func testRows(count int) []testRow {
	rows := []testRow{}
	for i := range count {
		rows = append(rows, testRow{id: int32(i), name: fmt.Sprintf("name%d", i)})
	}

	return rows
}

func TestBasicUpdatePlannerExecuteInsert(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "given an empty table, when a record is inserted naming the fields in the order of the schema, then 1 record is reported and the table holds it",
			sql:  "insert into test (id, name) values (5, 'e')",
		},
		{
			name: "given an empty table, when a record is inserted naming the fields out of the order of the schema, then 1 record is reported and each value goes to the field it was named with",
			sql:  "insert into test (name, id) values ('e', 5)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, 0)

			count, err := NewBasicUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteInsert() error = %v", err)
			}

			if count != 1 {
				t.Errorf("ExecuteInsert() = %d, want 1", count)
			}
			if got, want := readTestRows(t, tx, mm), []testRow{{id: 5, name: "e"}}; !slices.Equal(got, want) {
				t.Errorf("the table holds %v, want %v", got, want)
			}
		})
	}

	t.Run("given no table of the name, when a record is inserted into it, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewBasicUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, "insert into missing (id) values (5)"), tx)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("ExecuteInsert() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}
	})

	t.Run("given a table whose id is an int, when a record is inserted with a varchar for id, then it reports ErrFieldTypeMismatch", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 0)

		_, err := NewBasicUpdatePlanner(mm).ExecuteInsert(mustParseUpdate[parse.InsertData](t, "insert into test (id) values ('x')"), tx)
		if !errors.Is(err, recordmanager.ErrFieldTypeMismatch) {
			t.Errorf("ExecuteInsert() error = %v, want %v", err, recordmanager.ErrFieldTypeMismatch)
		}
	})
}

func TestBasicUpdatePlannerExecuteDelete(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantCount int
		wantRows  []testRow
	}{
		{
			name:      "given a table of 20 records, when the record of id 7 is deleted, then 1 record is reported and the other 19 remain",
			sql:       "delete from test where id = 7",
			wantCount: 1,
			wantRows:  slices.Delete(testRows(testRecordCount), 7, 8),
		},
		{
			name:      "given a table of 20 records, when records are deleted with no where clause, then 20 are reported and none remain",
			sql:       "delete from test",
			wantCount: testRecordCount,
			wantRows:  []testRow{},
		},
		{
			name:      "given a table of 20 records, when the records of an id none holds are deleted, then 0 are reported and all 20 remain",
			sql:       "delete from test where id = 100",
			wantCount: 0,
			wantRows:  testRows(testRecordCount),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, testRecordCount)

			count, err := NewBasicUpdatePlanner(mm).ExecuteDelete(mustParseUpdate[parse.DeleteData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteDelete() error = %v", err)
			}

			if count != tt.wantCount {
				t.Errorf("ExecuteDelete() = %d, want %d", count, tt.wantCount)
			}
			if got := readTestRows(t, tx, mm); !slices.Equal(got, tt.wantRows) {
				t.Errorf("the table holds %v, want %v", got, tt.wantRows)
			}
		})
	}
}

func TestBasicUpdatePlannerExecuteModify(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantCount int
		wantRows  []testRow
	}{
		{
			name:      "given a table of 20 records, when the name of the record of id 7 is set, then 1 record is reported and only that record's name changes",
			sql:       "update test set name = 'x' where id = 7",
			wantCount: 1,
			wantRows: func() []testRow {
				rows := testRows(testRecordCount)
				rows[7].name = "x"
				return rows
			}(),
		},
		{
			name:      "given a table of 20 records, when the name is set with no where clause, then 20 are reported and every record's name changes",
			sql:       "update test set name = 'x'",
			wantCount: testRecordCount,
			wantRows: func() []testRow {
				rows := testRows(testRecordCount)
				for i := range rows {
					rows[i].name = "x"
				}
				return rows
			}(),
		},
		// The new value is an expression, read against the record being
		// changed, so a field on the right is that record's own value.
		{
			name:      "given a table of 20 records, when id is set to the field id itself, then 20 are reported and every record keeps its id",
			sql:       "update test set id = id",
			wantCount: testRecordCount,
			wantRows:  testRows(testRecordCount),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, testRecordCount)

			count, err := NewBasicUpdatePlanner(mm).ExecuteModify(mustParseUpdate[parse.ModifyData](t, tt.sql), tx)
			if err != nil {
				t.Fatalf("ExecuteModify() error = %v", err)
			}

			if count != tt.wantCount {
				t.Errorf("ExecuteModify() = %d, want %d", count, tt.wantCount)
			}
			if got := readTestRows(t, tx, mm); !slices.Equal(got, tt.wantRows) {
				t.Errorf("the table holds %v, want %v", got, tt.wantRows)
			}
		})
	}
}

func TestBasicUpdatePlannerExecuteCreateTable(t *testing.T) {
	t.Run("when a table of an int and a varchar is created, then 0 records are reported and the catalogs hold its schema", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		count, err := NewBasicUpdatePlanner(mm).ExecuteCreateTable(mustParseUpdate[parse.CreateTableData](t, "create table test (id int, name varchar(10))"), tx)
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

func TestBasicUpdatePlannerExecuteCreateView(t *testing.T) {
	t.Run("given a table, when a view over it is created, then 0 records are reported and the catalogs hold its definition", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 0)

		count, err := NewBasicUpdatePlanner(mm).ExecuteCreateView(mustParseUpdate[parse.CreateViewData](t, "create view v as select id from test where id = 3"), tx)
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

func TestBasicUpdatePlannerExecuteCreateIndex(t *testing.T) {
	t.Run("given a table, when an index on its id is created, then 0 records are reported and the catalogs hold the index on id", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 0)

		count, err := NewBasicUpdatePlanner(mm).ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, "create index idx on test (id)"), tx)
		if err != nil {
			t.Fatalf("ExecuteCreateIndex() error = %v", err)
		}

		if count != 0 {
			t.Errorf("ExecuteCreateIndex() = %d, want 0", count)
		}

		indexes, err := mm.GetIndexInfo(tx, testTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo(%q) error = %v", testTableName, err)
		}
		if _, ok := indexes["id"]; !ok || len(indexes) != 1 {
			t.Errorf("the table has indexes on %v, want one on id", slices.Collect(maps.Keys(indexes)))
		}
	})
}

// mustParseUpdate parses sql as an update command and asserts it to the kind
// the test is about.
func mustParseUpdate[T parse.UpdateCommand](t *testing.T, sql string) T {
	t.Helper()

	p, err := parse.NewParser(sql)
	if err != nil {
		t.Fatalf("NewParser(%q) error = %v", sql, err)
	}
	cmd, err := p.UpdateCmd()
	if err != nil {
		t.Fatalf("UpdateCmd() of %q error = %v", sql, err)
	}

	data, ok := cmd.(T)
	if !ok {
		t.Fatalf("UpdateCmd() of %q = %T, want %T", sql, cmd, data)
	}

	return data
}

// readTestRows reads every record of the test table, in the order the table
// holds them.
func readTestRows(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager) []testRow {
	t.Helper()

	s, err := mustNewTablePlan(t, tx, mm).Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	rows := []testRow{}
	for {
		hasNext, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !hasNext {
			return rows
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		name, err := s.GetString("name")
		if err != nil {
			t.Fatalf("GetString(%q) error = %v", "name", err)
		}
		rows = append(rows, testRow{id: id, name: name})
	}
}
