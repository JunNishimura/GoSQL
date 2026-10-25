package plan

import (
	"fmt"
	"maps"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// IndexQueryPlanner turns a select statement into a plan as BasicQueryPlanner
// does, except that a table is read through an index wherever the where clause
// lets it be.
//
// A table is read through an index when the where clause equates a field it
// has an index on with a constant: the records the clause can keep are those
// the index files under that constant, so reading those alone loses none. The
// select over the product is planned as before, and tests each record against
// the whole clause again, so a term the index stands for is tested twice and
// gives the same records.
type IndexQueryPlanner struct {
	mm *metadatamanager.MetadataManager
}

var _ QueryPlanner = (*IndexQueryPlanner)(nil)

// NewIndexQueryPlanner returns a planner that reads tables, views and indexes
// through mm.
func NewIndexQueryPlanner(mm *metadatamanager.MetadataManager) *IndexQueryPlanner {
	return &IndexQueryPlanner{
		mm: mm,
	}
}

// CreatePlan returns the plan of the query data: the product of its tables in
// the order they are named, each read through an index where the where clause
// lets it be, a select over that for the where clause, and a projection onto
// the columns asked for.
//
// A name in the from clause that is a view is planned from its definition by
// this planner, so the tables the view reads are read through their indexes
// too, as far as the view's own where clause lets them be.
func (qp *IndexQueryPlanner) CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error) {
	var p Plan
	for _, tableName := range data.Tables() {
		tp, err := qp.planTable(tableName, data.Predicate(), tx)
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

// planTable returns the plan of reading one name of a from clause: a view's
// definition planned again, or a stored table read through whichever of its
// indexes pred lets it be read through.
func (qp *IndexQueryPlanner) planTable(tableName string, pred query.Predicate, tx *transaction.Transaction) (Plan, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, err
	}
	if isView {
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

	tp, err := NewTablePlan(tx, tableName, qp.mm)
	if err != nil {
		return nil, err
	}

	return qp.chooseIndex(tp, pred, tx)
}

// chooseIndex returns the plan of reading the table tp reads through the index
// pred lets it be read through for the fewest blocks, or tp itself if pred
// equates none of its indexed fields with a constant.
//
// Of two that cost the same, the one on the field that comes first by name is
// taken, so that which one a query is read through does not turn on the order
// the indexes happen to be listed in.
func (qp *IndexQueryPlanner) chooseIndex(tp *TablePlan, pred query.Predicate, tx *transaction.Transaction) (Plan, error) {
	indexes, err := qp.mm.GetIndexInfo(tx, tp.tableName)
	if err != nil {
		return nil, err
	}

	var best *IndexSelectPlan
	for _, fieldName := range slices.Sorted(maps.Keys(indexes)) {
		value, ok := constantEquatedWith(pred, fieldName)
		if !ok {
			continue
		}

		candidate := NewIndexSelectPlan(tp, indexes[fieldName], value)
		if best == nil || candidate.BlocksAccessed() < best.BlocksAccessed() {
			best = candidate
		}
	}

	if best == nil {
		return tp, nil
	}

	return best, nil
}
