package parse

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrBadSyntax is returned when the current token of the lexer is not the
// token that the parser requested.
var ErrBadSyntax = errors.New("bad syntax")

// keywords are the SQL words that cannot be used as a table or field name.
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

// Lexer is the interface through which the parser reads SQL. It holds the
// current token. The parser can check what the token is, and consume it.
//
// A Match method checks the current token and does not consume it. An Eat
// method consumes the current token if it has the requested kind, and reads
// the next token. Reading the next token is therefore part of an Eat, and the
// Eat that advanced returns any error from that read.
type Lexer struct {
	tz  *tokenizer
	cur token
}

// NewLexer returns a lexer positioned at the first token of s.
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

// MatchKeyword reports whether the current token is the keyword w. w must be
// in lower case.
//
// If w is not a keyword, MatchKeyword never matches, even if the current
// token is that exact word. Otherwise a keyword that the parser misspelled
// would match a field with the same spelling, and the query would parse.
func (l *Lexer) MatchKeyword(w string) bool {
	if _, isKeyword := keywords[w]; !isKeyword {
		return false
	}

	return l.cur.kind == tokenWord && l.cur.text == w
}

// MatchID reports whether the current token is a word that can be a table or
// field name. This is any word that is not a keyword.
func (l *Lexer) MatchID() bool {
	if l.cur.kind != tokenWord {
		return false
	}
	_, isKeyword := keywords[l.cur.text]

	return !isKeyword
}

// MatchEnd reports whether the lexer has reached the end of the input.
//
// It has no corresponding Eat method, because there is no token after the end
// to advance to.
func (l *Lexer) MatchEnd() bool {
	return l.cur.kind == tokenEOF
}

// EatDelim consumes the delimiter d.
func (l *Lexer) EatDelim(d rune) error {
	if !l.MatchDelim(d) {
		return l.unexpected(fmt.Sprintf("delimiter %q", d))
	}

	return l.advance()
}

// EatIntConstant consumes an int and returns its value.
//
// An int that does not fit in an int32 is rejected. A field holds an int32,
// so such a constant could never be compared with a field or written to one.
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

// EatStringConstant consumes a string constant and returns its text.
func (l *Lexer) EatStringConstant() (string, error) {
	if !l.MatchStringConstant() {
		return "", l.unexpected("string constant")
	}
	s := l.cur.text

	return s, l.advance()
}

// EatKeyword consumes the keyword w. w must be in lower case.
func (l *Lexer) EatKeyword(w string) error {
	if !l.MatchKeyword(w) {
		return l.unexpected(fmt.Sprintf("keyword %q", w))
	}

	return l.advance()
}

// EatID consumes a word that is not a keyword and returns it in lower case.
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

// unexpected returns the error for a current token that is not want.
func (l *Lexer) unexpected(want string) error {
	if l.cur.kind == tokenEOF {
		return fmt.Errorf("%w: want %s, got end of input", ErrBadSyntax, want)
	}

	return fmt.Errorf("%w: want %s, got %q", ErrBadSyntax, want, l.cur.text)
}
