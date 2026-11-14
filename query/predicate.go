package query

import (
	"slices"
	"strings"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Predicate is the condition that a query filters records by: a list of terms
// that must all hold.
//
// The terms are combined only with "and". This does not limit what a query
// can express, because any condition can be written as a disjunction of such
// predicates. It does let a planner split a predicate. A term of a
// conjunction can be moved to the input that has its fields, and the
// remaining terms keep their meaning. A predicate with an "or" could not be
// split this way.
type Predicate struct {
	terms []Term
}

// NewPredicate returns the predicate that holds when all the given terms
// hold.
//
// With no terms, the predicate holds for every record. A query without a
// where clause uses this. It is an ordinary predicate and not a missing one,
// so no later code needs a special case for an unfiltered query.
//
// The terms are copied. A caller that later writes to the slice it passed
// does not change the predicate.
func NewPredicate(terms ...Term) Predicate {
	return Predicate{
		terms: slices.Clone(terms),
	}
}

// IsSatisfied reports whether the current record of the scan satisfies every
// term.
//
// Evaluation stops at the first term that does not hold. The later terms
// cannot change the result, and each skipped term saves a field read. The
// cost of a predicate is therefore the cost of rejecting a record, not the
// cost of checking every term.
//
// A predicate with no terms is satisfied without any read of the scan.
func (p Predicate) IsSatisfied(s Scan) (bool, error) {
	for _, term := range p.terms {
		satisfied, err := term.IsSatisfied(s)
		if err != nil {
			return false, err
		}
		if !satisfied {
			return false, nil
		}
	}

	return true, nil
}

// ConjoinWith returns the predicate that holds when both this predicate and
// other hold.
//
// It creates a new predicate and does not modify this one. A planner derives
// several predicates from one while it decides which conditions to push down
// to which input. If ConjoinWith appended to the terms of its receiver, it
// would change the predicate that the others were derived from.
func (p Predicate) ConjoinWith(other Predicate) Predicate {
	return Predicate{
		terms: slices.Concat(p.terms, other.terms),
	}
}

// Terms returns the terms of the predicate in their original order.
//
// A planner uses it to split a predicate. The slice is a copy, so a caller
// that writes to it does not change the predicate.
func (p Predicate) Terms() []Term {
	return slices.Clone(p.terms)
}

// SelectSubPred returns the predicate of the terms that can be evaluated
// against a record of schema alone, in their original order. It also reports
// whether there are any such terms.
//
// A planner can test these terms while a table is read, before any join, so
// that the join receives fewer records. This is safe because the terms are
// combined with "and": a record that one term rejects is rejected by the
// whole predicate, wherever in the plan the term is tested.
//
// "No such terms" is reported separately from a predicate with no terms. A
// predicate with no terms holds for every record. A planner that received it
// would add a select that keeps every record, instead of adding no select.
func (p Predicate) SelectSubPred(schema *recordmanager.Schema) (Predicate, bool) {
	return p.filter(func(term Term) bool {
		return term.AppliesTo(schema)
	})
}

// JoinSubPred returns the predicate of the terms that can be evaluated
// against a record of the two schemas joined, but not against a record of
// either schema alone. The terms keep their original order. It also reports
// whether there are any such terms.
//
// A planner tests these terms where it joins a table of one schema to a plan
// of the other. They cannot be tested earlier, and testing them there stops
// the join from passing on pairs that they reject. A term that one schema can
// evaluate alone is excluded, because SelectSubPred has already placed it
// below the join.
//
// The two schemas are not merged into one for the check. They can share a
// field name, which a schema rejects. Each side of a term is therefore
// checked against one schema and then the other.
func (p Predicate) JoinSubPred(schema1, schema2 *recordmanager.Schema) (Predicate, bool) {
	appliesToEither := func(e Expression) bool {
		return e.AppliesTo(schema1) || e.AppliesTo(schema2)
	}

	return p.filter(func(term Term) bool {
		if term.AppliesTo(schema1) || term.AppliesTo(schema2) {
			return false
		}

		return appliesToEither(term.lhs) && appliesToEither(term.rhs)
	})
}

// filter returns the predicate of the terms for which keep returns true, in
// their original order. It also reports whether there are any such terms.
func (p Predicate) filter(keep func(Term) bool) (Predicate, bool) {
	var terms []Term
	for _, term := range p.terms {
		if keep(term) {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return Predicate{}, false
	}

	return Predicate{terms: terms}, true
}

// String returns the terms in their original order, separated by "and".
//
// A predicate with no terms returns the empty string. It represents a where
// clause that was not written, and code that prints a query omits "where" for
// it. Any other output here would be SQL that nobody wrote.
func (p Predicate) String() string {
	written := make([]string, 0, len(p.terms))
	for _, term := range p.terms {
		written = append(written, term.String())
	}

	return strings.Join(written, " and ")
}
