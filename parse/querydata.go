package parse

import (
	"slices"
	"strings"

	"github.com/JunNishimura/GoSQL/query"
)

// QueryData is what a select statement asks for: the fields to return, the
// tables to take them from, and the condition a record has to meet.
//
// The fields and tables are handed out as copies, as query.NewPredicate copies
// the terms it is given, so that a caller writing to what it was returned does
// not change the query under whoever else holds it.
type QueryData struct {
	fields []string
	tables []string
	pred   query.Predicate
}

// Fields are the fields the query returns, in the order written, which is the
// order of the columns of its result.
func (q QueryData) Fields() []string {
	return slices.Clone(q.fields)
}

// Tables are the tables the query reads, in the order written.
func (q QueryData) Tables() []string {
	return slices.Clone(q.tables)
}

// Predicate is the condition the query keeps records by. A query with no where
// clause has a predicate of no terms, which every record meets.
func (q QueryData) Predicate() query.Predicate {
	return q.pred
}

// String writes the query back out as SQL that parses to the same query data.
//
// A view is stored as this text and parsed again whenever it is used, so being
// read back is what it is for. It is written in the one form the parser leaves
// a query in, in lower case with a space after each comma, rather than as it
// was first typed.
func (q QueryData) String() string {
	s := "select " + strings.Join(q.fields, ", ") + " from " + strings.Join(q.tables, ", ")

	// A predicate of no terms writes nothing, and stands for a where clause
	// that was never written.
	if pred := q.pred.String(); pred != "" {
		s += " where " + pred
	}

	return s
}
