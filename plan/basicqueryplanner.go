package plan

import (
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/transaction"
)

// BasicQueryPlanner creates the simplest possible plan for a select
// statement: the product of its tables in the order written, a select over
// the product for the where clause, and a projection onto the requested
// columns.
//
// It compares no alternatives. Its plan follows the text of the query, even
// if another order of the same tables would be much cheaper. It is therefore
// a baseline for better planners, not a planner for large queries. It scans
// every table completely and ignores indexes. IndexQueryPlanner is the
// planner that uses them.
type BasicQueryPlanner struct {
	mm *metadatamanager.MetadataManager
}

// NewBasicQueryPlanner returns a planner that reads the metadata of tables
// and views through mm.
func NewBasicQueryPlanner(mm *metadatamanager.MetadataManager) *BasicQueryPlanner {
	return &BasicQueryPlanner{
		mm: mm,
	}
}

// CreatePlan returns the plan for the query data.
//
// A name in the from clause that is a view is planned from its definition.
// The definition is parsed again and planned like any other query. A view
// that is defined on another view is expanded recursively by the same call.
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

// planTable returns the plan for one name of a from clause, which is either a
// view or a stored table.
func (qp *BasicQueryPlanner) planTable(tableName string, tx *transaction.Transaction) (Plan, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, err
	}
	if !isView {
		return NewTablePlan(tx, tableName, qp.mm)
	}

	viewData, err := parseViewDefinition(tableName, definition)
	if err != nil {
		return nil, err
	}

	return qp.CreatePlan(viewData, tx)
}
