package parse

import "github.com/JunNishimura/GoSQL/query"

// Parser reads SQL by recursive descent, one method for each rule of the
// grammar, and builds what the rules describe as it goes.
//
// Each method starts at the first token of its rule and leaves the lexer at the
// first token after it, so a rule made of others is those methods called in the
// order their parts are written.
type Parser struct {
	lex *Lexer
}

// NewParser returns a parser at the start of s.
func NewParser(s string) (*Parser, error) {
	lex, err := NewLexer(s)
	if err != nil {
		return nil, err
	}

	return &Parser{lex: lex}, nil
}

// field parses a field name.
func (p *Parser) field() (string, error) {
	return p.lex.EatID()
}

// constant parses an int or a string constant.
func (p *Parser) constant() (query.Constant, error) {
	if p.lex.MatchStringConstant() {
		s, err := p.lex.EatStringConstant()
		if err != nil {
			return query.Constant{}, err
		}

		return query.NewStringConstant(s), nil
	}

	n, err := p.lex.EatIntConstant()
	if err != nil {
		return query.Constant{}, err
	}

	return query.NewIntConstant(n), nil
}

// expression parses a field name or a constant.
func (p *Parser) expression() (query.Expression, error) {
	if p.lex.MatchID() {
		name, err := p.field()
		if err != nil {
			return nil, err
		}

		return query.NewFieldExpression(name), nil
	}

	val, err := p.constant()
	if err != nil {
		return nil, err
	}

	return query.NewConstantExpression(val), nil
}

// term parses two expressions with an equals sign between them.
func (p *Parser) term() (query.Term, error) {
	lhs, err := p.expression()
	if err != nil {
		return query.Term{}, err
	}

	if err := p.lex.EatDelim('='); err != nil {
		return query.Term{}, err
	}

	rhs, err := p.expression()
	if err != nil {
		return query.Term{}, err
	}

	return query.NewTerm(lhs, rhs), nil
}

// predicate parses one or more terms joined by "and".
//
// The terms are gathered in a loop and made into one predicate at the end,
// rather than parsing what follows each "and" as a predicate of its own and
// conjoining it, which would build a predicate for every term only to take its
// terms back out.
func (p *Parser) predicate() (query.Predicate, error) {
	var terms []query.Term
	for {
		t, err := p.term()
		if err != nil {
			return query.Predicate{}, err
		}
		terms = append(terms, t)

		if !p.lex.MatchKeyword("and") {
			return query.NewPredicate(terms...), nil
		}
		if err := p.lex.EatKeyword("and"); err != nil {
			return query.Predicate{}, err
		}
	}
}
