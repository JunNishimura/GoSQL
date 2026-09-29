package plan

import (
	"fmt"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

// BasicQueryPlanner turns a select statement into a plan the simplest way
// there is: the product of its tables in the order they are named, a select
// over that for the where clause, and a projection onto the columns asked for.
//
// It weighs nothing. The plan it builds is the one the query reads as, however
// much a different order of the same tables would save, which makes it the
// baseline a smarter planner is measured against rather than one to run a
// large query through.
type BasicQueryPlanner struct {
	mm *metadatamanager.MetadataManager
}

// NewBasicQueryPlanner returns a planner that reads tables and views through
// mm.
func NewBasicQueryPlanner(mm *metadatamanager.MetadataManager) *BasicQueryPlanner {
	return &BasicQueryPlanner{
		mm: mm,
	}
}

// CreatePlan returns the plan of the query data.
//
// A name in the from clause that is a view is planned from its definition,
// which is parsed again and planned the way any query is. A view defined over
// another view is expanded in turn by the same call.
func (qp *BasicQueryPlanner) CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error) {
	var p Plan
	for _, tableName := range data.Tables() {
		tp, err := qp.planTable(tableName, tx)
		if err != nil {
			return nil, err
		}

		if p == nil {
			p = tp
			continue
		}

		p, err = NewProductPlan(p, tp)
		if err != nil {
			return nil, err
		}
	}

	p = NewSelectPlan(p, data.Predicate())

	return NewProjectPlan(p, data.Fields()...)
}

// planTable returns the plan of reading one name of a from clause, which is
// either a view or a stored table.
func (qp *BasicQueryPlanner) planTable(tableName string, tx *transaction.Transaction) (Plan, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, err
	}
	if !isView {
		return NewTablePlan(tx, tableName, qp.mm)
	}

	parser, err := parse.NewParser(definition)
	if err != nil {
		return nil, fmt.Errorf("parse the definition of view %q: %w", tableName, err)
	}
	viewData, err := parser.Query()
	if err != nil {
		return nil, fmt.Errorf("parse the definition of view %q: %w", tableName, err)
	}

	return qp.CreatePlan(viewData, tx)
}
