package parse

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// newTestParser makes a parser over s, failing the test if the first token
// cannot be read.
func newTestParser(t *testing.T, s string) *Parser {
	t.Helper()

	p, err := NewParser(s)
	if err != nil {
		t.Fatalf("NewParser(%q) error = %v", s, err)
	}

	return p
}

func TestParserField(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "given a parser at a word that is not a keyword, then it returns the word as a field name",
			input: "sname",
			want:  "sname",
		},
		{
			name:    "given a parser at a keyword, then it reports ErrBadSyntax",
			input:   "from",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.field()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("field() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("field() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("field() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParserConstant(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    query.Constant
		wantErr error
	}{
		{
			name:  "given a parser at an int, then it returns an int constant",
			input: "42",
			want:  query.NewIntConstant(42),
		},
		{
			name:  "given a parser at a string constant, then it returns a varchar constant",
			input: "'Math'",
			want:  query.NewStringConstant("Math"),
		},
		// The text is digits, so a parser that turned every constant into a
		// number where it could would come back with the int 42.
		{
			name:  "given a parser at a string constant whose text is digits, then it returns a varchar constant",
			input: "'42'",
			want:  query.NewStringConstant("42"),
		},
		{
			name:    "given a parser at a word, then it reports ErrBadSyntax",
			input:   "sname",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.constant()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("constant() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("constant() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("constant() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParserExpression(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    query.Expression
		wantErr error
	}{
		{
			name:  "given a parser at a word that is not a keyword, then it returns a field expression",
			input: "sname",
			want:  query.NewFieldExpression("sname"),
		},
		{
			name:  "given a parser at an int, then it returns a constant expression",
			input: "42",
			want:  query.NewConstantExpression(query.NewIntConstant(42)),
		},
		// A field and a string constant can be spelled the same, and only the
		// quotes say which one was written.
		{
			name:  "given a parser at a string constant whose text is a field name, then it returns a constant expression",
			input: "'sname'",
			want:  query.NewConstantExpression(query.NewStringConstant("sname")),
		},
		{
			name:    "given a parser at a keyword, then it reports ErrBadSyntax",
			input:   "where",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.expression()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expression() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("expression() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("expression() = %s (%T), want %s (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestParserTerm(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    query.Term
		wantErr error
	}{
		{
			name:  "given a parser at a field compared with a constant, then it returns the term of the two",
			input: "sid = 3",
			want: query.NewTerm(
				query.NewFieldExpression("sid"),
				query.NewConstantExpression(query.NewIntConstant(3)),
			),
		},
		{
			name:  "given a parser at a field compared with another field, then it returns the term of the two",
			input: "sid = studentid",
			want: query.NewTerm(
				query.NewFieldExpression("sid"),
				query.NewFieldExpression("studentid"),
			),
		},
		{
			name:  "given a parser at a constant compared with a field, then it keeps the constant on the left",
			input: "3 = sid",
			want: query.NewTerm(
				query.NewConstantExpression(query.NewIntConstant(3)),
				query.NewFieldExpression("sid"),
			),
		},
		{
			name:    "given a parser at two expressions with no equals sign between them, then it reports ErrBadSyntax",
			input:   "sid 3",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a parser at an expression and an equals sign with nothing after it, then it reports ErrBadSyntax",
			input:   "sid =",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.term()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("term() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("term() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("term() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParserPredicate(t *testing.T) {
	sidIs3 := query.NewTerm(
		query.NewFieldExpression("sid"),
		query.NewConstantExpression(query.NewIntConstant(3)),
	)
	majorIsMath := query.NewTerm(
		query.NewFieldExpression("major"),
		query.NewConstantExpression(query.NewStringConstant("Math")),
	)
	sidIsStudentID := query.NewTerm(
		query.NewFieldExpression("sid"),
		query.NewFieldExpression("studentid"),
	)

	tests := []struct {
		name    string
		input   string
		want    query.Predicate
		wantErr error
	}{
		{
			name:  "given a parser at one term, then it returns the predicate of that term",
			input: "sid = 3",
			want:  query.NewPredicate(sidIs3),
		},
		{
			name:  "given a parser at two terms joined by and, then it returns the predicate of both",
			input: "sid = 3 and major = 'Math'",
			want:  query.NewPredicate(sidIs3, majorIsMath),
		},
		// The terms after the first are parsed as a predicate of their own
		// and conjoined, so this is where their order could come out wrong.
		{
			name:  "given a parser at three terms joined by and, then it returns the predicate of all three in the order written",
			input: "sid = 3 and major = 'Math' and sid = studentid",
			want:  query.NewPredicate(sidIs3, majorIsMath, sidIsStudentID),
		},
		{
			name:    "given a parser at a term followed by and with nothing after it, then it reports ErrBadSyntax",
			input:   "sid = 3 and",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.predicate()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("predicate() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("predicate() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("predicate() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParserQuery(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantFields []string
		wantTables []string
		wantPred   query.Predicate
	}{
		// A query with no where clause keeps every record, which a predicate
		// of no terms already says, so there is no absence to stand for.
		{
			name:       "given a query of one field from one table, then it has that field and that table and a predicate of no terms",
			input:      "select sname from student",
			wantFields: []string{"sname"},
			wantTables: []string{"student"},
			wantPred:   query.NewPredicate(),
		},
		// The order of the fields is the order of the columns of the result,
		// so it is kept as written.
		{
			name:       "given a query of several fields from several tables with a where clause, then it has them all in the order written",
			input:      "select sname, sid, grade from student, enroll where sid = studentid and grade = 'A'",
			wantFields: []string{"sname", "sid", "grade"},
			wantTables: []string{"student", "enroll"},
			wantPred: query.NewPredicate(
				query.NewTerm(
					query.NewFieldExpression("sid"),
					query.NewFieldExpression("studentid"),
				),
				query.NewTerm(
					query.NewFieldExpression("grade"),
					query.NewConstantExpression(query.NewStringConstant("A")),
				),
			),
		},
		{
			name:       "given a query written in upper case, then its keywords are read and its names are in lower case",
			input:      "SELECT SName FROM Student",
			wantFields: []string{"sname"},
			wantTables: []string{"student"},
			wantPred:   query.NewPredicate(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.Query()
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}

			if !slices.Equal(got.Fields(), tt.wantFields) {
				t.Errorf("Fields() = %v, want %v", got.Fields(), tt.wantFields)
			}
			if !slices.Equal(got.Tables(), tt.wantTables) {
				t.Errorf("Tables() = %v, want %v", got.Tables(), tt.wantTables)
			}
			if !reflect.DeepEqual(got.Predicate(), tt.wantPred) {
				t.Errorf("Predicate() = %s, want %s", got.Predicate(), tt.wantPred)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given a statement that is not a query, then it reports ErrBadSyntax",
			input: "delete from student",
		},
		{
			name:  "given a query with no field, then it reports ErrBadSyntax",
			input: "select from student",
		},
		{
			name:  "given a query whose field list ends in a comma, then it reports ErrBadSyntax",
			input: "select sname, from student",
		},
		{
			name:  "given a query with no from, then it reports ErrBadSyntax",
			input: "select sname student",
		},
		{
			name:  "given a query with no table, then it reports ErrBadSyntax",
			input: "select sname from",
		},
		{
			name:  "given a query whose where has no predicate, then it reports ErrBadSyntax",
			input: "select sname from student where",
		},
		// Stopping at the end of the grammar and ignoring the rest would run
		// "where sid = 3 or sid = 4" as "where sid = 3", and answer a query
		// other than the one that was asked.
		{
			name:  "given a query followed by tokens the grammar has no place for, then it reports ErrBadSyntax",
			input: "select sname from student where sid = 3 or sid = 4",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.Query(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("Query() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}

func TestParserUpdateCmd(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType UpdateCommand
	}{
		{
			name:     "given an insert, then it is insert data",
			input:    "insert into student (sid) values (1)",
			wantType: InsertData{},
		},
		{
			name:     "given a delete, then it is delete data",
			input:    "delete from student",
			wantType: DeleteData{},
		},
		{
			name:     "given an update, then it is modify data",
			input:    "update student set gradyear = 2020",
			wantType: ModifyData{},
		},
		{
			name:     "given a create table, then it is create table data",
			input:    "create table student (sid int)",
			wantType: CreateTableData{},
		},
		{
			name:     "given a create view, then it is create view data",
			input:    "create view mathstudents as select sname from student",
			wantType: CreateViewData{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.UpdateCmd()
			if err != nil {
				t.Fatalf("UpdateCmd() error = %v", err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tt.wantType) {
				t.Errorf("UpdateCmd() = %T, want %T", got, tt.wantType)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given an empty input, then it reports ErrBadSyntax",
			input: "",
		},
		{
			name:  "given a query, then it reports ErrBadSyntax",
			input: "select sname from student",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.UpdateCmd(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("UpdateCmd() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}

func TestParserInsert(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTable  string
		wantFields []string
		wantValues []query.Constant
	}{
		{
			name:       "given an insert of an int and a string, then it is insert data of the table, the fields and the values in the order written",
			input:      "insert into student (sid, sname) values (1, 'Joe')",
			wantTable:  "student",
			wantFields: []string{"sid", "sname"},
			wantValues: []query.Constant{
				query.NewIntConstant(1),
				query.NewStringConstant("Joe"),
			},
		},
		{
			name:       "given an insert written in upper case, then its keywords are read and its names are in lower case",
			input:      "INSERT INTO Student (SId) VALUES (-5)",
			wantTable:  "student",
			wantFields: []string{"sid"},
			wantValues: []query.Constant{
				query.NewIntConstant(-5),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.insert()
			if err != nil {
				t.Fatalf("insert() error = %v", err)
			}

			if got.TableName() != tt.wantTable {
				t.Errorf("TableName() = %q, want %q", got.TableName(), tt.wantTable)
			}
			if !slices.Equal(got.Fields(), tt.wantFields) {
				t.Errorf("Fields() = %v, want %v", got.Fields(), tt.wantFields)
			}
			if !slices.Equal(got.Values(), tt.wantValues) {
				t.Errorf("Values() = %v, want %v", got.Values(), tt.wantValues)
			}
		})
	}

	errTests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{
			name:    "given a statement that is not an insert, then it reports ErrBadSyntax",
			input:   "delete from student",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given an insert with no into, then it reports ErrBadSyntax",
			input:   "insert student (sid) values (1)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given an insert with no parentheses around its fields, then it reports ErrBadSyntax",
			input:   "insert into student sid values (1)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given an insert with no values, then it reports ErrBadSyntax",
			input:   "insert into student (sid) (1)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given an insert whose value list ends in a comma, then it reports ErrBadSyntax",
			input:   "insert into student (sid) values (1,)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given an insert whose value is a field name, then it reports ErrBadSyntax",
			input:   "insert into student (sid) values (sname)",
			wantErr: ErrBadSyntax,
		},
		// Which value goes to which field is settled by position, so a value
		// with no field, or a field with no value, has nowhere to be written.
		{
			name:    "given an insert of more fields than values, then it reports ErrFieldValueCountMismatch",
			input:   "insert into student (sid, sname) values (1)",
			wantErr: ErrFieldValueCountMismatch,
		},
		{
			name:    "given an insert of more values than fields, then it reports ErrFieldValueCountMismatch",
			input:   "insert into student (sid) values (1, 'Joe')",
			wantErr: ErrFieldValueCountMismatch,
		},
		{
			name:    "given an insert followed by tokens the grammar has no place for, then it reports ErrBadSyntax",
			input:   "insert into student (sid) values (1) (2)",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.insert(); !errors.Is(err, tt.wantErr) {
				t.Errorf("insert() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParserDelete(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTable string
		wantPred  query.Predicate
	}{
		// A delete with no where clause removes every record, which a
		// predicate of no terms already says.
		{
			name:      "given a delete with no where clause, then it is delete data of the table and a predicate of no terms",
			input:     "delete from student",
			wantTable: "student",
			wantPred:  query.NewPredicate(),
		},
		{
			name:      "given a delete with a where clause, then it is delete data of the table and the predicate written",
			input:     "delete from student where sid = 3 and major = 'Math'",
			wantTable: "student",
			wantPred: query.NewPredicate(
				query.NewTerm(
					query.NewFieldExpression("sid"),
					query.NewConstantExpression(query.NewIntConstant(3)),
				),
				query.NewTerm(
					query.NewFieldExpression("major"),
					query.NewConstantExpression(query.NewStringConstant("Math")),
				),
			),
		},
		{
			name:      "given a delete written in upper case, then its keywords are read and its names are in lower case",
			input:     "DELETE FROM Student WHERE SId = 3",
			wantTable: "student",
			wantPred: query.NewPredicate(
				query.NewTerm(
					query.NewFieldExpression("sid"),
					query.NewConstantExpression(query.NewIntConstant(3)),
				),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.delete()
			if err != nil {
				t.Fatalf("delete() error = %v", err)
			}

			if got.TableName() != tt.wantTable {
				t.Errorf("TableName() = %q, want %q", got.TableName(), tt.wantTable)
			}
			if !reflect.DeepEqual(got.Predicate(), tt.wantPred) {
				t.Errorf("Predicate() = %s, want %s", got.Predicate(), tt.wantPred)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given a statement that is not a delete, then it reports ErrBadSyntax",
			input: "insert into student (sid) values (1)",
		},
		{
			name:  "given a delete with no from, then it reports ErrBadSyntax",
			input: "delete student",
		},
		{
			name:  "given a delete with no table, then it reports ErrBadSyntax",
			input: "delete from where sid = 3",
		},
		{
			name:  "given a delete whose where has no predicate, then it reports ErrBadSyntax",
			input: "delete from student where",
		},
		// Ignoring what follows would delete the records where sid is 3 and
		// leave those where it is 4, which were asked to be deleted too.
		{
			name:  "given a delete followed by tokens the grammar has no place for, then it reports ErrBadSyntax",
			input: "delete from student where sid = 3 or sid = 4",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.delete(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("delete() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}

func TestParserModify(t *testing.T) {
	sidIs3 := query.NewPredicate(
		query.NewTerm(
			query.NewFieldExpression("sid"),
			query.NewConstantExpression(query.NewIntConstant(3)),
		),
	)

	tests := []struct {
		name      string
		input     string
		wantTable string
		wantField string
		wantValue query.Expression
		wantPred  query.Predicate
	}{
		// An update with no where clause changes every record, which a
		// predicate of no terms already says.
		{
			name:      "given an update of a field to a constant with no where clause, then it is modify data of the table, the field, the constant and a predicate of no terms",
			input:     "update student set gradyear = 2020",
			wantTable: "student",
			wantField: "gradyear",
			wantValue: query.NewConstantExpression(query.NewIntConstant(2020)),
			wantPred:  query.NewPredicate(),
		},
		{
			name:      "given an update of a field to a string with a where clause, then it is modify data of the table, the field, the string and the predicate written",
			input:     "update student set sname = 'Joe' where sid = 3",
			wantTable: "student",
			wantField: "sname",
			wantValue: query.NewConstantExpression(query.NewStringConstant("Joe")),
			wantPred:  sidIs3,
		},
		// The new value is an expression, so it can be what another field of
		// the same record holds.
		{
			name:      "given an update of a field to another field, then its new value is a field expression",
			input:     "update student set majorid = minorid where sid = 3",
			wantTable: "student",
			wantField: "majorid",
			wantValue: query.NewFieldExpression("minorid"),
			wantPred:  sidIs3,
		},
		{
			name:      "given an update written in upper case, then its keywords are read and its names are in lower case",
			input:     "UPDATE Student SET GradYear = 2020 WHERE SId = 3",
			wantTable: "student",
			wantField: "gradyear",
			wantValue: query.NewConstantExpression(query.NewIntConstant(2020)),
			wantPred:  sidIs3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.modify()
			if err != nil {
				t.Fatalf("modify() error = %v", err)
			}

			if got.TableName() != tt.wantTable {
				t.Errorf("TableName() = %q, want %q", got.TableName(), tt.wantTable)
			}
			if got.TargetField() != tt.wantField {
				t.Errorf("TargetField() = %q, want %q", got.TargetField(), tt.wantField)
			}
			if got.NewValue() != tt.wantValue {
				t.Errorf("NewValue() = %s (%T), want %s (%T)", got.NewValue(), got.NewValue(), tt.wantValue, tt.wantValue)
			}
			if !reflect.DeepEqual(got.Predicate(), tt.wantPred) {
				t.Errorf("Predicate() = %s, want %s", got.Predicate(), tt.wantPred)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given a statement that is not an update, then it reports ErrBadSyntax",
			input: "delete from student",
		},
		{
			name:  "given an update with no set, then it reports ErrBadSyntax",
			input: "update student gradyear = 2020",
		},
		{
			name:  "given an update with no field to set, then it reports ErrBadSyntax",
			input: "update student set = 2020",
		},
		{
			name:  "given an update with no equals sign, then it reports ErrBadSyntax",
			input: "update student set gradyear 2020",
		},
		{
			name:  "given an update with no new value, then it reports ErrBadSyntax",
			input: "update student set gradyear =",
		},
		{
			name:  "given an update whose where has no predicate, then it reports ErrBadSyntax",
			input: "update student set gradyear = 2020 where",
		},
		// Only one field is set per update, so a second assignment is tokens
		// the grammar has no place for. Ignoring it would leave sname as it
		// was without saying so.
		{
			name:  "given an update that sets two fields, then it reports ErrBadSyntax",
			input: "update student set gradyear = 2020, sname = 'Joe'",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.modify(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("modify() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}

func TestParserCreate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType UpdateCommand
	}{
		{
			name:     "given a create table, then it is create table data",
			input:    "create table student (sid int)",
			wantType: CreateTableData{},
		},
		{
			name:     "given a create view, then it is create view data",
			input:    "create view mathstudents as select sname from student",
			wantType: CreateViewData{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			got, err := p.create()
			if err != nil {
				t.Fatalf("create() error = %v", err)
			}
			if reflect.TypeOf(got) != reflect.TypeOf(tt.wantType) {
				t.Errorf("create() = %T, want %T", got, tt.wantType)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given a statement that is not a create, then it reports ErrBadSyntax",
			input: "delete from student",
		},
		{
			name:  "given a create of nothing, then it reports ErrBadSyntax",
			input: "create",
		},
		{
			name:  "given a create of a kind of thing there is no create for, then it reports ErrBadSyntax",
			input: "create database school",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParser(t, tt.input)

			if _, err := p.create(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("create() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}

// newTestParserAfterCreate makes a parser over s and eats the create it starts
// with, which is where create leaves the parser before it hands over to the
// method for the kind of thing created.
func newTestParserAfterCreate(t *testing.T, s string) *Parser {
	t.Helper()

	p := newTestParser(t, s)
	if err := p.lex.EatKeyword("create"); err != nil {
		t.Fatalf("EatKeyword(%q) of %q error = %v", "create", s, err)
	}

	return p
}

func TestParserCreateTable(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTable  string
		wantFields []fieldSpec
	}{
		// The order of the fields is the order a layout gives them offsets
		// in, so it is kept as written.
		{
			name:      "given a create table of int and varchar fields, then it is create table data of the table and a schema of the fields in the order written",
			input:     "create table student (sid int, sname varchar(10), gradyear int)",
			wantTable: "student",
			wantFields: []fieldSpec{
				{name: "sid", fieldType: recordmanager.FieldTypeInt},
				{name: "sname", fieldType: recordmanager.FieldTypeVarchar, length: 10},
				{name: "gradyear", fieldType: recordmanager.FieldTypeInt},
			},
		},
		{
			name:      "given a create table written in upper case, then its keywords and types are read and its names are in lower case",
			input:     "CREATE TABLE Student (SId INT, SName VARCHAR(10))",
			wantTable: "student",
			wantFields: []fieldSpec{
				{name: "sid", fieldType: recordmanager.FieldTypeInt},
				{name: "sname", fieldType: recordmanager.FieldTypeVarchar, length: 10},
			},
		},
		// A varchar of no characters holds only the empty string, which is of
		// little use but not wrong.
		{
			name:      "given a create table of a varchar of length zero, then its field has length zero",
			input:     "create table student (note varchar(0))",
			wantTable: "student",
			wantFields: []fieldSpec{
				{name: "note", fieldType: recordmanager.FieldTypeVarchar, length: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParserAfterCreate(t, tt.input)

			got, err := p.createTable()
			if err != nil {
				t.Fatalf("createTable() error = %v", err)
			}

			if got.TableName() != tt.wantTable {
				t.Errorf("TableName() = %q, want %q", got.TableName(), tt.wantTable)
			}

			sch := got.Schema()
			var gotFields []fieldSpec
			for _, name := range sch.Fields() {
				fieldType, err := sch.Type(name)
				if err != nil {
					t.Fatalf("Type(%q) error = %v", name, err)
				}
				length, err := sch.Length(name)
				if err != nil {
					t.Fatalf("Length(%q) error = %v", name, err)
				}
				gotFields = append(gotFields, fieldSpec{name: name, fieldType: fieldType, length: length})
			}
			if !slices.Equal(gotFields, tt.wantFields) {
				t.Errorf("fields of Schema() = %+v, want %+v", gotFields, tt.wantFields)
			}
		})
	}

	errTests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{
			name:    "given a create table with no table name, then it reports ErrBadSyntax",
			input:   "create table (sid int)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table with no parentheses around its fields, then it reports ErrBadSyntax",
			input:   "create table student sid int",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table of no fields, then it reports ErrBadSyntax",
			input:   "create table student ()",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table whose field has no type, then it reports ErrBadSyntax",
			input:   "create table student (sid)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table whose field is of a type there is none of, then it reports ErrBadSyntax",
			input:   "create table student (gpa float)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table of a varchar with no length, then it reports ErrBadSyntax",
			input:   "create table student (sname varchar)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table of a varchar whose length is a string, then it reports ErrBadSyntax",
			input:   "create table student (sname varchar('10'))",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table whose field list ends in a comma, then it reports ErrBadSyntax",
			input:   "create table student (sid int,)",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a create table followed by tokens the grammar has no place for, then it reports ErrBadSyntax",
			input:   "create table student (sid int) (sname varchar(10))",
			wantErr: ErrBadSyntax,
		},
		// The grammar allows any int here, so this is not a syntax error, but
		// no string is shorter than no characters.
		{
			name:    "given a create table of a varchar of negative length, then it reports ErrInvalidVarcharLength",
			input:   "create table student (sname varchar(-1))",
			wantErr: ErrInvalidVarcharLength,
		},
		{
			name:    "given a create table of two fields of the same name, then it reports recordmanager.ErrDuplicateField",
			input:   "create table student (sid int, sid varchar(10))",
			wantErr: recordmanager.ErrDuplicateField,
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParserAfterCreate(t, tt.input)

			if _, err := p.createTable(); !errors.Is(err, tt.wantErr) {
				t.Errorf("createTable() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestParserCreateView(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantView    string
		wantViewDef string
	}{
		{
			name:        "given a create view of a query with a where clause, then it is create view data of the view and the query written back out",
			input:       "create view mathstudents as select sname, sid from student where major = 'Math'",
			wantView:    "mathstudents",
			wantViewDef: "select sname, sid from student where major = 'Math'",
		},
		{
			name:        "given a create view written in upper case, then its names are in lower case and its query is written back out in lower case",
			input:       "CREATE VIEW MathStudents AS SELECT SName FROM Student",
			wantView:    "mathstudents",
			wantViewDef: "select sname from student",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParserAfterCreate(t, tt.input)

			got, err := p.createView()
			if err != nil {
				t.Fatalf("createView() error = %v", err)
			}

			if got.ViewName() != tt.wantView {
				t.Errorf("ViewName() = %q, want %q", got.ViewName(), tt.wantView)
			}
			if got.ViewDef() != tt.wantViewDef {
				t.Errorf("ViewDef() = %q, want %q", got.ViewDef(), tt.wantViewDef)
			}
		})
	}

	errTests := []struct {
		name  string
		input string
	}{
		{
			name:  "given a create view with no view name, then it reports ErrBadSyntax",
			input: "create view as select sname from student",
		},
		{
			name:  "given a create view with no as, then it reports ErrBadSyntax",
			input: "create view mathstudents select sname from student",
		},
		{
			name:  "given a create view with no query, then it reports ErrBadSyntax",
			input: "create view mathstudents as",
		},
		{
			name:  "given a create view of a statement that is not a query, then it reports ErrBadSyntax",
			input: "create view mathstudents as delete from student",
		},
		{
			name:  "given a create view followed by tokens the grammar has no place for, then it reports ErrBadSyntax",
			input: "create view mathstudents as select sname from student where sid = 3 or sid = 4",
		},
	}

	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestParserAfterCreate(t, tt.input)

			if _, err := p.createView(); !errors.Is(err, ErrBadSyntax) {
				t.Errorf("createView() error = %v, want %v", err, ErrBadSyntax)
			}
		})
	}
}
