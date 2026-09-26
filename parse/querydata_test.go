package parse

import (
	"slices"
	"testing"
)

// newTestQueryData parses s as a query, failing the test if it does not parse.
func newTestQueryData(t *testing.T, s string) QueryData {
	t.Helper()

	qd, err := newTestParser(t, s).Query()
	if err != nil {
		t.Fatalf("Query() of %q error = %v", s, err)
	}

	return qd
}

func TestQueryDataFields(t *testing.T) {
	t.Run("given query data of a parsed query, when the fields it returned are written to, then the fields it returns afterwards are unchanged", func(t *testing.T) {
		qd := newTestQueryData(t, "select sname, sid from student")

		qd.Fields()[0] = "grade"

		if got, want := qd.Fields(), []string{"sname", "sid"}; !slices.Equal(got, want) {
			t.Errorf("Fields() = %v, want %v", got, want)
		}
	})
}

func TestQueryDataTables(t *testing.T) {
	t.Run("given query data of a parsed query, when the tables it returned are written to, then the tables it returns afterwards are unchanged", func(t *testing.T) {
		qd := newTestQueryData(t, "select sname from student, enroll")

		qd.Tables()[0] = "dept"

		if got, want := qd.Tables(), []string{"student", "enroll"}; !slices.Equal(got, want) {
			t.Errorf("Tables() = %v, want %v", got, want)
		}
	})
}
