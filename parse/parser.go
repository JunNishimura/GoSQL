package parse

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
)

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

// Query parses a select statement, which has to be the whole of the input.
//
// Tokens left over after the statement are an error rather than ignored.
// Ignoring them would run "where sid = 3 or sid = 4" as "where sid = 3", and
// answer a query other than the one that was asked without saying so.
func (p *Parser) Query() (QueryData, error) {
	if err := p.lex.EatKeyword("select"); err != nil {
		return QueryData{}, err
	}

	fields, err := p.idList()
	if err != nil {
		return QueryData{}, err
	}

	if err := p.lex.EatKeyword("from"); err != nil {
		return QueryData{}, err
	}

	tables, err := p.idList()
	if err != nil {
		return QueryData{}, err
	}

	pred := query.NewPredicate()
	if p.lex.MatchKeyword("where") {
		if err := p.lex.EatKeyword("where"); err != nil {
			return QueryData{}, err
		}

		pred, err = p.predicate()
		if err != nil {
			return QueryData{}, err
		}
	}

	if err := p.end(); err != nil {
		return QueryData{}, err
	}

	return QueryData{
		fields: fields,
		tables: tables,
		pred:   pred,
	}, nil
}

// UpdateCmd parses a statement that changes the database, which has to be the
// whole of the input.
func (p *Parser) UpdateCmd() (UpdateCommand, error) {
	if p.lex.MatchKeyword("insert") {
		return p.insert()
	}

	return nil, p.lex.unexpected("update command")
}

// insert parses an insert statement.
//
// The counts of fields and values are compared only once the statement has
// been read to its end, so that a statement wrong in both ways is reported for
// its syntax first.
func (p *Parser) insert() (InsertData, error) {
	if err := p.lex.EatKeyword("insert"); err != nil {
		return InsertData{}, err
	}
	if err := p.lex.EatKeyword("into"); err != nil {
		return InsertData{}, err
	}

	tableName, err := p.lex.EatID()
	if err != nil {
		return InsertData{}, err
	}

	if err := p.lex.EatDelim('('); err != nil {
		return InsertData{}, err
	}
	fields, err := p.idList()
	if err != nil {
		return InsertData{}, err
	}
	if err := p.lex.EatDelim(')'); err != nil {
		return InsertData{}, err
	}

	if err := p.lex.EatKeyword("values"); err != nil {
		return InsertData{}, err
	}

	if err := p.lex.EatDelim('('); err != nil {
		return InsertData{}, err
	}
	vals, err := p.constList()
	if err != nil {
		return InsertData{}, err
	}
	if err := p.lex.EatDelim(')'); err != nil {
		return InsertData{}, err
	}

	if err := p.end(); err != nil {
		return InsertData{}, err
	}

	if len(fields) != len(vals) {
		return InsertData{}, fmt.Errorf("insert into %s: %d fields and %d values: %w", tableName, len(fields), len(vals), ErrFieldValueCountMismatch)
	}

	return InsertData{
		tableName: tableName,
		fields:    fields,
		vals:      vals,
	}, nil
}

// constList parses one or more constants separated by commas.
func (p *Parser) constList() ([]query.Constant, error) {
	var vals []query.Constant
	for {
		val, err := p.constant()
		if err != nil {
			return nil, err
		}
		vals = append(vals, val)

		if !p.lex.MatchDelim(',') {
			return vals, nil
		}
		if err := p.lex.EatDelim(','); err != nil {
			return nil, err
		}
	}
}

// end reports ErrBadSyntax unless the statement just parsed is the whole of the
// input.
func (p *Parser) end() error {
	if !p.lex.MatchEnd() {
		return p.lex.unexpected("end of input")
	}

	return nil
}

// idList parses one or more names separated by commas.
//
// The fields of a select list and the tables of a from clause are both this,
// so one method reads either.
func (p *Parser) idList() ([]string, error) {
	var ids []string
	for {
		id, err := p.lex.EatID()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)

		if !p.lex.MatchDelim(',') {
			return ids, nil
		}
		if err := p.lex.EatDelim(','); err != nil {
			return nil, err
		}
	}
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
