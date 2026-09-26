package parse

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrUnterminatedString reports a string constant whose closing quote never
// comes before the end of the input.
var ErrUnterminatedString = errors.New("unterminated string constant")

// tokenKind is which of the few shapes a token of SQL can take.
//
// Keywords and identifiers are both words here. Which words are keywords is a
// matter of the grammar, so the lexer, which knows the grammar, tells the two
// apart; the tokenizer only knows where one word ends and the next begins.
type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenDelim
	tokenInt
	tokenString
	tokenWord
)

// token is one piece of the input.
//
// The text of a word is in lower case, the text of a string constant is what
// was between the quotes, and the text of an int is its digits, with a leading
// minus sign if it had one. An int is left as text so that whoever turns it
// into a number is also the one to say what happens when it does not fit.
type token struct {
	kind tokenKind
	text string
}

// tokenizer splits SQL into tokens, one at a time.
type tokenizer struct {
	input []rune
	pos   int
}

func newTokenizer(s string) *tokenizer {
	return &tokenizer{input: []rune(s)}
}

// next reads the token that follows the last one read.
//
// Once the input is used up, every call returns a token of kind tokenEOF.
func (t *tokenizer) next() (token, error) {
	t.skipSpace()

	if t.pos >= len(t.input) {
		return token{kind: tokenEOF}, nil
	}

	r := t.input[t.pos]
	switch {
	case isWordStart(r):
		return token{kind: tokenWord, text: strings.ToLower(t.readWhile(isWordPart))}, nil
	case unicode.IsDigit(r):
		return token{kind: tokenInt, text: t.readWhile(unicode.IsDigit)}, nil
	case r == '-' && t.pos+1 < len(t.input) && unicode.IsDigit(t.input[t.pos+1]):
		t.pos++
		return token{kind: tokenInt, text: "-" + t.readWhile(unicode.IsDigit)}, nil
	case r == '\'':
		return t.readString()
	default:
		t.pos++
		return token{kind: tokenDelim, text: string(r)}, nil
	}
}

func (t *tokenizer) skipSpace() {
	t.readWhile(unicode.IsSpace)
}

// readWhile consumes runes as long as keep holds, and returns what it
// consumed.
func (t *tokenizer) readWhile(keep func(rune) bool) string {
	start := t.pos
	for t.pos < len(t.input) && keep(t.input[t.pos]) {
		t.pos++
	}

	return string(t.input[start:t.pos])
}

// readString reads a string constant, with t.pos at its opening quote.
func (t *tokenizer) readString() (token, error) {
	start := t.pos
	t.pos++

	text := t.readWhile(func(r rune) bool { return r != '\'' })
	if t.pos >= len(t.input) {
		return token{}, fmt.Errorf("read string constant at %d: %w", start, ErrUnterminatedString)
	}
	t.pos++

	return token{kind: tokenString, text: text}, nil
}

func isWordStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func isWordPart(r rune) bool {
	return isWordStart(r) || unicode.IsDigit(r)
}
