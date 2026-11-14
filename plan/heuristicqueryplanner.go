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

// HeuristicQueryPlanner turns a select statement into a plan by choosing the
// order its tables are joined in, one table at a time.
//
// It does not weigh every order there is, which grows with the factorial of
// the number of tables. It goes by rules of thumb instead, each aimed at
// keeping what is handed from one join to the next small:
//
//   - each table is read under the terms of the where clause it answers on
//     its own, through an index where one of them lets it be;
//   - the table that gives back the fewest records goes first;
//   - a table the where clause joins to those already planned is added before
//     one it does not, since a join keeps only the pairs that agree and a
//     product keeps them all;
//   - of the tables that can be added, the one whose plan gives back the
//     fewest records is.
//
// Where two tables would give back as many records, the one named first in
// the from clause is taken, so that the plan of a query does not change from
// one run to the next.
type HeuristicQueryPlanner struct {
	mm *metadatamanager.MetadataManager
}

var _ QueryPlanner = (*HeuristicQueryPlanner)(nil)

// NewHeuristicQueryPlanner returns a planner that reads tables, statistics and
// indexes through mm.
func NewHeuristicQueryPlanner(mm *metadatamanager.MetadataManager) *HeuristicQueryPlanner {
	return &HeuristicQueryPlanner{
		mm: mm,
	}
}

// CreatePlan returns the plan of the query data: its tables joined in the
// order the rules of thumb pick, and a projection onto the columns asked for.
//
// There is no select over the whole of it. Every term of the where clause is
// tested where its fields are first all in hand, which is either where one
// table is read or where a second is joined to it, so none is left over to
// test at the end.
//
// A term naming a field none of the tables has is refused with
// recordmanager.ErrFieldNotFound. It could be tested nowhere, and planning
// the rest would quietly keep the records it was written to drop.
//
// A name in the from clause that is a view is planned from its definition by
// this planner, and the plan that gives is then taken as one more table, with
// the fields the definition selects. A view defined over another view is
// expanded in turn by the same call.
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

// newPlanner returns the planner of one name of a from clause under the query
// predicate pred: of a view, planned from its definition, or of a stored
// table.
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

// checkFieldsKnown reports recordmanager.ErrFieldNotFound for the first term
// of pred with a side that none of the tables planners plan can evaluate.
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

// lowest returns, of the plans makePlan makes of planners, the one that gives
// back the fewest records, the planners left once the one it was made of is
// taken out, and whether makePlan made any plan at all.
//
// makePlan reports false for a planner it has no plan of, which is passed
// over. Of two plans that give back as many records, the one of the planner
// that comes first is taken, and planners keep the order of the from clause,
// so that is the table named first.
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
