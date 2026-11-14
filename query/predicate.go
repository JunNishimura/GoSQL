package query

import (
	"slices"
	"strings"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Predicate is the condition a query keeps records by: terms that all have to
// hold.
//
// Only "and" joins them. That is not a limit on what can be asked, since any
// condition can be written as a disjunction of these, but it is what lets a
// planner take a predicate apart: a term of a conjunction can be moved to
// whichever input holds the fields it names, and the ones left behind still
// mean what they did. A predicate holding an "or" could not be split that way.
type Predicate struct {
	terms []Term
}

// NewPredicate returns the predicate that holds where all of the given terms
// do.
//
// Given no terms it holds everywhere, which is what a query with no where
// clause asks for. That is a predicate like any other rather than an absence,
// so nothing downstream has to treat the unfiltered case apart.
//
// The terms are copied, so a caller that passes a slice it goes on to write to
// does not change the predicate under it.
func NewPredicate(terms ...Term) Predicate {
	return Predicate{
		terms: slices.Clone(terms),
	}
}

// IsSatisfied reports whether the record the scan is on meets every term.
//
// Reading stops at the first term that does not hold. The terms after it cannot
// change the answer, and each one skipped is a field that is not read, so a
// predicate costs what it takes to rule a record out rather than what it takes
// to check all of it.
//
// A predicate of no terms is satisfied without the scan being read at all.
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

// ConjoinWith returns the predicate holding where both this one and other do.
//
// It builds another predicate rather than growing this one. A planner makes
// several predicates out of one while working out which conditions can be
// pushed down to which input, and a conjunction that appended into the terms it
// was called on would change the predicate the others were taken from.
func (p Predicate) ConjoinWith(other Predicate) Predicate {
	return Predicate{
		terms: slices.Concat(p.terms, other.terms),
	}
}

// Terms returns the terms of the predicate, in the order they were given.
//
// This is how a planner takes a predicate apart. The slice is a copy, so a
// caller that writes to it does not change the predicate it came from.
func (p Predicate) Terms() []Term {
	return slices.Clone(p.terms)
}

// SelectSubPred returns the predicate of the terms that can be evaluated
// against a record of schema alone, in the order they were given, and whether
// there are any.
//
// These are the terms a planner can test as a table is read, before it is
// joined to anything, so that the join is given fewer records. That is safe
// because the terms are joined by "and": a record one of them rules out is
// ruled out of the whole predicate, wherever in the plan it is tested.
//
// Having no such terms is reported apart from a predicate of no terms. That
// one holds everywhere, and a planner given it would put a select over the
// table that keeps every record, rather than putting none.
func (p Predicate) SelectSubPred(schema *recordmanager.Schema) (Predicate, bool) {
	var terms []Term
	for _, term := range p.terms {
		if term.AppliesTo(schema) {
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return Predicate{}, false
	}

	return Predicate{terms: terms}, true
}

// String writes the terms in the order they were given, with "and" between
// them.
//
// A predicate of no terms is the empty string. It stands for a where clause
// that was never written, and whoever prints a query writes no "where" for it
// at all, so there is nothing to put here that would not be SQL nobody asked
// for.
func (p Predicate) String() string {
	written := make([]string, 0, len(p.terms))
	for _, term := range p.terms {
		written = append(written, term.String())
	}

	return strings.Join(written, " and ")
}
