package parse

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
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

	pred, err := p.optionalWhere()
	if err != nil {
		return QueryData{}, err
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
	switch {
	case p.lex.MatchKeyword("insert"):
		return p.insert()
	case p.lex.MatchKeyword("delete"):
		return p.delete()
	case p.lex.MatchKeyword("update"):
		return p.modify()
	case p.lex.MatchKeyword("create"):
		return p.create()
	default:
		return nil, p.lex.unexpected("update command")
	}
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

// delete parses a delete statement.
func (p *Parser) delete() (DeleteData, error) {
	if err := p.lex.EatKeyword("delete"); err != nil {
		return DeleteData{}, err
	}
	if err := p.lex.EatKeyword("from"); err != nil {
		return DeleteData{}, err
	}

	tableName, err := p.lex.EatID()
	if err != nil {
		return DeleteData{}, err
	}

	pred, err := p.optionalWhere()
	if err != nil {
		return DeleteData{}, err
	}

	if err := p.end(); err != nil {
		return DeleteData{}, err
	}

	return DeleteData{
		tableName: tableName,
		pred:      pred,
	}, nil
}

// modify parses an update statement.
//
// Only one field is set per statement, so a second assignment is left over
// after it and reported by end, rather than ignored.
func (p *Parser) modify() (ModifyData, error) {
	if err := p.lex.EatKeyword("update"); err != nil {
		return ModifyData{}, err
	}

	tableName, err := p.lex.EatID()
	if err != nil {
		return ModifyData{}, err
	}

	if err := p.lex.EatKeyword("set"); err != nil {
		return ModifyData{}, err
	}

	fieldName, err := p.field()
	if err != nil {
		return ModifyData{}, err
	}

	if err := p.lex.EatDelim('='); err != nil {
		return ModifyData{}, err
	}

	newValue, err := p.expression()
	if err != nil {
		return ModifyData{}, err
	}

	pred, err := p.optionalWhere()
	if err != nil {
		return ModifyData{}, err
	}

	if err := p.end(); err != nil {
		return ModifyData{}, err
	}

	return ModifyData{
		tableName: tableName,
		fieldName: fieldName,
		newValue:  newValue,
		pred:      pred,
	}, nil
}

// create parses a create statement, handing over to the method for the kind of
// thing created once the create has been eaten.
//
// The kind is the word after the create, and the lexer sees only the token it
// is at, so it cannot be told apart before the create is taken.
func (p *Parser) create() (UpdateCommand, error) {
	if err := p.lex.EatKeyword("create"); err != nil {
		return nil, err
	}

	switch {
	case p.lex.MatchKeyword("table"):
		return p.createTable()
	case p.lex.MatchKeyword("view"):
		return p.createView()
	case p.lex.MatchKeyword("index"):
		return p.createIndex()
	default:
		return nil, p.lex.unexpected("table, view or index")
	}
}

// fieldSpec is what a create table says about one field, before it is added to
// a schema. The length is an int, as the schema takes it, and is unused for an
// int field.
type fieldSpec struct {
	name      string
	fieldType recordmanager.FieldType
	length    int
}

// createTable parses the rest of a create table statement, from the table that
// follows the create.
//
// The fields are added to the schema only once the statement has been read to
// its end, so that, as with an insert, a statement wrong in more than one way
// is reported for its syntax first.
func (p *Parser) createTable() (CreateTableData, error) {
	if err := p.lex.EatKeyword("table"); err != nil {
		return CreateTableData{}, err
	}

	tableName, err := p.lex.EatID()
	if err != nil {
		return CreateTableData{}, err
	}

	if err := p.lex.EatDelim('('); err != nil {
		return CreateTableData{}, err
	}
	specs, err := p.fieldDefs()
	if err != nil {
		return CreateTableData{}, err
	}
	if err := p.lex.EatDelim(')'); err != nil {
		return CreateTableData{}, err
	}

	if err := p.end(); err != nil {
		return CreateTableData{}, err
	}

	sch := recordmanager.NewSchema()
	for _, spec := range specs {
		if spec.length < 0 {
			return CreateTableData{}, fmt.Errorf("create table %s: field %s of length %d: %w", tableName, spec.name, spec.length, ErrInvalidVarcharLength)
		}
		if err := sch.AddField(spec.name, spec.fieldType, spec.length); err != nil {
			return CreateTableData{}, fmt.Errorf("create table %s: %w", tableName, err)
		}
	}

	return CreateTableData{
		tableName: tableName,
		schema:    sch,
	}, nil
}

// fieldDefs parses one or more field definitions separated by commas.
func (p *Parser) fieldDefs() ([]fieldSpec, error) {
	return commaSeparated(p, p.fieldDef)
}

// fieldDef parses a field name and the type that follows it: int, or varchar
// with its length in parentheses.
func (p *Parser) fieldDef() (fieldSpec, error) {
	name, err := p.field()
	if err != nil {
		return fieldSpec{}, err
	}

	switch {
	case p.lex.MatchKeyword("int"):
		if err := p.lex.EatKeyword("int"); err != nil {
			return fieldSpec{}, err
		}

		return fieldSpec{name: name, fieldType: recordmanager.FieldTypeInt}, nil
	case p.lex.MatchKeyword("varchar"):
		if err := p.lex.EatKeyword("varchar"); err != nil {
			return fieldSpec{}, err
		}
		if err := p.lex.EatDelim('('); err != nil {
			return fieldSpec{}, err
		}
		length, err := p.lex.EatIntConstant()
		if err != nil {
			return fieldSpec{}, err
		}
		if err := p.lex.EatDelim(')'); err != nil {
			return fieldSpec{}, err
		}

		return fieldSpec{name: name, fieldType: recordmanager.FieldTypeVarchar, length: int(length)}, nil
	default:
		return fieldSpec{}, p.lex.unexpected("field type")
	}
}

// createView parses the rest of a create view statement, from the view that
// follows the create.
//
// The query is the last part of the statement, so the check Query makes that
// nothing follows it is also the check that nothing follows the statement.
func (p *Parser) createView() (CreateViewData, error) {
	if err := p.lex.EatKeyword("view"); err != nil {
		return CreateViewData{}, err
	}

	viewName, err := p.lex.EatID()
	if err != nil {
		return CreateViewData{}, err
	}

	if err := p.lex.EatKeyword("as"); err != nil {
		return CreateViewData{}, err
	}

	qd, err := p.Query()
	if err != nil {
		return CreateViewData{}, err
	}

	return CreateViewData{
		viewName: viewName,
		qd:       qd,
	}, nil
}

// createIndex parses the rest of a create index statement, from the index that
// follows the create.
//
// An index is on one field, so a second one is left where the closing
// parenthesis should be and reported, rather than ignored.
func (p *Parser) createIndex() (CreateIndexData, error) {
	if err := p.lex.EatKeyword("index"); err != nil {
		return CreateIndexData{}, err
	}

	indexName, err := p.lex.EatID()
	if err != nil {
		return CreateIndexData{}, err
	}

	if err := p.lex.EatKeyword("on"); err != nil {
		return CreateIndexData{}, err
	}

	tableName, err := p.lex.EatID()
	if err != nil {
		return CreateIndexData{}, err
	}

	if err := p.lex.EatDelim('('); err != nil {
		return CreateIndexData{}, err
	}
	fieldName, err := p.field()
	if err != nil {
		return CreateIndexData{}, err
	}
	if err := p.lex.EatDelim(')'); err != nil {
		return CreateIndexData{}, err
	}

	if err := p.end(); err != nil {
		return CreateIndexData{}, err
	}

	return CreateIndexData{
		indexName: indexName,
		tableName: tableName,
		fieldName: fieldName,
	}, nil
}

// optionalWhere parses a where clause if there is one, and returns its
// predicate. With no where clause, it returns a predicate of no terms, which
// every record meets, so a statement without one needs nothing apart.
func (p *Parser) optionalWhere() (query.Predicate, error) {
	if !p.lex.MatchKeyword("where") {
		return query.NewPredicate(), nil
	}

	if err := p.lex.EatKeyword("where"); err != nil {
		return query.Predicate{}, err
	}

	return p.predicate()
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
	return commaSeparated(p, p.lex.EatID)
}

// constList parses one or more constants separated by commas.
func (p *Parser) constList() ([]query.Constant, error) {
	return commaSeparated(p, p.constant)
}

// commaSeparated parses one or more of what item parses, separated by commas,
// and returns them in the order written.
//
// It is a function rather than a method because a method cannot take a type
// parameter. A list ends at the first item not followed by a comma, so a comma
// with nothing after it is an item that item fails to parse.
func commaSeparated[T any](p *Parser, item func() (T, error)) ([]T, error) {
	var items []T
	for {
		it, err := item()
		if err != nil {
			return nil, err
		}
		items = append(items, it)

		if !p.lex.MatchDelim(',') {
			return items, nil
		}
		if err := p.lex.EatDelim(','); err != nil {
			return nil, err
		}
	}
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

// field parses a field name.
func (p *Parser) field() (string, error) {
	return p.lex.EatID()
}
