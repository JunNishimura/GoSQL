package plan

import (
	"maps"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// IndexQueryPlanner creates a plan for a select statement in the same way as
// BasicQueryPlanner, except that it reads a table through an index whenever
// the where clause allows it.
//
// A table can be read through an index when the where clause equates an
// indexed field of the table with a constant. Only the records that the index
// holds for that constant can satisfy the clause, so reading only those
// records loses no result. The select over the product is planned as before
// and tests each record against the whole clause. A term that the index
// already covers is therefore tested twice, with the same result.
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

// CreatePlan returns the plan for the query data. The tables are joined in
// the order written. Each table is read through an index where the where
// clause allows it, and joined to the earlier tables through an index where
// the clause allows that. A select for the where clause and a projection onto
// the requested columns follow.
//
// A name in the from clause that is a view is planned from its definition by
// this planner. The tables of the view are therefore also read through their
// indexes, as far as the where clause of the view allows.
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

// planTable returns the plan for one name of a from clause. For a view, this
// is the plan of its definition. For a stored table, it is a read through the
// index that pred allows, if any. For a stored table, planTable also returns
// the plan of a full table scan, because an index join starts from that plan.
// For a view it returns nil instead.
func (qp *IndexQueryPlanner) planTable(tableName string, pred query.Predicate, tx *transaction.Transaction) (Plan, *TablePlan, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, nil, err
	}
	if isView {
		viewData, err := parseViewDefinition(tableName, definition)
		if err != nil {
			return nil, nil, err
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

// join returns the plan that joins right, the plan of the next table, to
// left, the plan of the earlier tables.
//
// stored is the plan of a full scan of that table, or nil for a view, which
// has no index to join through. If pred equates an indexed field of the table
// with a field of left, the two are joined through that index. The exception
// is a right side that is already read through an index on a constant, when
// the product with it reads fewer blocks. In all other cases the two are
// joined by a product, as BasicQueryPlanner joins every table.
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

// chooseJoin returns the plan that joins the table of tp to left through an
// index of that table. Of the indexes that pred allows, it chooses the one
// that reads the fewest blocks. It returns nil if pred allows none.
//
// pred allows a join through an index when it equates the indexed field with
// a field of left. A field name that both sides have is skipped, because a
// term with that name is ambiguous. If two indexes cost the same, the one
// whose field name sorts first is chosen, as in chooseIndex.
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

// cheaperJoin returns the plan that reads fewer blocks: join, the index join,
// or product, the product of the same tables. It returns join if both read
// the same number of blocks.
//
// Neither is cheaper in every case. A product reads its right side again for
// every left record. It is therefore cheaper only when the right side is
// already reduced to very few records by an index on a constant. Otherwise an
// index search on the join field for each left record reads fewer blocks.
func cheaperJoin(join, product Plan) Plan {
	if product.BlocksAccessed() < join.BlocksAccessed() {
		return product
	}

	return join
}

// chooseIndex returns the plan that reads the table of tp through the
// cheapest index that pred allows, as cheapestIndexSelect chooses it. It
// returns tp itself if pred equates none of the indexed fields with a
// constant.
func (qp *IndexQueryPlanner) chooseIndex(tp *TablePlan, pred query.Predicate, tx *transaction.Transaction) (Plan, error) {
	indexes, err := qp.mm.GetIndexInfo(tx, tp.tableName)
	if err != nil {
		return nil, err
	}

	if best, ok := cheapestIndexSelect(tp, indexes, pred); ok {
		return best, nil
	}

	return tp, nil
}
