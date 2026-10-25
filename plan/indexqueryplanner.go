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

// CreatePlan returns the plan of the query data: its tables in the order they
// are named, each read through an index where the where clause lets it be and
// each joined to those before it through an index where it lets that be, a
// select over that for the where clause, and a projection onto the columns
// asked for.
//
// A name in the from clause that is a view is planned from its definition by
// this planner, so the tables the view reads are read through their indexes
// too, as far as the view's own where clause lets them be.
func (qp *IndexQueryPlanner) CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error) {
	var p Plan
	for _, tableName := range data.Tables() {
		tp, stored, err := qp.planTable(tableName, data.Predicate(), tx)
		if err != nil {
			return nil, err
		}

		if p == nil {
			p = tp
			continue
		}

		p, err = qp.join(p, tp, stored, data.Predicate(), tx)
		if err != nil {
			return nil, err
		}
	}

	p = NewSelectPlan(p, data.Predicate())

	return NewProjectPlan(p, data.Fields()...)
}

// planTable returns the plan of reading one name of a from clause: a view's
// definition planned again, or a stored table read through whichever of its
// indexes pred lets it be read through. For a stored table, the plan of
// reading the whole of it comes back too, since that is what a join through
// one of its indexes starts from; for a view, nil does.
func (qp *IndexQueryPlanner) planTable(tableName string, pred query.Predicate, tx *transaction.Transaction) (Plan, *TablePlan, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, nil, err
	}
	if isView {
		parser, err := parse.NewParser(definition)
		if err != nil {
			return nil, nil, fmt.Errorf("parse the definition of view %q: %w", tableName, err)
		}
		viewData, err := parser.Query()
		if err != nil {
			return nil, nil, fmt.Errorf("parse the definition of view %q: %w", tableName, err)
		}

		p, err := qp.CreatePlan(viewData, tx)

		return p, nil, err
	}

	tp, err := NewTablePlan(tx, tableName, qp.mm)
	if err != nil {
		return nil, nil, err
	}

	p, err := qp.chooseIndex(tp, pred, tx)

	return p, tp, err
}

// join returns the plan of joining right, the plan of reading the next table,
// to left, the plan of the tables before it.
//
// stored is the plan of reading the whole of that table, or nil if it is a
// view, which has no index to join through. If pred equates an indexed field
// of the table with a field of left, the two are joined through that index,
// unless right is already read through an index on a constant and the product
// with that reads less. Otherwise they are joined by a product, as
// BasicQueryPlanner joins every table.
func (qp *IndexQueryPlanner) join(left, right Plan, stored *TablePlan, pred query.Predicate, tx *transaction.Transaction) (Plan, error) {
	product, err := NewProductPlan(left, right)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return product, nil
	}

	indexJoin, err := qp.chooseJoin(left, stored, pred, tx)
	if err != nil || indexJoin == nil {
		return product, err
	}

	if _, narrowed := right.(*IndexSelectPlan); narrowed {
		return cheaperJoin(indexJoin, product), nil
	}

	return indexJoin, nil
}

// chooseJoin returns the plan of joining the table tp reads to left through
// the index of the table pred lets them be joined through for the fewest
// blocks, or nil if there is none.
//
// pred lets them be joined through an index when it equates the indexed field
// with a field of left. A field the two sides share a name for is passed over,
// since which side a term naming it means cannot be told. Of two that cost the
// same, the one on the field that comes first by name is taken, as it is by
// chooseIndex.
func (qp *IndexQueryPlanner) chooseJoin(left Plan, tp *TablePlan, pred query.Predicate, tx *transaction.Transaction) (*IndexJoinPlan, error) {
	indexes, err := qp.mm.GetIndexInfo(tx, tp.tableName)
	if err != nil {
		return nil, err
	}

	var best *IndexJoinPlan
	for _, fieldName := range slices.Sorted(maps.Keys(indexes)) {
		joinField, ok := equatesWithField(pred, fieldName)
		if !ok || !left.Schema().HasField(joinField) || left.Schema().HasField(fieldName) || tp.Schema().HasField(joinField) {
			continue
		}

		candidate, err := NewIndexJoinPlan(left, tp, indexes[fieldName], joinField)
		if err != nil {
			return nil, err
		}
		if best == nil || candidate.BlocksAccessed() < best.BlocksAccessed() {
			best = candidate
		}
	}

	return best, nil
}

// cheaperJoin returns whichever of join, the plan of joining through an index,
// and product, the plan of the product the same tables would otherwise be
// joined by, reads fewer blocks, and join if they read as many.
//
// Neither is cheaper in every case. A product reads the right side again for
// every record on the left, so it pays off only when what is on the right is
// already narrowed to very few records, by an index on a constant; otherwise
// searching the index on the join field for each record on the left reads
// less.
func cheaperJoin(join, product Plan) Plan {
	if product.BlocksAccessed() < join.BlocksAccessed() {
		return product
	}

	return join
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
