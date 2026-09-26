package parse

import (
	"reflect"
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

// A view is stored as the text of its query and parsed again whenever it is
// used, so what String writes has to be SQL this package reads back.
func TestQueryDataString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "given query data of a query with no where clause, then it is the query with no where",
			input: "select sname from student",
			want:  "select sname from student",
		},
		// A string constant is written in single quotes, the only quotes the
		// tokenizer reads a string constant from.
		{
			name:  "given query data of a query of several fields and tables with a where clause, then it is the query with its string constants in single quotes",
			input: "select sname, sid from student, enroll where sid = studentid and major = 'Math'",
			want:  "select sname, sid from student, enroll where sid = studentid and major = 'Math'",
		},
		{
			name:  "given query data of a query written in upper case and without spaces after its commas, then it is the query in lower case with a space after each comma",
			input: "SELECT SName,SId FROM Student WHERE SId = 3",
			want:  "select sname, sid from student where sid = 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qd := newTestQueryData(t, tt.input)

			if got := qd.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("given query data of a query with a string constant, when its string is parsed again, then it is the same query data", func(t *testing.T) {
		qd := newTestQueryData(t, "select sname from student where major = 'Math' and sid = 3")

		reparsed := newTestQueryData(t, qd.String())

		if !reflect.DeepEqual(reparsed, qd) {
			t.Errorf("query data of %q = %+v, want %+v", qd.String(), reparsed, qd)
		}
	})
}
