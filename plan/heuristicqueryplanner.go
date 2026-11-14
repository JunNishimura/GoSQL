package plan

import (
	"fmt"
	"slices"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// HeuristicQueryPlanner creates a plan for a select statement by choosing the
// join order of its tables, one table at a time.
//
// It does not compare every possible order, because their number grows with
// the factorial of the number of tables. It uses heuristics instead. Each one
// aims to keep the intermediate results between joins small:
//
//   - Each table is read with the terms of the where clause that apply to it
//     alone, and through an index if one of those terms allows it.
//   - The table that returns the fewest records comes first.
//   - A table that the where clause joins to the tables already planned is
//     added before a table that it does not. A join keeps only the matching
//     pairs, and a product keeps all pairs.
//   - Among the tables that can be added, the one whose plan returns the
//     fewest records is added.
//
// If two tables return the same number of records, the one that comes first
// in the from clause is chosen. The plan of a query is then the same on every
// run.
type HeuristicQueryPlanner struct {
	mm *metadatamanager.MetadataManager
}

var _ QueryPlanner = (*HeuristicQueryPlanner)(nil)

// NewHeuristicQueryPlanner returns a planner that reads tables, statistics
// and indexes through mm.
func NewHeuristicQueryPlanner(mm *metadatamanager.MetadataManager) *HeuristicQueryPlanner {
	return &HeuristicQueryPlanner{
		mm: mm,
	}
}

// CreatePlan returns the plan for the query data: its tables joined in the
// order that the heuristics choose, and a projection onto the requested
// columns.
//
// The plan has no select over the whole result. Every term of the where
// clause is tested at the first point where all its fields are available.
// That point is either the read of one table or the join of a second table
// to it, so no term remains to be tested at the end.
//
// A term with a field that none of the tables has is rejected with
// recordmanager.ErrFieldNotFound. It could not be tested anywhere, and
// planning the rest of the query would silently keep the records that the
// term was meant to drop.
//
// A name in the from clause that is a view is planned from its definition by
// this planner. The resulting plan is then treated as one more table, with
// the fields that the definition selects. A view that is defined on another
// view is expanded recursively by the same call.
func (qp *HeuristicQueryPlanner) CreatePlan(data parse.QueryData, tx *transaction.Transaction) (Plan, error) {
	planners := make([]*tablePlanner, 0, len(data.Tables()))
	for _, tableName := range data.Tables() {
		tp, err := qp.newPlanner(tableName, data.Predicate(), tx)
		if err != nil {
			return nil, err
		}
		planners = append(planners, tp)
	}

	if err := checkFieldsKnown(data.Predicate(), planners); err != nil {
		return nil, err
	}

	current, planners, _, err := lowest(planners, func(tp *tablePlanner) (Plan, bool, error) {
		return tp.makeSelectPlan(), true, nil
	})
	if err != nil {
		return nil, err
	}

	for len(planners) > 0 {
		next, rest, ok, err := lowest(planners, func(tp *tablePlanner) (Plan, bool, error) {
			return tp.makeJoinPlan(current)
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			next, rest, _, err = lowest(planners, func(tp *tablePlanner) (Plan, bool, error) {
				p, err := tp.makeProductPlan(current)
				return p, true, err
			})
			if err != nil {
				return nil, err
			}
		}
		current, planners = next, rest
	}

	return NewProjectPlan(current, data.Fields()...)
}

// newPlanner returns the table planner for one name of a from clause, under
// the query predicate pred. The name is either a view, which is planned from
// its definition, or a stored table.
func (qp *HeuristicQueryPlanner) newPlanner(tableName string, pred query.Predicate, tx *transaction.Transaction) (*tablePlanner, error) {
	definition, isView, err := qp.mm.GetViewDefinition(tx, tableName)
	if err != nil {
		return nil, err
	}
	if !isView {
		return newTablePlanner(tx, tableName, pred, qp.mm)
	}

	viewData, err := parseViewDefinition(tableName, definition)
	if err != nil {
		return nil, err
	}

	p, err := qp.CreatePlan(viewData, tx)
	if err != nil {
		return nil, err
	}

	return newViewPlanner(tx, p, pred), nil
}

// checkFieldsKnown returns recordmanager.ErrFieldNotFound for the first term
// of pred that has a side which none of the tables in planners can evaluate.
func checkFieldsKnown(pred query.Predicate, planners []*tablePlanner) error {
	known := func(e query.Expression) bool {
		return slices.ContainsFunc(planners, func(tp *tablePlanner) bool {
			return e.AppliesTo(tp.schema())
		})
	}

	for _, term := range pred.Terms() {
		if !known(term.LHS()) || !known(term.RHS()) {
			return fmt.Errorf("plan the term %s: %w", term, recordmanager.ErrFieldNotFound)
		}
	}

	return nil
}

// lowest calls makePlan for each planner and returns the plan that returns
// the fewest records. It also returns the planners without the one that
// produced that plan, and reports whether makePlan produced any plan.
//
// makePlan returns false for a planner that has no such plan, and lowest
// skips it. If two plans return the same number of records, the plan of the
// earlier planner is chosen. The planners are in the order of the from
// clause, so that is the table that is written first.
func lowest(planners []*tablePlanner, makePlan func(*tablePlanner) (Plan, bool, error)) (Plan, []*tablePlanner, bool, error) {
	var (
		best      Plan
		bestIndex int
	)
	for i, tp := range planners {
		candidate, ok, err := makePlan(tp)
		if err != nil {
			return nil, nil, false, err
		}
		if !ok {
			continue
		}
		if best == nil || candidate.RecordsOutput() < best.RecordsOutput() {
			best, bestIndex = candidate, i
		}
	}

	if best == nil {
		return nil, planners, false, nil
	}

	return best, slices.Delete(planners, bestIndex, bestIndex+1), true, nil
}
