package plan

import (
	"cmp"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The search value shows which index a plan uses, as in the tests of the
// index query planner.
func TestTablePlannerMakeSelectPlan(t *testing.T) {
	tests := []struct {
		name    string
		indexed []string
		sql     string
		// wantSearch is the value that the plan searches an index for, or nil
		// if the plan should not use an index.
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
		// The index covers one term, and the other term must still be tested:
		// the record with id 7 has the key 7, but its name is not 'x'.
		{
			name:       "given a table of 20 records indexed on id, when the select plan under a predicate where id is 7 and name is 'x' is made, then it reads the table through the index for 7 and reads nothing",
			indexed:    []string{"id"},
			sql:        "select id from test where id = 7 and name = 'x'",
			wantSearch: new(query.NewIntConstant(7)),
			want:       []int32{},
		},
		// The test table has no oid. A plan that tested the join term against
		// its records would fail to read them and would not return id 3.
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

	// A select with no terms keeps every record. It would cost one test per
	// record and change nothing. The planner distinguishes "no term to test"
	// from a predicate with no terms in order to avoid such a select.
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

// mustMakeSelectPlan creates the select plan for the test table under the
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

// The other table, with oids 0 and 1, is the current plan. A product returns
// its pairs chunk by chunk, so the pairs are compared after sorting.
func TestTablePlannerMakeProductPlan(t *testing.T) {
	tests := []struct {
		name    string
		indexed []string
		sql     string
		// wantSearch is the value that the plan searches an index for, or nil
		// if the plan should not use an index.
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

	// The current plan is scanned again for every chunk of the table. It is
	// therefore the left side, which the multibuffer product materializes
	// and does not compute again.
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

// mustMakeProductPlan creates the product plan of current and the test table
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

// sortedPairs returns pairs sorted by id and then by oid. It is used to
// compare the pairs of plans that do not define an order.
func sortedPairs(pairs [][2]int32) [][2]int32 {
	sorted := slices.Clone(pairs)
	slices.SortFunc(sorted, func(a, b [2]int32) int {
		return cmp.Or(cmp.Compare(a[0], b[0]), cmp.Compare(a[1], b[1]))
	})

	return sorted
}

// The other table, with oids from 0, is the current plan. It is joined to the
// test table, with ids from 0, on oid = id.
//
// Whether the join uses the index on id depends on the cost. With 3 oids, the
// index join reads 13 blocks. The product reads 3 blocks when the test table
// has 20 records, and 17 when it has 200. The cases that expect the index
// therefore use 200 records, and the cases that expect a product use 20.
func TestTablePlannerMakeJoinPlan(t *testing.T) {
	tests := []struct {
		name        string
		recordCount int
		indexed     []string
		sql         string
		// wantIndexJoin reports whether the plan should join through the index
		// on id.
		wantIndexJoin bool
		want          [][2]int32
	}{
		{
			name:          "given a table of 200 records indexed on id and another of 3 already planned, when the join plan under a predicate where id is oid is made, then it joins through the index on id and pairs each oid with the same id",
			recordCount:   200,
			indexed:       []string{"id"},
			sql:           "select id, oid from test, other where id = oid",
			wantIndexJoin: true,
			want:          [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		// The index covers the join term. The term on name must still be
		// tested, and only on the table's side of the join.
		{
			name:          "given a table of 200 records indexed on id and another of 3 already planned, when the join plan under a predicate where id is oid and name is 'name1' is made, then it joins through the index on id and keeps only the pair of 1",
			recordCount:   200,
			indexed:       []string{"id"},
			sql:           "select id, oid from test, other where id = oid and name = 'name1'",
			wantIndexJoin: true,
			want:          [][2]int32{{1, 1}},
		},
		{
			name:          "given a table of 20 records indexed on id and another of 3 already planned, when the join plan under a predicate where id is oid is made, then it puts them beside each other, which reads fewer blocks, and pairs each oid with the same id",
			recordCount:   20,
			indexed:       []string{"id"},
			sql:           "select id, oid from test, other where id = oid",
			wantIndexJoin: false,
			want:          [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		{
			name:          "given a table of 200 records with no index and another of 3 already planned, when the join plan under a predicate where id is oid and name is 'name1' is made, then it puts them beside each other and keeps only the pair of 1",
			recordCount:   200,
			indexed:       nil,
			sql:           "select id, oid from test, other where id = oid and name = 'name1'",
			wantIndexJoin: false,
			want:          [][2]int32{{1, 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, tt.recordCount)
			createIndexesThrough(t, tx, mm, tt.indexed)
			createOtherTable(t, tx, mm, 3)

			current := mustNewTablePlanOf(t, tx, mm, otherTableName)
			p, ok := mustMakeJoinPlan(t, tx, mm, tt.sql, current)
			if !ok {
				t.Fatalf("makeJoinPlan() ok = false, want a join plan")
			}

			joinField, isIndexJoin := indexJoinFieldOf(p)
			switch {
			case tt.wantIndexJoin && !isIndexJoin:
				t.Errorf("the plan joins through no index, want it to join through the index on id")
			case !tt.wantIndexJoin && isIndexJoin:
				t.Errorf("the plan joins through an index on the value of %s, want no index join", joinField)
			case isIndexJoin && joinField != "oid":
				t.Errorf("the plan joins through the index on id on the value of %s, want oid", joinField)
			}

			if pairs := sortedPairs(openAndReadIDPairs(t, p)); !slices.Equal(pairs, tt.want) {
				t.Errorf("the plan read (id, oid) pairs %v, want %v", pairs, tt.want)
			}
		})
	}

	// Without a join term, the caller chooses the plan. The heuristic planner
	// tries the other tables first and then falls back to a product.
	t.Run("given a table of 20 records and another of 3 already planned, when the join plan under a predicate where id is 3 is made, then there is none, since no term joins them", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		createOtherTable(t, tx, mm, 3)

		current := mustNewTablePlanOf(t, tx, mm, otherTableName)
		if p, ok := mustMakeJoinPlan(t, tx, mm, "select id, oid from test, other where id = 3", current); ok {
			t.Errorf("makeJoinPlan() = %T, want none", p)
		}
	})
}

// mustMakeJoinPlan creates the join plan of current and the test table under
// the predicate of sql.
func mustMakeJoinPlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string, current Plan) (Plan, bool) {
	t.Helper()

	tp, err := newTablePlanner(tx, testTableName, mustParseQuery(t, sql).Predicate(), mm)
	if err != nil {
		t.Fatalf("newTablePlanner(%q) error = %v", testTableName, err)
	}

	p, ok, err := tp.makeJoinPlan(current)
	if err != nil {
		t.Fatalf("makeJoinPlan() error = %v", err)
	}

	return p, ok
}
