package query

import "fmt"

// Expression is one side of a term of a predicate: either the name of a field,
// which stands for whatever the record being looked at holds in it, or a
// constant, which stands for itself.
//
// Both kinds answer the same question in the same shape — what value does this
// come to, for the record this scan is on — so the interface carries the work
// rather than standing in for a type switch. That is what makes this an
// interface where Constant, whose two kinds are read out with methods of
// different return types, is a struct instead.
type Expression interface {
	// Evaluate returns the value the expression comes to for the record the
	// scan is on.
	Evaluate(s Scan) (Constant, error)
	fmt.Stringer
}

// Nothing reads these: they are here so that an implementation drifting away
// from the interface is a compile error in this file rather than at whichever
// term first tried to hold one.
var (
	_ Expression = FieldExpression{}
	_ Expression = ConstantExpression{}
)

// FieldExpression is the name of a field, standing for whatever the record
// being looked at holds in it.
//
// It carries no value of its own. The same expression comes to a different
// value at every record, which is what lets a predicate keep some records and
// drop others rather than deciding once for the whole table.
type FieldExpression struct {
	fieldName string
}

// NewFieldExpression returns the expression standing for the field of this
// name.
//
// The name is not checked against a schema here, because an expression is built
// before it is settled which of a query's tables it will be read against.
// Whether the field is there at all comes out when it is evaluated.
func NewFieldExpression(fieldName string) FieldExpression {
	return FieldExpression{
		fieldName: fieldName,
	}
}

// Evaluate returns what the record the scan is on holds in the field.
//
// Which kind of constant that is comes from the scan, which takes it from the
// schema, so nothing here decides whether the value is a number or text.
func (e FieldExpression) Evaluate(s Scan) (Constant, error) {
	return s.GetValue(e.fieldName)
}

// String is the field name on its own, without quotes, so that a printed term
// reads as the column it names rather than as text that happens to spell it.
func (e FieldExpression) String() string {
	return e.fieldName
}

// ConstantExpression is a value written into the query itself: the 10 of
// "where grade = 10", or the 'math' of "where dept = 'math'".
type ConstantExpression struct {
	val Constant
}

// NewConstantExpression returns the expression standing for val.
func NewConstantExpression(val Constant) ConstantExpression {
	return ConstantExpression{
		val: val,
	}
}

// Evaluate returns the constant, whatever the scan is on.
//
// The scan is taken and not used, which is the whole of the difference between
// the two kinds of expression. A literal is the same value at every record, and
// reaching for the scan to say so would make a term of two literals depend on
// there being a record to read.
func (e ConstantExpression) Evaluate(_ Scan) (Constant, error) {
	return e.val, nil
}

// String is the constant's own text, which leaves a varchar quoted.
//
// A field and a literal can be spelled the same, so in a printed term the
// quoting is the only thing telling the two apart.
func (e ConstantExpression) String() string {
	return e.val.String()
}
