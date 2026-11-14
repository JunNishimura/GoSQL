package plan

import (
	"cmp"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Which index a plan reads through is told by the value it searches for, as in
// the index query planner's tests.
func TestTablePlannerMakeSelectPlan(t *testing.T) {
	tests := []struct {
		name    string
		indexed []string
		sql     string
		// wantSearch is the value the plan searches an index for, or nil if it
		// should not read the table through an index.
		wantSearch *query.Constant
		want       []int32
	}{
		{
			name:       "given a table of 20 records indexed on id, when the select plan under a predicate where id is 7 is made, then it reads the table through the index for 7 and reads only id 7",
			indexed:    []string{"id"},
			sql:        "select id from test where id = 7",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{7},
		},
		{
			name:       "given a table of 20 records with no index, when the select plan under a predicate where id is 7 is made, then it reads the table in full and reads only id 7",
			indexed:    nil,
			sql:        "select id from test where id = 7",
			wantSearch: nil,
			want:       []int32{7},
		},
		// The index stands for one term, and the other still has to be tested:
		// id 7 is filed under 7 but its name is not 'x'.
		{
			name:       "given a table of 20 records indexed on id, when the select plan under a predicate where id is 7 and name is 'x' is made, then it reads the table through the index for 7 and reads nothing",
			indexed:    []string{"id"},
			sql:        "select id from test where id = 7 and name = 'x'",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{},
		},
		// The test table has no oid, so a plan that tested the join term
		// against its records would fail to read them rather than read id 3.
		{
			name:       "given a table of 20 records with no index, when the select plan under a predicate where id is 3 and id is oid is made, then it tests only the term on its own fields and reads only id 3",
			indexed:    nil,
			sql:        "select id from test, other where id = 3 and id = oid",
			wantSearch: nil,
			want:       []int32{3},
		},
		{
			name:       "given a table of 20 records indexed on id, when the select plan under a predicate of no terms is made, then it reads the table in full and reads every id",
			indexed:    []string{"id"},
			sql:        "select id from test",
			wantSearch: nil,
			want:       []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, testRecordCount)
			createIndexesThrough(t, tx, mm, tt.indexed)

			p := mustMakeSelectPlan(t, tx, mm, tt.sql)

			got, ok := indexSearchOf(p)
			switch {
			case tt.wantSearch == nil && ok:
				t.Errorf("the plan reads the table through an index for %s, want no index read", got)
			case tt.wantSearch != nil && !ok:
				t.Errorf("the plan reads no index, want one for %s", *tt.wantSearch)
			case tt.wantSearch != nil && got != *tt.wantSearch:
				t.Errorf("the plan reads the table through an index for %s, want %s", got, *tt.wantSearch)
			}

			if ids := openAndReadIDs(t, p); !slices.Equal(ids, tt.want) {
				t.Errorf("the plan read ids %v, want %v", ids, tt.want)
			}
		})
	}

	// A select of no terms keeps every record, so it would cost a test of
	// each record and change nothing. Having no term to test is what the
	// planner is told apart from a predicate of no terms for.
	t.Run("given a table of 20 records with no index, when the select plan under a predicate whose only term is on another table is made, then it is the plan of reading the table with no select over it", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustMakeSelectPlan(t, tx, mm, "select id from test, other where id = oid")

		if _, ok := p.(*TablePlan); !ok {
			t.Errorf("makeSelectPlan() = %T, want *TablePlan", p)
		}
	})
}

// mustMakeSelectPlan makes the select plan of the test table under the
// predicate of sql.
func mustMakeSelectPlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string) Plan {
	t.Helper()

	tp, err := newTablePlanner(tx, testTableName, mustParseQuery(t, sql).Predicate(), mm)
	if err != nil {
		t.Fatalf("newTablePlanner(%q) error = %v", testTableName, err)
	}
	if tp == nil {
		t.Fatalf("newTablePlanner(%q) = nil, want a planner", testTableName)
	}

	p := tp.makeSelectPlan()
	if p == nil {
		t.Fatalf("makeSelectPlan() = nil, want a plan")
	}

	return p
}

