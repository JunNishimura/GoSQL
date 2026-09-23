package query

import "fmt"

// Term is one comparison of a predicate: two expressions that a record has to
// bring to the same value.
//
// Both sides are expressions rather than a field on one side and a value on the
// other, which is what lets one kind of term do two jobs. A field against a
// constant picks records out of a table; a field against another field is the
// condition two tables are joined on. A predicate holding either does not have
// to know which it is holding.
//
// Equality is the only comparison here. It is what the operators of a query are
// built out of, and ordering is not: less-than earns its place with the range
// searches an index serves, which is a later part of the database than this
// one.
type Term struct {
	lhs Expression
	rhs Expression
}

// NewTerm returns the term that holds when the two expressions come to the same
// value.
func NewTerm(lhs, rhs Expression) Term {
	return Term{
		lhs: lhs,
		rhs: rhs,
	}
}

// IsSatisfied reports whether the record the scan is on brings the two sides to
// the same value.
//
// The comparison is ==, so two constants of different kinds are never equal
// however they are written. A term over an int field and a varchar one keeps no
// records at all, rather than keeping those where the text spells the number,
// and that is the answer wanted: a query comparing values of two types is one
// that matches nothing.
func (t Term) IsSatisfied(s Scan) (bool, error) {
	lhsVal, err := t.lhs.Evaluate(s)
	if err != nil {
		return false, err
	}

	rhsVal, err := t.rhs.Evaluate(s)
	if err != nil {
		return false, err
	}

	return lhsVal == rhsVal, nil
}

// String writes the two sides with an equals sign between them.
//
// The sign is written in rather than carried, because equality is the only
// comparison a term can be. Each side prints itself, which is what keeps a
// varchar constant quoted and so tells it apart from a field of the same
// spelling.
func (t Term) String() string {
	return fmt.Sprintf("%s = %s", t.lhs, t.rhs)
}
