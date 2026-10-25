package plan

import (
	"errors"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Which index a plan reads through is told by the value it searches for: the
// cases equate id with an int and name with a varchar, so the kind and the
// value say which of the two was taken.
func TestIndexQueryPlannerCreatePlan(t *testing.T) {
	tests := []struct {
		name    string
		indexed []string
		sql     string
		// wantSearch is the value the plan searches an index for, or nil if it
		// should read no table through an index.
		wantSearch *query.Constant
		want       []int32
	}{
		{
			name:       "given a table of 20 records indexed on id, when a query where id is 7 is planned, then the plan reads the table through the index for 7 and reads only id 7",
			indexed:    []string{"id"},
			sql:        "select id from test where id = 7",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{7},
		},
		{
			name:       "given a table of 20 records indexed on id, when a query where 7 is id, with the constant on the left, is planned, then the plan reads the table through the index for 7",
			indexed:    []string{"id"},
			sql:        "select id from test where 7 = id",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{7},
		},
		{
			name:       "given a table of 20 records with no index, when a query where id is 7 is planned, then the plan reads no table through an index and reads only id 7",
			indexed:    nil,
			sql:        "select id from test where id = 7",
			wantSearch: nil,
			want:       []int32{7},
		},
		{
			name:       "given a table of 20 records indexed on name, when a query where id is 7 is planned, then the plan reads no table through an index and reads only id 7",
			indexed:    []string{"name"},
			sql:        "select id from test where id = 7",
			wantSearch: nil,
			want:       []int32{7},
		},
		{
			name:       "given a table of 20 records indexed on id, when a query with no where clause is planned, then the plan reads no table through an index and reads every id",
			indexed:    []string{"id"},
			sql:        "select id from test",
			wantSearch: nil,
			want:       []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
		},
		// The two indexes cost the same to search, so the one on the field
		// that comes first by name is taken, whatever order the catalog lists
		// them in.
		{
			name:       "given a table of 20 records indexed on both its fields, when a query where name is 'name7' and id is 7 is planned, then the plan reads the table through the index on id for 7",
			indexed:    []string{"name", "id"},
			sql:        "select id from test where name = 'name7' and id = 7",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{7},
		},
		// The index stands for one term, and the other still has to be tested:
		// id 7 is filed under 7 but its name is not 'x'.
		{
			name:       "given a table of 20 records indexed on id, when a query where id is 7 and name is 'x' is planned, then the plan reads the table through the index for 7 and reads nothing",
			indexed:    []string{"id"},
			sql:        "select id from test where id = 7 and name = 'x'",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, testRecordCount)
			createIndexesThrough(t, tx, mm, tt.indexed)

			p := mustCreateIndexPlan(t, tx, mm, tt.sql)

			got, ok := indexSearchOf(p)
			switch {
			case tt.wantSearch == nil && ok:
				t.Errorf("the plan reads a table through an index for %s, want no index read", got)
			case tt.wantSearch != nil && !ok:
				t.Errorf("the plan reads no table through an index, want one for %s", *tt.wantSearch)
			case tt.wantSearch != nil && got != *tt.wantSearch:
				t.Errorf("the plan reads a table through an index for %s, want %s", got, *tt.wantSearch)
			}

			if ids := openAndReadIDs(t, p); !slices.Equal(ids, tt.want) {
				t.Errorf("the plan read ids %v, want %v", ids, tt.want)
			}
		})
	}

	t.Run("given a view of the test table where id is 3, which is indexed on id, when a query reading the view is planned, then the plan reads the table through the index for 3", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		createIndexesThrough(t, tx, mm, []string{"id"})
		mustCreateView(t, tx, mm, "v", "select id from test where id = 3")

		p := mustCreateIndexPlan(t, tx, mm, "select id from v")

		if got, ok := indexSearchOf(p); !ok || got != query.NewIntConstant(3) {
			t.Errorf("the plan reads through an index for %s (%t), want one for 3", got, ok)
		}
		if got, want := openAndReadIDs(t, p), []int32{3}; !slices.Equal(got, want) {
			t.Errorf("the plan read ids %v, want %v", got, want)
		}
	})

	t.Run("given a table of 3 records indexed on id and another of 2, when a query joining them where id is 1 is planned, then the plan reads the indexed table through the index for 1 and reads the one pair that agrees", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createIndexesThrough(t, tx, mm, []string{"id"})
		createOtherTable(t, tx, mm, 2)

		p := mustCreateIndexPlan(t, tx, mm, "select id, oid from test, other where id = oid and id = 1")

		if got, ok := indexSearchOf(p); !ok || got != query.NewIntConstant(1) {
			t.Errorf("the plan reads through an index for %s (%t), want one for 1", got, ok)
		}
		if got, want := openAndReadIDPairs(t, p), [][2]int32{{1, 1}}; !slices.Equal(got, want) {
			t.Errorf("the plan read (id, oid) pairs %v, want %v", got, want)
		}
	})

	t.Run("given a query naming a table that is neither a table nor a view, when it is planned, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewIndexQueryPlanner(mm).CreatePlan(mustParseQuery(t, "select id from missing"), tx)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("CreatePlan() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}
	})
}

// createIndexesThrough makes an index on each of fields of the test table
// through an index update planner, so that it holds the records the table
// already has.
func createIndexesThrough(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, fields []string) {
	t.Helper()

	up := NewIndexUpdatePlanner(mm)
	for _, field := range fields {
		sql := "create index " + field + "_index on test (" + field + ")"
		if _, err := up.ExecuteCreateIndex(mustParseUpdate[parse.CreateIndexData](t, sql), tx); err != nil {
			t.Fatalf("ExecuteCreateIndex(%q) error = %v", sql, err)
		}
	}
}

// mustCreateIndexPlan plans sql through an index query planner.
func mustCreateIndexPlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string) Plan {
	t.Helper()

	p, err := NewIndexQueryPlanner(mm).CreatePlan(mustParseQuery(t, sql), tx)
	if err != nil {
		t.Fatalf("CreatePlan(%q) error = %v", sql, err)
	}
	if p == nil {
		t.Fatalf("CreatePlan(%q) = nil, want a plan", sql)
	}

	return p
}

// indexSearchOf returns the value the first index select plan within p
// searches for, and whether there is one.
func indexSearchOf(p Plan) (query.Constant, bool) {
	switch p := p.(type) {
	case *IndexSelectPlan:
		return p.value, true
	case *ProjectPlan:
		return indexSearchOf(p.p)
	case *SelectPlan:
		return indexSearchOf(p.p)
	case *ProductPlan:
		if v, ok := indexSearchOf(p.p1); ok {
			return v, true
		}

		return indexSearchOf(p.p2)
	default:
		return query.Constant{}, false
	}
}
