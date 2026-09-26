package parse

import (
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
)

// newTestInsertData parses s as an insert, failing the test if it does not
// parse as one.
func newTestInsertData(t *testing.T, s string) InsertData {
	t.Helper()

	cmd, err := newTestParser(t, s).UpdateCmd()
	if err != nil {
		t.Fatalf("UpdateCmd() of %q error = %v", s, err)
	}
	id, ok := cmd.(InsertData)
	if !ok {
		t.Fatalf("UpdateCmd() of %q = %T, want InsertData", s, cmd)
	}

	return id
}

func TestInsertDataFields(t *testing.T) {
	t.Run("given insert data of a parsed insert, when the fields it returned are written to, then the fields it returns afterwards are unchanged", func(t *testing.T) {
		id := newTestInsertData(t, "insert into student (sid, sname) values (1, 'Joe')")

		id.Fields()[0] = "grade"

		if got, want := id.Fields(), []string{"sid", "sname"}; !slices.Equal(got, want) {
			t.Errorf("Fields() = %v, want %v", got, want)
		}
	})
}

func TestInsertDataValues(t *testing.T) {
	t.Run("given insert data of a parsed insert, when the values it returned are written to, then the values it returns afterwards are unchanged", func(t *testing.T) {
		id := newTestInsertData(t, "insert into student (sid, sname) values (1, 'Joe')")

		id.Values()[0] = query.NewIntConstant(2)

		want := []query.Constant{
			query.NewIntConstant(1),
			query.NewStringConstant("Joe"),
		}
		if got := id.Values(); !slices.Equal(got, want) {
			t.Errorf("Values() = %v, want %v", got, want)
		}
	})
}
