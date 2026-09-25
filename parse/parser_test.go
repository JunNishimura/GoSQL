package parse

import (
	"errors"
	"reflect"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
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
