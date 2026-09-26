package parse

import (
	"errors"
	"testing"
)

// newTestLexer makes a lexer over s, failing the test if the first token
// cannot be read.
func newTestLexer(t *testing.T, s string) *Lexer {
	t.Helper()

	l, err := NewLexer(s)
	if err != nil {
		t.Fatalf("NewLexer(%q) error = %v", s, err)
	}

	return l
}

func TestNewLexer(t *testing.T) {
	t.Run("when the first token is a string constant with no closing quote, then it reports ErrUnterminatedString", func(t *testing.T) {
		_, err := NewLexer("'Joe")
		if !errors.Is(err, ErrUnterminatedString) {
			t.Errorf("NewLexer() error = %v, want %v", err, ErrUnterminatedString)
		}
	})
}

func TestLexerMatchDelim(t *testing.T) {
	tests := []struct {
		name  string
		input string
		delim rune
		want  bool
	}{
		{
			name:  "given a lexer at a comma, when a comma is matched, then it matches",
			input: ",",
			delim: ',',
			want:  true,
		},
		{
			name:  "given a lexer at a comma, when an equals sign is matched, then it does not match",
			input: ",",
			delim: '=',
			want:  false,
		},
		{
			name:  "given a lexer at a word, when a delimiter is matched, then it does not match",
			input: "a",
			delim: 'a',
			want:  false,
		},
		{
			name:  "given a lexer at the end of its input, when a delimiter is matched, then it does not match",
			input: "",
			delim: ',',
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchDelim(tt.delim); got != tt.want {
				t.Errorf("MatchDelim(%q) = %t, want %t", tt.delim, got, tt.want)
			}
		})
	}
}

func TestLexerMatchIntConstant(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "given a lexer at an int, then it matches",
			input: "42",
			want:  true,
		},
		{
			name:  "given a lexer at a string constant whose text is digits, then it does not match",
			input: "'42'",
			want:  false,
		},
		{
			name:  "given a lexer at a word, then it does not match",
			input: "a",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchIntConstant(); got != tt.want {
				t.Errorf("MatchIntConstant() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLexerMatchStringConstant(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "given a lexer at a string constant, then it matches",
			input: "'Joe'",
			want:  true,
		},
		{
			name:  "given a lexer at a word, then it does not match",
			input: "joe",
			want:  false,
		},
		{
			name:  "given a lexer at an int, then it does not match",
			input: "42",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchStringConstant(); got != tt.want {
				t.Errorf("MatchStringConstant() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLexerMatchKeyword(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		keyword string
		want    bool
	}{
		{
			name:    "given a lexer at the word select, when select is matched, then it matches",
			input:   "select",
			keyword: "select",
			want:    true,
		},
		{
			name:    "given a lexer at the word SELECT in upper case, when select is matched, then it matches",
			input:   "SELECT",
			keyword: "select",
			want:    true,
		},
		{
			name:    "given a lexer at the word select, when from is matched, then it does not match",
			input:   "select",
			keyword: "from",
			want:    false,
		},
		// Only a keyword is matched as one, so a keyword misspelled by the
		// caller cannot match a field of the same name in the input.
		{
			name:    "given a lexer at a word that is not a keyword, when that word is matched as a keyword, then it does not match",
			input:   "sname",
			keyword: "sname",
			want:    false,
		},
		// A string constant is a value, so its text being a keyword makes it
		// no more a keyword than any other value.
		{
			name:    "given a lexer at a string constant whose text is select, when select is matched, then it does not match",
			input:   "'select'",
			keyword: "select",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchKeyword(tt.keyword); got != tt.want {
				t.Errorf("MatchKeyword(%q) = %t, want %t", tt.keyword, got, tt.want)
			}
		})
	}
}

func TestLexerMatchID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "given a lexer at a word that is not a keyword, then it matches",
			input: "sname",
			want:  true,
		},
		// The tokenizer reads keywords and identifiers alike as words, so this
		// is the case where the lexer tells the two apart.
		{
			name:  "given a lexer at a keyword, then it does not match",
			input: "select",
			want:  false,
		},
		{
			name:  "given a lexer at a string constant, then it does not match",
			input: "'sname'",
			want:  false,
		},
		{
			name:  "given a lexer at an int, then it does not match",
			input: "42",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchID(); got != tt.want {
				t.Errorf("MatchID() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestLexerMatchEnd(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "given a lexer over an empty input, then it matches",
			input: "",
			want:  true,
		},
		{
			name:  "given a lexer over input of only whitespace, then it matches",
			input: " \t\n ",
			want:  true,
		},
		{
			name:  "given a lexer at a token, then it does not match",
			input: "or",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			if got := l.MatchEnd(); got != tt.want {
				t.Errorf("MatchEnd() = %t, want %t", got, tt.want)
			}
		})
	}

	t.Run("given a lexer at the last token, when it is eaten, then it matches", func(t *testing.T) {
		l := newTestLexer(t, "sname")

		if _, err := l.EatID(); err != nil {
			t.Fatalf("EatID() error = %v", err)
		}
		if !l.MatchEnd() {
			t.Errorf("MatchEnd() = false after the last token was eaten, want true")
		}
	})
}

