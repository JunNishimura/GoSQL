package parse

import "github.com/JunNishimura/GoSQL/query"

// QueryData is what a select statement asks for: the fields to return, the
// tables to take them from, and the condition a record has to meet.
type QueryData struct {
	fields []string
	tables []string
	pred   query.Predicate
}

// Fields are the fields the query returns, in the order written, which is the
// order of the columns of its result.
func (q QueryData) Fields() []string {
	return q.fields
}

// Tables are the tables the query reads, in the order written.
func (q QueryData) Tables() []string {
	return q.tables
}

// Predicate is the condition the query keeps records by. A query with no where
// clause has a predicate of no terms, which every record meets.
func (q QueryData) Predicate() query.Predicate {
	return q.pred
}
