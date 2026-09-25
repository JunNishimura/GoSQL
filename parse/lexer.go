package parse

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrBadSyntax reports that the token the lexer is at is not the one the
// parser asked to eat.
var ErrBadSyntax = errors.New("bad syntax")

// keywords are the words of SQL that cannot name a table or a field.
var keywords = map[string]struct{}{
	"select":  {},
	"from":    {},
	"where":   {},
	"and":     {},
	"insert":  {},
	"into":    {},
	"values":  {},
	"delete":  {},
	"update":  {},
	"set":     {},
	"create":  {},
	"table":   {},
	"int":     {},
	"varchar": {},
	"view":    {},
	"as":      {},
	"index":   {},
	"on":      {},
}

// Lexer is what the parser reads SQL through: it holds the token it is at, and
// lets the parser ask what that token is and take it.
//
// A Match method looks at the current token and leaves it where it is. An Eat
// method takes the current token if it is of the kind asked for, and reads the
// one after it; so reading the next token is part of eating one, and an error
// from reading it is returned by the Eat that moved there.
type Lexer struct {
	tz  *tokenizer
	cur token
}

// NewLexer returns a lexer at the first token of s.
func NewLexer(s string) (*Lexer, error) {
	l := &Lexer{tz: newTokenizer(s)}
	if err := l.advance(); err != nil {
		return nil, err
	}

	return l, nil
}

// MatchDelim reports whether the current token is the delimiter d.
func (l *Lexer) MatchDelim(d rune) bool {
	return l.cur.kind == tokenDelim && l.cur.text == string(d)
}

// MatchIntConstant reports whether the current token is an int.
func (l *Lexer) MatchIntConstant() bool {
	return l.cur.kind == tokenInt
}

// MatchStringConstant reports whether the current token is a string constant.
func (l *Lexer) MatchStringConstant() bool {
	return l.cur.kind == tokenString
}

// MatchKeyword reports whether the current token is the keyword w, which is
// given in lower case.
//
// A w that is not a keyword never matches, even where the current token is
// that very word. Otherwise a keyword misspelled by the parser would match a
// field that happens to share the misspelling, and the query would parse.
func (l *Lexer) MatchKeyword(w string) bool {
	if _, isKeyword := keywords[w]; !isKeyword {
		return false
	}

	return l.cur.kind == tokenWord && l.cur.text == w
}

// MatchID reports whether the current token is a word that could name a table
// or a field, which is any word but a keyword.
func (l *Lexer) MatchID() bool {
	if l.cur.kind != tokenWord {
		return false
	}
	_, isKeyword := keywords[l.cur.text]

	return !isKeyword
}

// EatDelim takes the delimiter d.
func (l *Lexer) EatDelim(d rune) error {
	if !l.MatchDelim(d) {
		return l.unexpected(fmt.Sprintf("delimiter %q", d))
	}

	return l.advance()
}

// EatIntConstant takes an int and returns its value.
//
// An int that does not fit in an int32 is refused, since that is what a field
// holds and the constant could never be compared with or written to one.
func (l *Lexer) EatIntConstant() (int32, error) {
	if !l.MatchIntConstant() {
		return 0, l.unexpected("int constant")
	}

	n, err := strconv.ParseInt(l.cur.text, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: int constant %s: %w", ErrBadSyntax, l.cur.text, err)
	}

	return int32(n), l.advance()
}

// EatStringConstant takes a string constant and returns its text.
func (l *Lexer) EatStringConstant() (string, error) {
	if !l.MatchStringConstant() {
		return "", l.unexpected("string constant")
	}
	s := l.cur.text

	return s, l.advance()
}

// EatKeyword takes the keyword w, which is given in lower case.
func (l *Lexer) EatKeyword(w string) error {
	if !l.MatchKeyword(w) {
		return l.unexpected(fmt.Sprintf("keyword %q", w))
	}

	return l.advance()
}

// EatID takes a word that is not a keyword and returns it in lower case.
func (l *Lexer) EatID() (string, error) {
	if !l.MatchID() {
		return "", l.unexpected("identifier")
	}
	id := l.cur.text

	return id, l.advance()
}

func (l *Lexer) advance() error {
	tok, err := l.tz.next()
	if err != nil {
		return err
	}
	l.cur = tok

	return nil
}

// unexpected is the error for being at the current token when want was asked
// for.
func (l *Lexer) unexpected(want string) error {
	if l.cur.kind == tokenEOF {
		return fmt.Errorf("%w: want %s, got end of input", ErrBadSyntax, want)
	}

	return fmt.Errorf("%w: want %s, got %q", ErrBadSyntax, want, l.cur.text)
}
