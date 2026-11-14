package parse

import (
	"slices"
	"strings"

	"github.com/JunNishimura/GoSQL/query"
)

// QueryData is the content of a select statement: the fields to return, the
// tables to read, and the condition that a record must satisfy.
//
// The fields and tables are returned as copies, in the same way that
// query.NewPredicate copies its terms. A caller that writes to a returned
// slice then does not change the query for its other holders.
type QueryData struct {
	fields []string
	tables []string
	pred   query.Predicate
}

// Fields returns the fields of the query result, in the order written. This
// is the order of the result columns.
func (q QueryData) Fields() []string {
	return slices.Clone(q.fields)
}

// Tables returns the tables that the query reads, in the order written.
func (q QueryData) Tables() []string {
	return slices.Clone(q.tables)
}

// Predicate returns the condition that the query filters records by. A query
// with no where clause has a predicate with no terms, which every record
// satisfies.
func (q QueryData) Predicate() query.Predicate {
	return q.pred
}

// String returns the query as SQL that parses to the same query data.
//
// A view is stored as this text and parsed again each time it is used, so the
// text must be parseable. It is written in the normalized form of the parser:
// lower case, with a space after each comma. It is not the text as originally
// typed.
func (q QueryData) String() string {
	s := "select " + strings.Join(q.fields, ", ") + " from " + strings.Join(q.tables, ", ")

	// A predicate with no terms returns the empty string. It represents a
	// where clause that was not written.
	if pred := q.pred.String(); pred != "" {
		s += " where " + pred
	}

	return s
}
