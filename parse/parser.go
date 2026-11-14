package parse

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// Parser is a recursive descent parser for SQL. It has one method for each
// rule of the grammar, and each method builds the data that its rule
// describes.
//
// Each method starts at the first token of its rule and leaves the lexer at
// the first token after the rule. A rule that consists of other rules
// therefore calls their methods in the order in which its parts are written.
type Parser struct {
	lex *Lexer
}

// NewParser returns a parser positioned at the start of s.
func NewParser(s string) (*Parser, error) {
	lex, err := NewLexer(s)
	if err != nil {
		return nil, err
	}

	return &Parser{lex: lex}, nil
}

// Query parses a select statement, which must be the whole input.
//
// Tokens that remain after the statement are an error and are not ignored.
// If they were ignored, "where sid = 3 or sid = 4" would run as "where sid =
// 3". The database would then silently answer a different query than the one
// that was written.
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

// UpdateCmd parses a statement that changes the database, which must be the
// whole input.
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
// The numbers of fields and values are compared only after the whole
// statement has been read. A statement with both a syntax error and a count
// mismatch is then reported for its syntax first.
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
// A statement sets only one field. A second assignment remains unparsed after
// the statement, and end reports it as an error.
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

// create parses a create statement. After it consumes "create", it delegates
// to the method for the kind of object that is created.
//
// The kind is the word after "create". The lexer exposes only its current
// token, so the kind is not known until "create" has been consumed.
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

// fieldSpec is the definition of one field in a create table statement,
// before it is added to a schema. The length is an int, as a schema expects,
// and is unused for an int field.
type fieldSpec struct {
	name      string
	fieldType recordmanager.FieldType
	length    int
}

// createTable parses the rest of a create table statement, starting at the
// "table" keyword after "create".
//
// The fields are added to the schema only after the whole statement has been
// read. As with an insert, a statement with more than one error is then
// reported for its syntax first.
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

// fieldDef parses a field name and its type: int, or varchar with a length in
// parentheses.
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

// createView parses the rest of a create view statement, starting at the
// "view" keyword after "create".
//
// The query is the last part of the statement. The check in Query that no
// token follows the query therefore also checks that no token follows the
// statement.
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

// createIndex parses the rest of a create index statement, starting at the
// "index" keyword after "create".
//
// An index is on one field. A second field appears where the closing
// parenthesis is expected, and is reported as an error.
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

// optionalWhere parses a where clause if one is present, and returns its
// predicate. Without a where clause it returns a predicate with no terms,
// which every record satisfies. A statement without a where clause therefore
// needs no special case.
func (p *Parser) optionalWhere() (query.Predicate, error) {
	if !p.lex.MatchKeyword("where") {
		return query.NewPredicate(), nil
	}

	if err := p.lex.EatKeyword("where"); err != nil {
		return query.Predicate{}, err
	}

	return p.predicate()
}

// end returns ErrBadSyntax unless the statement just parsed is the whole
// input.
func (p *Parser) end() error {
	if !p.lex.MatchEnd() {
		return p.lex.unexpected("end of input")
	}

	return nil
}

// idList parses one or more names separated by commas.
//
// The fields of a select list and the tables of a from clause both have this
// form, so one method parses both.
func (p *Parser) idList() ([]string, error) {
	return commaSeparated(p, p.lex.EatID)
}

// constList parses one or more constants separated by commas.
func (p *Parser) constList() ([]query.Constant, error) {
	return commaSeparated(p, p.constant)
}

// commaSeparated parses one or more items separated by commas, using item for
// each, and returns them in the order written.
//
// It is a function and not a method, because a method cannot have a type
// parameter. A list ends at the first item that is not followed by a comma. A
// trailing comma is therefore followed by an item that item fails to parse.
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

// predicate parses one or more terms separated by "and".
//
// The terms are collected in a loop and combined into one predicate at the
// end. The alternative is to parse the text after each "and" as a separate
// predicate and conjoin it. That would build a predicate for every term only
// to extract its terms again.
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

// term parses two expressions separated by an equals sign.
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
