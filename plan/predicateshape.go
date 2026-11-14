package plan

import "github.com/JunNishimura/GoSQL/query"

// The functions here inspect the structure of a predicate: the fields of its
// terms, and what each field is equated with. A plan needs this structure to
// estimate how many records a predicate keeps. The predicate exposes the
// structure and does not estimate itself, because the estimate needs
// statistics that only a plan has.

// fieldNameOf returns the field name of e, and reports whether e is a field
// expression and not a constant.
func fieldNameOf(e query.Expression) (string, bool) {
	field, ok := e.(query.FieldExpression)
	if !ok {
		return "", false
	}

	return field.FieldName(), true
}

// isField reports whether e is the field expression for fieldName.
func isField(e query.Expression, fieldName string) bool {
	name, ok := fieldNameOf(e)
	return ok && name == fieldName
}

// equatesWithConstant reports whether a term of pred equates fieldName with a
// constant, on either side.
func equatesWithConstant(pred query.Predicate, fieldName string) bool {
	_, ok := constantEquatedWith(pred, fieldName)

	return ok
}

// constantEquatedWith returns the constant that a term of pred equates
// fieldName with, on either side, and reports whether such a term exists.
//
// An index on fieldName is searched for this constant. A record that
// satisfies the predicate has the constant in fieldName, so the records that
// the index holds for it are the only records that can satisfy the predicate.
// If several terms equate the field with a constant, the first is used. A
// record that satisfies the predicate satisfies every term, so a search for
// any of the constants finds it.
func constantEquatedWith(pred query.Predicate, fieldName string) (query.Constant, bool) {
	for _, term := range pred.Terms() {
		if c, ok := term.RHS().(query.ConstantExpression); ok && isField(term.LHS(), fieldName) {
			return c.Value(), true
		}
		if c, ok := term.LHS().(query.ConstantExpression); ok && isField(term.RHS(), fieldName) {
			return c.Value(), true
		}
	}

	return query.Constant{}, false
}

// equatesWithField returns the other field that a term of pred equates
// fieldName with, on either side, and reports whether such a term exists.
func equatesWithField(pred query.Predicate, fieldName string) (string, bool) {
	for _, term := range pred.Terms() {
		lhsName, lhsIsField := fieldNameOf(term.LHS())
		rhsName, rhsIsField := fieldNameOf(term.RHS())
		if !lhsIsField || !rhsIsField {
			continue
		}

		if lhsName == fieldName {
			return rhsName, true
		}
		if rhsName == fieldName {
			return lhsName, true
		}
	}

	return "", false
}
