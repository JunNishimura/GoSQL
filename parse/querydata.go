package parse

import (
	"slices"

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