// The other table, of oids 0 and 1, is what is already planned. A product
// gives its pairs a chunk at a time, so they are compared sorted.
func TestTablePlannerMakeProductPlan(t *testing.T) {
	tests := []struct {
		name    string
		indexed []string
		sql     string
		// wantSearch is the value the plan searches an index for, or nil if it
		// should not read the table through an index.
		wantSearch *query.Constant
		want       [][2]int32
	}{
		{
			name:       "given a table of 20 records with no index and another of 2 already planned, when the product plan under a predicate where id is 3 is made, then id 3 alone is put beside each oid",
			indexed:    nil,
			sql:        "select id, oid from test, other where id = 3",
			wantSearch: nil,
			want:       [][2]int32{{3, 0}, {3, 1}},
		},
		{
			name:       "given a table of 20 records indexed on id and another of 2 already planned, when the product plan under a predicate where id is 3 is made, then the table is read through the index for 3 and id 3 alone is put beside each oid",
			indexed:    []string{"id"},
			sql:        "select id, oid from test, other where id = 3",
			wantSearch: new(query.NewIntConstant(3)),
			want:       [][2]int32{{3, 0}, {3, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, testRecordCount)
			createIndexesThrough(t, tx, mm, tt.indexed)
			createOtherTable(t, tx, mm, 2)

			current := mustNewTablePlanOf(t, tx, mm, otherTableName)
			p := mustMakeProductPlan(t, tx, mm, tt.sql, current)

			got, ok := indexSearchOf(p)
			switch {
			case tt.wantSearch == nil && ok:
				t.Errorf("the plan reads the table through an index for %s, want no index read", got)
			case tt.wantSearch != nil && !ok:
				t.Errorf("the plan reads no index, want one for %s", *tt.wantSearch)
			case tt.wantSearch != nil && got != *tt.wantSearch:
				t.Errorf("the plan reads the table through an index for %s, want %s", got, *tt.wantSearch)
			}

			if pairs := sortedPairs(openAndReadIDPairs(t, p)); !slices.Equal(pairs, tt.want) {
				t.Errorf("the plan read (id, oid) pairs %v, want %v", pairs, tt.want)
			}
		})
	}

	// What is already planned is read through again for every chunk of the
	// table, so it goes on the left, where the multibuffer product
	// materializes it rather than working it out again.
	t.Run("given a table of 20 records and another of 2 already planned, when the product plan is made, then it is a multibuffer product with what is already planned on the left", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		createOtherTable(t, tx, mm, 2)

		current := mustNewTablePlanOf(t, tx, mm, otherTableName)
		p := mustMakeProductPlan(t, tx, mm, "select id, oid from test, other", current)

		mp, ok := p.(*MultibufferProductPlan)
		if !ok {
			t.Fatalf("makeProductPlan() = %T, want *MultibufferProductPlan", p)
		}
		if mp.lhs.src != current {
			t.Errorf("the left side of the product = %T, want the plan already planned", mp.lhs.src)
		}
	})
}

// mustMakeProductPlan makes the product plan of current and the test table
// under the predicate of sql.
func mustMakeProductPlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string, current Plan) Plan {
	t.Helper()

	tp, err := newTablePlanner(tx, testTableName, mustParseQuery(t, sql).Predicate(), mm)
	if err != nil {
		t.Fatalf("newTablePlanner(%q) error = %v", testTableName, err)
	}

	p, err := tp.makeProductPlan(current)
	if err != nil {
		t.Fatalf("makeProductPlan() error = %v", err)
	}
	if p == nil {
		t.Fatalf("makeProductPlan() = nil, want a plan")
	}

	return p
}

// sortedPairs returns pairs sorted by id and then by oid, for comparing the
// pairs of plans that do not promise an order.
func sortedPairs(pairs [][2]int32) [][2]int32 {
	sorted := slices.Clone(pairs)
	slices.SortFunc(sorted, func(a, b [2]int32) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})

	return sorted
}
