package parse

import (
	"errors"
	"slices"
	"testing"
)

// tokenize reads tokens from s until the end of the input, and returns them
// without the final end-of-input token.
func tokenize(t *testing.T, s string) ([]token, error) {
	t.Helper()

	tz := newTokenizer(s)

	var toks []token
	for {
		tok, err := tz.next()
		if err != nil {
			return toks, err
		}
		if tok.kind == tokenEOF {
			return toks, nil
		}
		toks = append(toks, tok)
	}
}

func TestTokenizerNext(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []token
	}{
		{
			name:  "when the input is empty, then there are no tokens",
			input: "",
			want:  nil,
		},
		{
			name:  "when the input is only whitespace, then there are no tokens",
			input: " \t\n ",
			want:  nil,
		},
		{
			name:  "when a word is read, then it is a word token",
			input: "select",
			want: []token{
				{kind: tokenWord, text: "select"},
			},
		},
		// Keywords and field names are matched without regard to case, so the
		// tokenizer settles that once rather than leaving it to every match.
		{
			name:  "when a word in upper case is read, then its text is in lower case",
			input: "SELECT StudentName",
			want: []token{
				{kind: tokenWord, text: "select"},
				{kind: tokenWord, text: "studentname"},
			},
		},
		{
			name:  "when a word has underscores and digits, then they are part of the word",
			input: "_student_id2",
			want: []token{
				{kind: tokenWord, text: "_student_id2"},
			},
		},
		{
			name:  "when digits are read, then they are an int token",
			input: "42",
			want: []token{
				{kind: tokenInt, text: "42"},
			},
		},
		{
			name:  "when a minus sign is followed by digits, then they are one negative int token",
			input: "-42",
			want: []token{
				{kind: tokenInt, text: "-42"},
			},
		},
		{
			name:  "when a minus sign is not followed by a digit, then it is a delimiter",
			input: "- 42",
			want: []token{
				{kind: tokenDelim, text: "-"},
				{kind: tokenInt, text: "42"},
			},
		},
		{
			name:  "when digits run into letters, then the int ends where the letters begin",
			input: "42abc",
			want: []token{
				{kind: tokenInt, text: "42"},
				{kind: tokenWord, text: "abc"},
			},
		},
		// Unlike a word, a string constant is a value as it is stored, so its
		// case is kept.
		{
			name:  "when text in single quotes is read, then it is a string token without the quotes and with its case kept",
			input: "'Joe Smith'",
			want: []token{
				{kind: tokenString, text: "Joe Smith"},
			},
		},
		{
			name:  "when a pair of single quotes is read, then it is a string token of the empty string",
			input: "''",
			want: []token{
				{kind: tokenString, text: ""},
			},
		},
		// A dot is what separates a table from a field, so it must not be
		// taken into the word on either side.
		{
			name:  "when a dot sits between two words, then it is a delimiter of its own",
			input: "student.sname",
			want: []token{
				{kind: tokenWord, text: "student"},
				{kind: tokenDelim, text: "."},
				{kind: tokenWord, text: "sname"},
			},
		},
		{
			name:  "when punctuation is read without spaces around it, then each character is a delimiter of its own",
			input: "(a,b)=",
			want: []token{
				{kind: tokenDelim, text: "("},
				{kind: tokenWord, text: "a"},
				{kind: tokenDelim, text: ","},
				{kind: tokenWord, text: "b"},
				{kind: tokenDelim, text: ")"},
				{kind: tokenDelim, text: "="},
			},
		},
		{
			name:  "when a whole query is read, then its words, delimiters and constants come out in order",
			input: "select sname from student where sid = 3 and major = 'Math'",
			want: []token{
				{kind: tokenWord, text: "select"},
				{kind: tokenWord, text: "sname"},
				{kind: tokenWord, text: "from"},
				{kind: tokenWord, text: "student"},
				{kind: tokenWord, text: "where"},
				{kind: tokenWord, text: "sid"},
				{kind: tokenDelim, text: "="},
				{kind: tokenInt, text: "3"},
				{kind: tokenWord, text: "and"},
				{kind: tokenWord, text: "major"},
				{kind: tokenDelim, text: "="},
				{kind: tokenString, text: "Math"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokenize(t, tt.input)
			if err != nil {
				t.Fatalf("next() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("tokens of %q = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}

	t.Run("when a string constant has no closing quote, then it reports ErrUnterminatedString", func(t *testing.T) {
		_, err := tokenize(t, "'Joe")
		if !errors.Is(err, ErrUnterminatedString) {
			t.Errorf("next() error = %v, want %v", err, ErrUnterminatedString)
		}
	})

	// The lexer looks at the current token after the last one has been
	// eaten, so reaching the end must not be something that happens once.
	t.Run("given a tokenizer at the end of its input, when next is called again, then it is still the end of input", func(t *testing.T) {
		tz := newTokenizer("a")

		for i := range 3 {
			if _, err := tz.next(); err != nil {
				t.Fatalf("call %d: next() error = %v", i+1, err)
			}
		}

		tok, err := tz.next()
		if err != nil {
			t.Fatalf("next() error = %v", err)
		}
		if tok.kind != tokenEOF {
			t.Errorf("next() = %+v, want end of input", tok)
		}
	})
}
