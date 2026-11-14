package plan

import (
	"maps"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// tablePlanner works out the plans of one name of a from clause for a
// heuristic query planner: reading it on its own, joining it to what is
// already planned, and putting it beside what is already planned.
//
// Each plan tests the terms of the query's predicate that can be tested where
// it reads, so that what it hands on is as small as the predicate lets it be.
//
// The name may be a view as well as a stored table. A view is taken as a table
// whose records are those its plan gives back and which has no index, so it
// is read in full and joined by a product, under the same terms a table would
// be.
type tablePlanner struct {
	tx   *transaction.Transaction
	pred query.Predicate
	// p is the plan of reading the whole of the table, or of the view.
	p Plan
	// stored is p, for a stored table, and nil for a view.
	stored *TablePlan
	// indexes are the indexes of the table, by the field each is on, and none
	// for a view.
	indexes map[string]*metadatamanager.IndexInfo
}

// newViewPlanner returns the planner of a view, whose definition p is the plan
// of, under the query predicate pred.
func newViewPlanner(tx *transaction.Transaction, p Plan, pred query.Predicate) *tablePlanner {
	return &tablePlanner{
		tx:   tx,
		pred: pred,
		p:    p,
	}
}

// newTablePlanner returns the planner of the stored table tableName under the
// query predicate pred, reading the table's statistics and indexes through mm.
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

// schema is the fields of the records of the table, or of the view.
func (tp *tablePlanner) schema() *recordmanager.Schema {
	return tp.p.Schema()
}

// makeSelectPlan returns the plan of reading the table on its own: through an
// index where the predicate equates an indexed field with a constant, and
// under a select of the terms that can be tested against its records alone.
func (tp *tablePlanner) makeSelectPlan() Plan {
	p := tp.p
	if indexSelect, ok := cheapestIndexSelect(tp.stored, tp.indexes, tp.pred); ok {
		p = indexSelect
	}

	return tp.addSelectPred(p)
}

// makeJoinPlan returns the plan of joining the table to current, and whether
// the predicate has a term that joins them at all.
//
// The join goes through an index of the table where a join term equates its
// indexed field with a field of current, unless putting the two beside each
// other and testing the join terms over that reads fewer blocks, as
// cheaperJoin weighs them. Either way, the terms on the table alone are tested
// on its side of the join and the join terms over the join.
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

// makeIndexJoin returns the plan of joining the table to current through
// whichever of its indexes a term of joinPred equates with a field of current
// for the fewest blocks, with the terms on the table alone and the join terms
// tested over it, or nil if there is none.
//
// joinPred holds only terms with a field of the table on one side and a field
// of current on the other, so the field a term equates an indexed field with
// is one of current's. Of two that cost the same, the one on the field that
// comes first by name is taken, as it is by cheapestIndexSelect.
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

// makeProductPlan returns the plan of putting every record of current beside
// every record of the table, read as makeSelectPlan reads it.
//
// The table is read through an index where the predicate lets it be, unlike
// SimpleDB, which reads it in full under a select. A product reads its right
// side once for each chunk of the left, so the fewer records it is narrowed
// to before that, the less is read; and it is when an index has narrowed it to
// very few that a product is cheaper than a join through an index at all.
//
// current goes on the left, where it is materialized, since it is a plan of
// one or more tables already joined and working it out again for every chunk
// would cost more than reading back what it gave the first time.
func (tp *tablePlanner) makeProductPlan(current Plan) (Plan, error) {
	return NewMultibufferProductPlan(tp.tx, current, tp.makeSelectPlan())
}

// addSelectPred returns p under a select of the terms of the predicate that
// can be tested against the table's records alone, or p itself if there are
// none.
func (tp *tablePlanner) addSelectPred(p Plan) Plan {
	selectPred, ok := tp.pred.SelectSubPred(tp.schema())
	if !ok {
		return p
	}

	return NewSelectPlan(p, selectPred)
}

// cheapestIndexSelect returns the plan of reading the table tp reads through
// whichever of indexes pred equates with a constant for the fewest blocks,
// and whether there is one.
//
// Of two that cost the same, the one on the field that comes first by name is
// taken, so that which one a query is read through does not turn on the order
// the indexes happen to be listed in.
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
