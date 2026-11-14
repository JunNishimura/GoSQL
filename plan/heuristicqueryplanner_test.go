package plan

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// thirdTableName is the table that a case with three tables adds to the test
// table and the other table. Its only field, tid, is an int.
const thirdTableName = "third"

func TestHeuristicQueryPlannerCreatePlan(t *testing.T) {
	tests := []struct {
		name       string
		testCount  int
		otherCount int
		sql        string
		want       [][2]int32
	}{
		{
			name:       "given a table of 20 records and another of 3, when a query where id is 7 and oid is 0 is planned, then the plan reads only the pair of 7 and 0",
			testCount:  20,
			otherCount: 3,
			sql:        "select id, oid from test, other where id = 7 and oid = 0",
			want:       [][2]int32{{7, 0}},
		},
		{
			name:       "given a table of 20 records and another of 3, when a query joining them where id is oid is planned, then the plan pairs each oid with the same id",
			testCount:  20,
			otherCount: 3,
			sql:        "select id, oid from test, other where id = oid",
			want:       [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		{
			name:       "given a table of 20 records and another of 3, when a query with no term joining them where id is 3 is planned, then the plan puts id 3 beside each oid",
			testCount:  20,
			otherCount: 3,
			sql:        "select id, oid from test, other where id = 3",
			want:       [][2]int32{{3, 0}, {3, 1}, {3, 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, tt.testCount)
			createOtherTable(t, tx, mm, tt.otherCount)

			p := mustCreateHeuristicPlan(t, tx, mm, tt.sql)

			if got := sortedPairs(openAndReadIDPairs(t, p)); !slices.Equal(got, tt.want) {
				t.Errorf("the plan read (id, oid) pairs %v, want %v", got, tt.want)
			}
		})
	}

	// Every term must be tested at some point. A term with a field that none
	// of the tables has cannot be tested anywhere, and omitting it would keep
	// the records that it was meant to drop.
	t.Run("given a table of 20 records, when a query where a field none of its tables has is 1 is planned, then it reports ErrFieldNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		sql := "select id from test where missing = 1"
		_, err := NewHeuristicQueryPlanner(mm).CreatePlan(mustParseQuery(t, sql), tx)
		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("CreatePlan(%q) error = %v, want %v", sql, err, recordmanager.ErrFieldNotFound)
		}
	})
}

// A view in the from clause is planned from its definition by the same
// planner. It is then treated as one more table, with the fields that the
// definition selects and no index.
func TestHeuristicQueryPlannerCreatePlanViews(t *testing.T) {
	t.Run("given a view of the test table where id is 3, which is indexed on id, when a query reading the view is planned, then the plan reads the table through the index for 3", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		createIndexesThrough(t, tx, mm, []string{"id"})
		mustCreateView(t, tx, mm, "v", "select id from test where id = 3")

		p := mustCreateHeuristicPlan(t, tx, mm, "select id from v")

		if got, ok := indexSearchOf(p); !ok || got != query.NewIntConstant(3) {
			t.Errorf("the plan reads through an index for %s (%t), want one for 3", got, ok)
		}
		if got, want := openAndReadIDs(t, p), []int32{3}; !slices.Equal(got, want) {
			t.Errorf("the plan read ids %v, want %v", got, want)
		}
	})

	t.Run("given a view of the ids of a table of 20 records and a table of 3, when a query joining them where id is oid and id is 1 is planned, then the plan reads the one pair of 1", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		createOtherTable(t, tx, mm, 3)
		mustCreateView(t, tx, mm, "v", "select id from test")

		p := mustCreateHeuristicPlan(t, tx, mm, "select id, oid from v, other where id = oid and id = 1")

		if got, want := sortedPairs(openAndReadIDPairs(t, p)), [][2]int32{{1, 1}}; !slices.Equal(got, want) {
			t.Errorf("the plan read (id, oid) pairs %v, want %v", got, want)
		}
	})

	// The view selects only id. name is a field of the underlying table but
	// not of the view, so a query on the view cannot use it.
	t.Run("given a view of the ids of the test table, when a query reading the view where name is 'name1' is planned, then it reports ErrFieldNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		mustCreateView(t, tx, mm, "v", "select id from test")

		sql := "select id from v where name = 'name1'"
		_, err := NewHeuristicQueryPlanner(mm).CreatePlan(mustParseQuery(t, sql), tx)
		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("CreatePlan(%q) error = %v, want %v", sql, err, recordmanager.ErrFieldNotFound)
		}
	})
}

