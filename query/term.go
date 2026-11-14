package query

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Term is one comparison in a predicate: two expressions that must evaluate
// to the same value for a record.
//
// Both sides are expressions. A term is not limited to a field on one side
// and a value on the other, so one type serves two purposes. A field compared
// with a constant selects records from a table. A field compared with another
// field is a join condition between two tables. A predicate handles both in
// the same way.
//
// Equality is the only comparison. The query operators here are built on
// equality and not on ordering. Less-than becomes useful with the range
// searches of an index, which belong to a later part of the database.
type Term struct {
	lhs Expression
	rhs Expression
}

// NewTerm returns the term that holds when the two expressions evaluate to
// the same value.
func NewTerm(lhs, rhs Expression) Term {
	return Term{
		lhs: lhs,
		rhs: rhs,
	}
}

// LHS returns the expression on the left of the equals sign.
func (t Term) LHS() Expression {
	return t.lhs
}

// RHS returns the expression on the right of the equals sign.
func (t Term) RHS() Expression {
	return t.rhs
}

// IsSatisfied reports whether the two sides evaluate to the same value for
// the current record of the scan.
//
// The comparison is ==, so two constants of different types are never equal,
// whatever their contents. A term that compares an int field with a varchar
// field keeps no records. It does not keep the records in which the text
// spells the number. This is the intended result: a query that compares
// values of two types matches nothing.
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

// AppliesTo reports whether both sides of the term can be evaluated against a
// record of schema.
//
// One side is not enough. A term that compares a field of this table with a
// field of another table cannot be tested until the two tables are joined.
func (t Term) AppliesTo(schema *recordmanager.Schema) bool {
	return t.lhs.AppliesTo(schema) && t.rhs.AppliesTo(schema)
}

// String returns the two sides separated by an equals sign.
//
// The sign is a literal and not a stored value, because equality is the only
// comparison of a term. Each side formats itself. A varchar constant
// therefore stays quoted, which distinguishes it from a field with the same
// spelling.
func (t Term) String() string {
	return fmt.Sprintf("%s = %s", t.lhs, t.rhs)
}
