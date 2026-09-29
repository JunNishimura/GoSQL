package plan

import (
	"errors"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestBasicQueryPlannerCreatePlan(t *testing.T) {
	t.Run("given a table of 20 records, when a query selecting id where id is 7 is planned, then the plan reads only id 7 and has only the field id", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustCreatePlan(t, tx, mm, "select id from test where id = 7")

		if got, want := p.Schema().Fields(), []string{"id"}; !slices.Equal(got, want) {
			t.Errorf("the plan has fields %v, want %v", got, want)
		}
		if got, want := openAndReadIDs(t, p), []int32{7}; !slices.Equal(got, want) {
			t.Errorf("the plan read ids %v, want %v", got, want)
		}
	})

	// The tables are multiplied in the order the query names them, the first
	// on the left. That order decides what the product costs, so it is part
	// of what the planner is asked for rather than a detail of how it works.
	t.Run("given a table of 3 records and one of 2, when a query naming both with no where clause is planned, then the plan reads every pair, the first table's records on the left", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createOtherTable(t, tx, mm, 2)

		p := mustCreatePlan(t, tx, mm, "select id, oid from test, other")

		want := [][2]int32{
			{0, 0}, {0, 1},
			{1, 0}, {1, 1},
			{2, 0}, {2, 1},
		}
		if got := openAndReadIDPairs(t, p); !slices.Equal(got, want) {
			t.Errorf("the plan read (id, oid) pairs %v, want %v", got, want)
		}
	})

	t.Run("given a table of 3 records and one of 2, when a query joining them on id equal to oid is planned, then the plan reads only the pairs that agree", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createOtherTable(t, tx, mm, 2)

		p := mustCreatePlan(t, tx, mm, "select id, oid from test, other where id = oid")

		want := [][2]int32{
			{0, 0},
			{1, 1},
		}
		if got := openAndReadIDPairs(t, p); !slices.Equal(got, want) {
			t.Errorf("the plan read (id, oid) pairs %v, want %v", got, want)
		}
	})

	t.Run("given a view of the test table where id is 3, when a query reading the view is planned, then the plan reads what the view's definition does", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		mustCreateView(t, tx, mm, "v", "select id from test where id = 3")

		p := mustCreatePlan(t, tx, mm, "select id from v")

		if got, want := openAndReadIDs(t, p), []int32{3}; !slices.Equal(got, want) {
			t.Errorf("the plan read ids %v, want %v", got, want)
		}
	})

	// A view's definition is planned the way any query is, so a view it reads
	// is expanded in turn.
	t.Run("given a view defined over another view, when a query reading the outer view is planned, then the plan reads through both definitions", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)
		mustCreateView(t, tx, mm, "inner", "select id from test where id = 3")
		mustCreateView(t, tx, mm, "outer", "select id from inner")

		p := mustCreatePlan(t, tx, mm, "select id from outer")

		if got, want := openAndReadIDs(t, p), []int32{3}; !slices.Equal(got, want) {
			t.Errorf("the plan read ids %v, want %v", got, want)
		}
	})

	t.Run("given a query naming a table that is neither a table nor a view, when it is planned, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewBasicQueryPlanner(mm).CreatePlan(mustParseQuery(t, "select id from missing"), tx)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("CreatePlan() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}
	})
}

func mustParseQuery(t *testing.T, sql string) parse.QueryData {
	t.Helper()

	p, err := parse.NewParser(sql)
	if err != nil {
		t.Fatalf("NewParser(%q) error = %v", sql, err)
	}
	data, err := p.Query()
	if err != nil {
		t.Fatalf("Query() of %q error = %v", sql, err)
	}

	return data
}

func mustCreatePlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, sql string) Plan {
	t.Helper()

	p, err := NewBasicQueryPlanner(mm).CreatePlan(mustParseQuery(t, sql), tx)
	if err != nil {
		t.Fatalf("CreatePlan(%q) error = %v", sql, err)
	}

	return p
}

func mustCreateView(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager, viewName string, definition string) {
	t.Helper()

	if err := mm.CreateView(tx, viewName, definition); err != nil {
		t.Fatalf("CreateView(%q) error = %v", viewName, err)
	}
}

func openAndReadIDs(t *testing.T, p Plan) []int32 {
	t.Helper()

	s, err := p.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	return readIDs(t, s)
}

func openAndReadIDPairs(t *testing.T, p Plan) [][2]int32 {
	t.Helper()

	s, err := p.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	return readIDPairs(t, s)
}