// The join order of a plan is read from the plan itself, from left to right.
// The estimates that determine the order come from the statistics, which
// estimate 1 + n/3 distinct values for every field of a table with n records:
//
//	a table of 20 records, 7 distinct values; where id is a constant, 20/7 = 2
//	a table of 30 records, 11 distinct values
//	a table of 3 records, 2 distinct values
//	a table of 1 or 2 records, 1 distinct value
func TestHeuristicQueryPlannerCreatePlanOrder(t *testing.T) {
	tests := []struct {
		name       string
		testCount  int
		otherCount int
		thirdCount int
		sql        string
		wantOrder  []string
		want       [][2]int32
	}{
		{
			name:       "given a table of 20 records named first and another of 3, when a query joining them is planned, then the plan starts from the other, which gives back fewer records",
			testCount:  20,
			otherCount: 3,
			sql:        "select id, oid from test, other where id = oid",
			wantOrder:  []string{otherTableName, testTableName},
			want:       [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		{
			name:       "given a table of 3 records named first and another of 20 where id is 1, when a query joining them is planned, then the plan starts from the table of 20, which the term on id cuts to 2",
			testCount:  20,
			otherCount: 3,
			sql:        "select id, oid from other, test where id = oid and id = 1",
			wantOrder:  []string{testTableName, otherTableName},
			want:       [][2]int32{{1, 1}},
		},
		{
			name:       "given two tables of 3 records each, when a query joining them is planned, then the plan starts from the one named first in the from clause",
			testCount:  3,
			otherCount: 3,
			sql:        "select id, oid from other, test where id = oid",
			wantOrder:  []string{otherTableName, testTableName},
			want:       [][2]int32{{0, 0}, {1, 1}, {2, 2}},
		},
		// Starting from third, the join with other on oid is estimated at
		// 2*30/11 = 5 records, and the product with test at 2*2 = 4. The
		// product returns fewer records, but a join is always preferred.
		{
			name:       "given a table of 2 records joined to one of 30 and not to one of 20 where id is 5, when a query of the three is planned, then the plan joins the table of 30 before putting the other beside them",
			testCount:  20,
			otherCount: 30,
			thirdCount: 2,
			sql:        "select id, oid from third, other, test where oid = tid and id = 5",
			wantOrder:  []string{thirdTableName, otherTableName, testTableName},
			want:       [][2]int32{{5, 0}, {5, 1}},
		},
		// Starting from third, the join with other on oid is estimated at
		// 1*3/2 = 1 record, and the join with test on id at 1*20/7 = 2. other
		// is therefore joined first, although test comes first in the query.
		{
			name:       "given a table of 1 record joined to one of 20 and to one of 3, when a query of the three is planned, then the plan joins the table of 3 first, which gives back fewer records",
			testCount:  20,
			otherCount: 3,
			thirdCount: 1,
			sql:        "select id, oid from third, test, other where id = tid and oid = tid",
			wantOrder:  []string{thirdTableName, otherTableName, testTableName},
			want:       [][2]int32{{0, 0}},
		},
		// No term joins any two of these tables. third with tid = 0 is
		// estimated at 1/1 = 1 record, other with oid = 2 at 3/2 = 1, and test
		// with id = 1 at 2. third comes first because it is written first.
		// other is then added at 1*1 = 1, before test at 1*2 = 2.
		{
			name:       "given a table of 1 record, one of 20 and one of 3, none joined to another, when a query of the three is planned, then the plan puts the table of 3 beside the first before the table of 20, since that product gives back fewer records",
			testCount:  20,
			otherCount: 3,
			thirdCount: 1,
			sql:        "select id, oid from third, test, other where tid = 0 and id = 1 and oid = 2",
			wantOrder:  []string{thirdTableName, otherTableName, testTableName},
			want:       [][2]int32{{1, 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			mm := newTestMetadataManager(t, tx)
			createTestTable(t, tx, mm, tt.testCount)
			createOtherTable(t, tx, mm, tt.otherCount)
			if tt.thirdCount > 0 {
				createThirdTable(t, tx, mm, tt.thirdCount)
			}

			p := mustCreateHeuristicPlan(t, tx, mm, tt.sql)

			if got := tableOrderOf(p); !slices.Equal(got, tt.wantOrder) {
				t.Errorf("the plan joins its tables in the order %v, want %v", got, tt.wantOrder)
			}
			if got := sortedPairs(openAndReadIDPairs(t, p)); !slices.Equal(got, tt.want) {
				t.Errorf("the plan read (id, oid) pairs %v, want %v", got, tt.want)
			}
		})
	}
}

// mustCreateHeuristicPlan plans sql through a heuristic query planner.
func mustCreateHeuristicPlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string) Plan {
	t.Helper()

	p, err := NewHeuristicQueryPlanner(mm).CreatePlan(mustParseQuery(t, sql), tx)
	if err != nil {
		t.Fatalf("CreatePlan(%q) error = %v", sql, err)
	}
	if p == nil {
		t.Fatalf("CreatePlan(%q) = nil, want a plan", sql)
	}

	return p
}

// tableOrderOf returns the names of the tables that p reads, in join order,
// starting with the first table of the plan.
func tableOrderOf(p Plan) []string {
	switch p := p.(type) {
	case *ProjectPlan:
		return tableOrderOf(p.p)
	case *SelectPlan:
		return tableOrderOf(p.p)
	case *MultibufferProductPlan:
		return slices.Concat(tableOrderOf(p.lhs.src), tableOrderOf(p.rhs))
	case *IndexJoinPlan:
		return slices.Concat(tableOrderOf(p.p1), []string{p.p2.tableName})
	case *IndexSelectPlan:
		return []string{p.tp.tableName}
	case *TablePlan:
		return []string{p.tableName}
	default:
		return []string{fmt.Sprintf("%T", p)}
	}
}

// createThirdTable creates the third table and writes count records to it.
// The i-th record has tid i.
func createThirdTable(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, count int) {
	t.Helper()

	if err := mm.CreateTable(tx, thirdTableName, newIntSchema(t, "tid")); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", thirdTableName, err)
	}

	layout, err := mm.GetLayout(tx, thirdTableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", thirdTableName, err)
	}

	ts, err := query.NewTableScan(tx, thirdTableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", thirdTableName, err)
	}
	defer ts.Close()

	for i := range count {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("tid", int32(i)); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
	}
}
