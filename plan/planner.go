package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

// QueryPlanner turns a parsed select statement into a plan.
//
// It is an interface so that the planner behind a Planner can be swapped for
// one that weighs plans against each other, without the Planner or its
// callers changing.
type QueryPlanner interface {
	CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error)
}

// UpdatePlanner carries out parsed statements that change the database, one
// method to a kind of statement. Each reports how many records it changed.
type UpdatePlanner interface {
	ExecuteInsert(data parse.InsertData, tx *transaction.Transaction) (int, error)
	ExecuteDelete(data parse.DeleteData, tx *transaction.Transaction) (int, error)
	ExecuteModify(data parse.ModifyData, tx *transaction.Transaction) (int, error)
	ExecuteCreateTable(data parse.CreateTableData, tx *transaction.Transaction) (int, error)
	ExecuteCreateView(data parse.CreateViewData, tx *transaction.Transaction) (int, error)
	ExecuteCreateIndex(data parse.CreateIndexData, tx *transaction.Transaction) (int, error)
}

var (
	_ QueryPlanner  = (*BasicQueryPlanner)(nil)
	_ UpdatePlanner = (*BasicUpdatePlanner)(nil)
)

// Planner is the way in from SQL text: it parses a statement and hands it to
// whichever planner deals with its kind.
//
// It does nothing to a statement itself. What a statement means is the
// business of the planners behind it, and undoing what a failed one left
// behind is the business of whoever holds the transaction.
type Planner struct {
	qp QueryPlanner
	up UpdatePlanner
}

// NewPlanner returns a planner that hands select statements to qp and every
// other statement to up.
func NewPlanner(qp QueryPlanner, up UpdatePlanner) *Planner {
	return &Planner{
		qp: qp,
		up: up,
	}
}

// CreateQueryPlan parses sql as a select statement and returns its plan.
func (p *Planner) CreateQueryPlan(sql string, tx *transaction.Transaction) (Plan, error) {
	parser, err := parse.NewParser(sql)
	if err != nil {
		return nil, err
	}

	data, err := parser.Query()
	if err != nil {
		return nil, err
	}

	return p.qp.CreatePlan(data, tx)
}

// ExecuteUpdate parses sql as a statement that changes the database, carries
// it out, and reports how many records it changed.
func (p *Planner) ExecuteUpdate(sql string, tx *transaction.Transaction) (int, error) {
	parser, err := parse.NewParser(sql)
	if err != nil {
		return 0, err
	}

	cmd, err := parser.UpdateCmd()
	if err != nil {
		return 0, err
	}

	switch data := cmd.(type) {
	case parse.InsertData:
		return p.up.ExecuteInsert(data, tx)
	case parse.DeleteData:
		return p.up.ExecuteDelete(data, tx)
	case parse.ModifyData:
		return p.up.ExecuteModify(data, tx)
	case parse.CreateTableData:
		return p.up.ExecuteCreateTable(data, tx)
	case parse.CreateViewData:
		return p.up.ExecuteCreateView(data, tx)
	case parse.CreateIndexData:
		return p.up.ExecuteCreateIndex(data, tx)
	default:
		// The parser makes no other kind, so this is reached only if it grows
		// one that was not added here.
		return 0, fmt.Errorf("execute %q: no planner method for a %T", sql, cmd)
	}
}
