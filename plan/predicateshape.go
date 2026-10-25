package plan

import "github.com/JunNishimura/GoSQL/query"

// The functions here read the shape of a predicate: which fields its terms
// name, and what each is equated with. A plan needs that shape to guess what a
// predicate keeps, and the predicate hands it out rather than guessing for
// itself, since the guess needs statistics only a plan holds.

// fieldNameOf returns the name of the field e stands for, and whether e
// stands for a field at all rather than a constant.
func fieldNameOf(e query.Expression) (string, bool) {
	field, ok := e.(query.FieldExpression)
	if !ok {
		return "", false
	}

	return field.FieldName(), true
}

// isField reports whether e stands for the field named fieldName.
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

// constantEquatedWith returns the constant a term of pred equates fieldName
// with, on either side, and whether there is such a term.
//
// It is what an index on fieldName is searched for: a record the predicate
// keeps holds that constant in fieldName, so the records filed under it are
// the only ones the predicate can keep. If more than one term equates the
// field with a constant, the first is taken; a record the predicate keeps
// satisfies every term, so searching for any one of them finds it.
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

// equatesWithField returns the other field a term of pred equates fieldName
// with, on either side.
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
