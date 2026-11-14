package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

// QueryPlanner creates a plan from a parsed select statement.
//
// It is an interface so that the query planner of a Planner can be replaced
// with one that compares plans, without a change to Planner or its callers.
type QueryPlanner interface {
	CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error)
}

// UpdatePlanner executes parsed statements that change the database. It has
// one method per kind of statement, and each method returns the number of
// records that it changed.
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

// Planner is the entry point for SQL text. It parses a statement and passes
// it to the planner for that kind of statement.
//
// Planner does not interpret a statement itself. The planners behind it
// define what a statement does. The owner of the transaction is responsible
// for rolling back the changes of a failed statement.
type Planner struct {
	qp QueryPlanner
	up UpdatePlanner
}

// NewPlanner returns a planner that passes select statements to qp and every
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

// ExecuteUpdate parses sql as a statement that changes the database, executes
// it, and returns the number of records that it changed.
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
		// The parser produces no other type. This line is reached only if a
		// new statement type is added to the parser but not to this switch.
		return 0, fmt.Errorf("execute %q: no planner method for a %T", sql, cmd)
	}
}

// parseViewDefinition parses definition, the select statement that the view
// viewName was created with, so that a query planner can plan the view.
func parseViewDefinition(viewName, definition string) (parse.QueryData, error) {
	parser, err := parse.NewParser(definition)
	if err != nil {
		return parse.QueryData{}, fmt.Errorf("parse the definition of view %q: %w", viewName, err)
	}

	data, err := parser.Query()
	if err != nil {
		return parse.QueryData{}, fmt.Errorf("parse the definition of view %q: %w", viewName, err)
	}

	return data, nil
}