// What follows the token being eaten is always a keyword, so that each case
// can tell that the lexer moved on by matching it.
func TestLexerEatDelim(t *testing.T) {
	t.Run("given a lexer at a comma, when a comma is eaten, then it moves on to the next token", func(t *testing.T) {
		l := newTestLexer(t, ", from")

		if err := l.EatDelim(','); err != nil {
			t.Fatalf("EatDelim(',') error = %v", err)
		}
		if !l.MatchKeyword("from") {
			t.Errorf("MatchKeyword(%q) = false after EatDelim, want true", "from")
		}
	})

	t.Run("given a lexer at a comma, when an equals sign is eaten, then it reports ErrBadSyntax", func(t *testing.T) {
		l := newTestLexer(t, ",")

		if err := l.EatDelim('='); !errors.Is(err, ErrBadSyntax) {
			t.Errorf("EatDelim('=') error = %v, want %v", err, ErrBadSyntax)
		}
	})
}

func TestLexerEatIntConstant(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int32
		wantErr error
	}{
		{
			name:  "given a lexer at an int, then it returns the int",
			input: "42 from",
			want:  42,
		},
		{
			name:  "given a lexer at a negative int, then it returns the int",
			input: "-42 from",
			want:  -42,
		},
		{
			name:  "given a lexer at the largest int32, then it returns the int",
			input: "2147483647 from",
			want:  2147483647,
		},
		{
			name:  "given a lexer at the smallest int32, then it returns the int",
			input: "-2147483648 from",
			want:  -2147483648,
		},
		// A field holds an int32, so a number one past what it can hold is not
		// a constant that could ever be compared with or written to one.
		{
			name:    "given a lexer at an int one past the largest int32, then it reports ErrBadSyntax",
			input:   "2147483648",
			wantErr: ErrBadSyntax,
		},
		{
			name:    "given a lexer at a string constant whose text is digits, then it reports ErrBadSyntax",
			input:   "'42'",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			got, err := l.EatIntConstant()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("EatIntConstant() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("EatIntConstant() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("EatIntConstant() = %d, want %d", got, tt.want)
			}
			if !l.MatchKeyword("from") {
				t.Errorf("MatchKeyword(%q) = false after EatIntConstant, want true", "from")
			}
		})
	}
}

func TestLexerEatStringConstant(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "given a lexer at a string constant, then it returns the text between the quotes",
			input: "'Joe Smith' from",
			want:  "Joe Smith",
		},
		{
			name:    "given a lexer at a word, then it reports ErrBadSyntax",
			input:   "joe",
			wantErr: ErrBadSyntax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			got, err := l.EatStringConstant()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("EatStringConstant() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("EatStringConstant() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("EatStringConstant() = %q, want %q", got, tt.want)
			}
			if !l.MatchKeyword("from") {
				t.Errorf("MatchKeyword(%q) = false after EatStringConstant, want true", "from")
			}
		})
	}
}

func TestLexerEatKeyword(t *testing.T) {
	t.Run("given a lexer at the word select, when select is eaten, then it moves on to the next token", func(t *testing.T) {
		l := newTestLexer(t, "select from")

		if err := l.EatKeyword("select"); err != nil {
			t.Fatalf("EatKeyword(%q) error = %v", "select", err)
		}
		if !l.MatchKeyword("from") {
			t.Errorf("MatchKeyword(%q) = false after EatKeyword, want true", "from")
		}
	})

	t.Run("given a lexer at the word select, when from is eaten, then it reports ErrBadSyntax", func(t *testing.T) {
		l := newTestLexer(t, "select")

		if err := l.EatKeyword("from"); !errors.Is(err, ErrBadSyntax) {
			t.Errorf("EatKeyword(%q) error = %v, want %v", "from", err, ErrBadSyntax)
		}
	})
}

func TestLexerEatID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "given a lexer at a word that is not a keyword, then it returns the word",
			input: "sname from",
			want:  "sname",
		},
		{
			name:  "given a lexer at a word in upper case, then it returns the word in lower case",
			input: "SName from",
			want:  "sname",
		},
		{
			name:    "given a lexer at a keyword, then it reports ErrBadSyntax",
			input:   "select",
			wantErr: ErrBadSyntax,
		},
		// Reading the token after the one eaten is part of eating it, so a
		// token that cannot be read shows up here and not at the next match.
		{
			name:    "given a lexer at a word followed by a string constant with no closing quote, then it reports ErrUnterminatedString",
			input:   "sname 'Joe",
			wantErr: ErrUnterminatedString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLexer(t, tt.input)

			got, err := l.EatID()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("EatID() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("EatID() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("EatID() = %q, want %q", got, tt.want)
			}
			if !l.MatchKeyword("from") {
				t.Errorf("MatchKeyword(%q) = false after EatID, want true", "from")
			}
		})
	}
}
