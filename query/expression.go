package query

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Expression is one side of a predicate term. It is either a field name,
// which evaluates to the value of that field in the current record, or a
// constant, which evaluates to itself.
//
// Both kinds answer the same question with the same signature: the value of
// the expression for the current record of a scan. The interface therefore
// carries real behavior and does not replace a type switch. This is why
// Expression is an interface, while Constant is a struct: the two types of a
// constant are read by methods with different return types.
type Expression interface {
	// Evaluate returns the value of the expression for the current record of
	// s.
	Evaluate(s Scan) (Constant, error)
	// AppliesTo reports whether the expression can be evaluated against a
	// record of schema.
	//
	// A planner classifies the terms of a predicate with it. If both sides of
	// a term apply to the schema of one table, the term can be tested while
	// that table is read, before any join.
	AppliesTo(schema *recordmanager.Schema) bool
	fmt.Stringer
}

// These variables are never read. They make a type that no longer satisfies
// the interface a compile error in this file, and not at the first term that
// uses it.
var (
	_ Expression = FieldExpression{}
	_ Expression = ConstantExpression{}
)

// FieldExpression is a field name. It evaluates to the value of that field in
// the current record.
//
// It holds no value itself. The same expression evaluates to a different
// value for each record. A predicate can therefore keep some records and drop
// others, instead of deciding once for the whole table.
type FieldExpression struct {
	fieldName string
}

// NewFieldExpression returns the expression for the field named fieldName.
//
// The name is not checked against a schema here. An expression is created
// before the planner decides which table of the query it is evaluated
// against. A missing field is reported when the expression is evaluated.
func NewFieldExpression(fieldName string) FieldExpression {
	return FieldExpression{
		fieldName: fieldName,
	}
}

// Evaluate returns the value of the field in the current record of s.
//
// The scan determines the type of the constant from the schema, so this
// method does not decide whether the value is an int or a varchar.
func (e FieldExpression) Evaluate(s Scan) (Constant, error) {
	return s.GetValue(e.fieldName)
}

// AppliesTo reports whether schema has the field of the expression.
func (e FieldExpression) AppliesTo(schema *recordmanager.Schema) bool {
	return schema.HasField(e.fieldName)
}

// FieldName returns the name of the field of the expression.
func (e FieldExpression) FieldName() string {
	return e.fieldName
}

// String returns the field name without quotes, so that a printed term shows
// a column name and not a string constant with the same spelling.
func (e FieldExpression) String() string {
	return e.fieldName
}

// ConstantExpression is a value written in the query itself: the 10 in
// "where grade = 10", or the 'math' in "where dept = 'math'".
type ConstantExpression struct {
	val Constant
}

// NewConstantExpression returns the expression for val.
func NewConstantExpression(val Constant) ConstantExpression {
	return ConstantExpression{
		val: val,
	}
}

// Evaluate returns the constant, whatever the current record of the scan is.
//
// The scan parameter is not used. This is the only difference between the two
// kinds of expression. A literal has the same value for every record. If it
// read the scan, a term of two literals would depend on the existence of a
// record.
func (e ConstantExpression) Evaluate(_ Scan) (Constant, error) {
	return e.val, nil
}

// AppliesTo always reports true. A constant can be evaluated against a record
// of any schema, including an empty one, because its evaluation reads nothing
// from the record.
func (e ConstantExpression) AppliesTo(_ *recordmanager.Schema) bool {
	return true
}

// Value returns the constant of the expression.
func (e ConstantExpression) Value() Constant {
	return e.val
}

// String returns the constant's own text, in which a varchar is quoted.
//
// A field and a literal can have the same spelling, so the quotes are the
// only difference between them in a printed term.
func (e ConstantExpression) String() string {
	return e.val.String()
}
