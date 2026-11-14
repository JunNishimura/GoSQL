package plan

import (
	"maps"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// tablePlanner creates the plans for one name of a from clause, for a
// heuristic query planner. The plans are a read of the table alone, a join
// with the current plan, and a product with the current plan.
//
// Each plan tests the terms of the query predicate that can be tested at that
// point, so that its output is as small as the predicate allows.
//
// The name can be a view or a stored table. A view is treated as a table that
// has the records of its plan and no index. It is therefore read completely
// and joined by a product, with the same terms as a table.
type tablePlanner struct {
	tx   *transaction.Transaction
	pred query.Predicate
	// p is the plan of a full read of the table or view.
	p Plan
	// stored is p for a stored table, and nil for a view.
	stored *TablePlan
	// indexes holds the indexes of the table, keyed by the indexed field. It
	// is empty for a view.
	indexes map[string]*metadatamanager.IndexInfo
}

// newViewPlanner returns the planner for a view under the query predicate
// pred. p is the plan of the view's definition.
func newViewPlanner(tx *transaction.Transaction, p Plan, pred query.Predicate) *tablePlanner {
	return &tablePlanner{
		tx:   tx,
		pred: pred,
		p:    p,
	}
}

// newTablePlanner returns the planner for the stored table tableName under
// the query predicate pred. It reads the statistics and indexes of the table
// through mm.
func newTablePlanner(tx *transaction.Transaction, tableName string, pred query.Predicate, mm *metadatamanager.MetadataManager) (*tablePlanner, error) {
	stored, err := NewTablePlan(tx, tableName, mm)
	if err != nil {
		return nil, err
	}

	indexes, err := mm.GetIndexInfo(tx, tableName)
	if err != nil {
		return nil, err
	}

	return &tablePlanner{
		tx:      tx,
		pred:    pred,
		p:       stored,
		stored:  stored,
		indexes: indexes,
	}, nil
}

// schema returns the fields of the records of the table or view.
func (tp *tablePlanner) schema() *recordmanager.Schema {
	return tp.p.Schema()
}

// makeSelectPlan returns the plan that reads the table alone. It reads
// through an index if the predicate equates an indexed field with a constant.
// It adds a select for the terms that can be tested against the table's
// records alone.
func (tp *tablePlanner) makeSelectPlan() Plan {
	p := tp.p
	if indexSelect, ok := cheapestIndexSelect(tp.stored, tp.indexes, tp.pred); ok {
		p = indexSelect
	}

	return tp.addSelectPred(p)
}

// makeJoinPlan returns the plan that joins the table to current, and reports
// whether the predicate has a term that joins the two.
//
// The join uses an index of the table if a join term equates its indexed
// field with a field of current. The exception is when a product with the
// join terms tested over it reads fewer blocks, as cheaperJoin decides. In
// both cases the terms on the table alone are tested on the table's side, and
// the join terms are tested over the join.
func (tp *tablePlanner) makeJoinPlan(current Plan) (Plan, bool, error) {
	joinPred, ok := tp.pred.JoinSubPred(tp.schema(), current.Schema())
	if !ok {
		return nil, false, nil
	}

	product, err := tp.makeProductPlan(current)
	if err != nil {
		return nil, false, err
	}
	productJoin := NewSelectPlan(product, joinPred)

	indexJoin, err := tp.makeIndexJoin(current, joinPred)
	if err != nil {
		return nil, false, err
	}
	if indexJoin == nil {
		return productJoin, true, nil
	}

	return cheaperJoin(indexJoin, productJoin), true, nil
}

// makeIndexJoin returns the plan that joins the table to current through an
// index of the table. Of the indexes whose field a term of joinPred equates
// with a field of current, it chooses the one that reads the fewest blocks.
// The plan tests the terms on the table alone and the join terms over the
// join. makeIndexJoin returns nil if no index qualifies.
//
// joinPred holds only terms with a field of the table on one side and a field
// of current on the other. The field that a term equates with an indexed
// field is therefore a field of current. If two indexes cost the same, the
// one whose field name sorts first is chosen, as in cheapestIndexSelect.
func (tp *tablePlanner) makeIndexJoin(current Plan, joinPred query.Predicate) (Plan, error) {
	var best Plan
	for _, fieldName := range slices.Sorted(maps.Keys(tp.indexes)) {
		joinField, ok := equatesWithField(joinPred, fieldName)
		if !ok {
			continue
		}

		indexJoin, err := NewIndexJoinPlan(current, tp.stored, tp.indexes[fieldName], joinField)
		if err != nil {
			return nil, err
		}

		candidate := NewSelectPlan(tp.addSelectPred(indexJoin), joinPred)
		if best == nil || candidate.BlocksAccessed() < best.BlocksAccessed() {
			best = candidate
		}
	}

	return best, nil
}

// makeProductPlan returns the plan for the product of current and the table.
// The table is read as in makeSelectPlan.
//
// The table is read through an index where the predicate allows it. SimpleDB
// differs here: it scans the table completely under a select. A product reads
// its right side once for each chunk of the left side, so a smaller right
// side means fewer block reads. A product is cheaper than an index join only
// when an index has reduced the right side to very few records.
//
// current is the left side, which is materialized. It is a plan of one or
// more joined tables, and computing it again for every chunk would cost more
// than reading its stored result.
func (tp *tablePlanner) makeProductPlan(current Plan) (Plan, error) {
	return NewMultibufferProductPlan(tp.tx, current, tp.makeSelectPlan())
}

// addSelectPred returns p under a select for the terms of the predicate that
// can be tested against the table's records alone. It returns p itself if
// there are no such terms.
func (tp *tablePlanner) addSelectPred(p Plan) Plan {
	selectPred, ok := tp.pred.SelectSubPred(tp.schema())
	if !ok {
		return p
	}

	return NewSelectPlan(p, selectPred)
}

// cheapestIndexSelect returns the plan that reads the table of tp through an
// index. Of the indexes whose field pred equates with a constant, it chooses
// the one that reads the fewest blocks. It also reports whether such an index
// exists.
//
// If two indexes cost the same, the one whose field name sorts first is
// chosen. The choice then does not depend on the iteration order of the
// indexes.
func cheapestIndexSelect(tp *TablePlan, indexes map[string]*metadatamanager.IndexInfo, pred query.Predicate) (*IndexSelectPlan, bool) {
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

	return best, best != nil
}
