package plan

import (
	"maps"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// tablePlanner works out the plans of one name of a from clause for a
// heuristic query planner: reading it on its own, joining it to what is
// already planned, and putting it beside what is already planned.
//
// Each plan tests the terms of the query's predicate that can be tested where
// it reads, so that what it hands on is as small as the predicate lets it be.
type tablePlanner struct {
	tx   *transaction.Transaction
	pred query.Predicate
	// stored is the plan of reading the whole of the table.
	stored *TablePlan
	// indexes are the indexes of the table, by the field each is on.
	indexes map[string]*metadatamanager.IndexInfo
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
		stored:  stored,
		indexes: indexes,
	}, nil
}

// makeSelectPlan returns the plan of reading the table on its own: through an
// index where the predicate equates an indexed field with a constant, and
// under a select of the terms that can be tested against its records alone.
func (tp *tablePlanner) makeSelectPlan() Plan {
	var p Plan = tp.stored
	if indexSelect, ok := cheapestIndexSelect(tp.stored, tp.indexes, tp.pred); ok {
		p = indexSelect
	}

	return tp.addSelectPred(p)
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
	selectPred, ok := tp.pred.SelectSubPred(tp.stored.Schema())
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
