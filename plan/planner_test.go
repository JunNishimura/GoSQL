package plan

import (
	"errors"
	"testing"

	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

// errFromPlanner is what the fake planners fail with when a case asks them to,
// so that a case can tell an error passed up from one the planner made itself.
var errFromPlanner = errors.New("error from planner")

// fakeQueryPlanner keeps the query data it was last given, and answers with
// the plan and error it was set to.
type fakeQueryPlanner struct {
	got  parse.QueryData
	plan Plan
	err  error
}

var _ QueryPlanner = (*fakeQueryPlanner)(nil)

func (fq *fakeQueryPlanner) CreatePlan(data parse.QueryData, _ *transaction.Transaction) (Plan, error) {
	fq.got = data
	return fq.plan, fq.err
}

// fakeUpdatePlanner keeps which of its methods was last called and the name
// of the table, view or index it was about. Each method reports a count of its
// own, so that a case can tell whose count came back.
type fakeUpdatePlanner struct {
	called string
	name   string
	err    error
}

var _ UpdatePlanner = (*fakeUpdatePlanner)(nil)

func (fu *fakeUpdatePlanner) ExecuteInsert(data parse.InsertData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteInsert", data.TableName()
	return 1, fu.err
}

func (fu *fakeUpdatePlanner) ExecuteDelete(data parse.DeleteData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteDelete", data.TableName()
	return 2, fu.err
}

func (fu *fakeUpdatePlanner) ExecuteModify(data parse.ModifyData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteModify", data.TableName()
	return 3, fu.err
}

func (fu *fakeUpdatePlanner) ExecuteCreateTable(data parse.CreateTableData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteCreateTable", data.TableName()
	return 4, fu.err
}

func (fu *fakeUpdatePlanner) ExecuteCreateView(data parse.CreateViewData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteCreateView", data.ViewName()
	return 5, fu.err
}

func (fu *fakeUpdatePlanner) ExecuteCreateIndex(data parse.CreateIndexData, _ *transaction.Transaction) (int, error) {
	fu.called, fu.name = "ExecuteCreateIndex", data.IndexName()
	return 6, fu.err
}

func TestPlannerCreateQueryPlan(t *testing.T) {
	t.Run("when a select statement is planned, then the query planner is given it parsed and its plan comes back", func(t *testing.T) {
		const sql = "select id from test where id = 7"
		want := &fakePlan{}
		qp := &fakeQueryPlanner{plan: want}

		got, err := NewPlanner(qp, &fakeUpdatePlanner{}).CreateQueryPlan(sql, nil)
		if err != nil {
			t.Fatalf("CreateQueryPlan() error = %v", err)
		}

		if qp.got.String() != sql {
			t.Errorf("the query planner was given %q, want %q", qp.got.String(), sql)
		}
		if got != want {
			t.Errorf("CreateQueryPlan() = %v, want the query planner's plan %v", got, want)
		}
	})

	t.Run("when a statement that does not parse is planned, then it reports ErrBadSyntax", func(t *testing.T) {
		_, err := NewPlanner(&fakeQueryPlanner{}, &fakeUpdatePlanner{}).CreateQueryPlan("select from test", nil)
		if !errors.Is(err, parse.ErrBadSyntax) {
			t.Errorf("CreateQueryPlan() error = %v, want %v", err, parse.ErrBadSyntax)
		}
	})

	t.Run("given a query planner that fails, when a select statement is planned, then its error comes back", func(t *testing.T) {
		qp := &fakeQueryPlanner{err: errFromPlanner}

		_, err := NewPlanner(qp, &fakeUpdatePlanner{}).CreateQueryPlan("select id from test", nil)
		if !errors.Is(err, errFromPlanner) {
			t.Errorf("CreateQueryPlan() error = %v, want %v", err, errFromPlanner)
		}
	})
}

func TestPlannerExecuteUpdate(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		wantCalled string
		wantName   string
		wantCount  int
	}{
		{
			name:       "when an insert is executed, then it goes to ExecuteInsert and its count comes back",
			sql:        "insert into test (id) values (5)",
			wantCalled: "ExecuteInsert",
			wantName:   "test",
			wantCount:  1,
		},
		{
			name:       "when a delete is executed, then it goes to ExecuteDelete and its count comes back",
			sql:        "delete from test where id = 7",
			wantCalled: "ExecuteDelete",
			wantName:   "test",
			wantCount:  2,
		},
		{
			name:       "when an update is executed, then it goes to ExecuteModify and its count comes back",
			sql:        "update test set name = 'x' where id = 7",
			wantCalled: "ExecuteModify",
			wantName:   "test",
			wantCount:  3,
		},
		{
			name:       "when a create table is executed, then it goes to ExecuteCreateTable and its count comes back",
			sql:        "create table test (id int)",
			wantCalled: "ExecuteCreateTable",
			wantName:   "test",
			wantCount:  4,
		},
		{
			name:       "when a create view is executed, then it goes to ExecuteCreateView and its count comes back",
			sql:        "create view v as select id from test",
			wantCalled: "ExecuteCreateView",
			wantName:   "v",
			wantCount:  5,
		},
		{
			name:       "when a create index is executed, then it goes to ExecuteCreateIndex and its count comes back",
			sql:        "create index idx on test (id)",
			wantCalled: "ExecuteCreateIndex",
			wantName:   "idx",
			wantCount:  6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := &fakeUpdatePlanner{}

			count, err := NewPlanner(&fakeQueryPlanner{}, up).ExecuteUpdate(tt.sql, nil)
			if err != nil {
				t.Fatalf("ExecuteUpdate() error = %v", err)
			}

			if up.called != tt.wantCalled {
				t.Errorf("the update planner's %s was called, want %s", up.called, tt.wantCalled)
			}
			if up.name != tt.wantName {
				t.Errorf("the update planner was given %q, want %q", up.name, tt.wantName)
			}
			if count != tt.wantCount {
				t.Errorf("ExecuteUpdate() = %d, want %d", count, tt.wantCount)
			}
		})
	}

	t.Run("when a select statement is executed as an update, then it reports ErrBadSyntax", func(t *testing.T) {
		_, err := NewPlanner(&fakeQueryPlanner{}, &fakeUpdatePlanner{}).ExecuteUpdate("select id from test", nil)
		if !errors.Is(err, parse.ErrBadSyntax) {
			t.Errorf("ExecuteUpdate() error = %v, want %v", err, parse.ErrBadSyntax)
		}
	})

	t.Run("given an update planner that fails, when an insert is executed, then its error comes back", func(t *testing.T) {
		up := &fakeUpdatePlanner{err: errFromPlanner}

		_, err := NewPlanner(&fakeQueryPlanner{}, up).ExecuteUpdate("insert into test (id) values (5)", nil)
		if !errors.Is(err, errFromPlanner) {
			t.Errorf("ExecuteUpdate() error = %v, want %v", err, errFromPlanner)
		}
	})
}
