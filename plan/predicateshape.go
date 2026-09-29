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

// isConstant reports whether e is a constant.
func isConstant(e query.Expression) bool {
	_, ok := e.(query.ConstantExpression)
	return ok
}

// equatesWithConstant reports whether a term of pred equates fieldName with a
// constant, on either side.
func equatesWithConstant(pred query.Predicate, fieldName string) bool {
	for _, term := range pred.Terms() {
		if isField(term.LHS(), fieldName) && isConstant(term.RHS()) {
			return true
		}
		if isConstant(term.LHS()) && isField(term.RHS(), fieldName) {
			return true
		}
	}

	return false
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
