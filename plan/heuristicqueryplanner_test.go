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

// thirdTableName is the table a case of three tables adds to the test and the
// other table. Its one field, tid, is an int.
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

	// Every term has to be tested somewhere. One naming a field none of the
	// tables has can be tested nowhere, and leaving it out would keep the
	// records it was written to drop.
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

// The order a plan joins its tables in is read off the plan itself, left to
// right. The guesses that order turns on are worked out from the statistics,
// which guess 1 + n/3 distinct values for every field of a table of n records:
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
		// From third, other joined on oid is guessed at 2*30/11 = 5 records,
		// and test put beside it at 2*2 = 4. The product gives back fewer, but
		// a join is taken whenever there is one.
		{
			name:       "given a table of 2 records joined to one of 30 and not to one of 20 where id is 5, when a query of the three is planned, then the plan joins the table of 30 before putting the other beside them",
			testCount:  20,
			otherCount: 30,
			thirdCount: 2,
			sql:        "select id, oid from third, other, test where oid = tid and id = 5",
			wantOrder:  []string{thirdTableName, otherTableName, testTableName},
			want:       [][2]int32{{5, 0}, {5, 1}},
		},
		// From third, other joined on oid is guessed at 1*3/2 = 1 record and
		// test joined on id at 1*20/7 = 2, so other is joined first though
		// test is named before it.
		{
			name:       "given a table of 1 record joined to one of 20 and to one of 3, when a query of the three is planned, then the plan joins the table of 3 first, which gives back fewer records",
			testCount:  20,
			otherCount: 3,
			thirdCount: 1,
			sql:        "select id, oid from third, test, other where id = tid and oid = tid",
			wantOrder:  []string{thirdTableName, otherTableName, testTableName},
			want:       [][2]int32{{0, 0}},
		},
		// No term joins any two of these. Third, where tid is 0, is guessed at
		// 1/1 = 1 record, other where oid is 2 at 3/2 = 1, and test where id is
		// 1 at 2, so third goes first, being named first, and other is put
		// beside it at 1*1 = 1 before test at 1*2 = 2.
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

// tableOrderOf returns the names of the tables p reads, in the order it joins
// them: the table it starts from first.
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

// createThirdTable makes the third table and writes count records into it,
// the i-th with a tid of i.
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
