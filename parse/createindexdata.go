package parse

var _ UpdateCommand = CreateIndexData{}

// CreateIndexData is the content of a create index statement: an index with
// the given name on one field of a table.
type CreateIndexData struct {
	indexName string
	tableName string
	fieldName string
}

func (CreateIndexData) isUpdateCommand() {}

// IndexName returns the name of the index to create.
func (d CreateIndexData) IndexName() string {
	return d.indexName
}

// TableName returns the table whose field is indexed.
func (d CreateIndexData) TableName() string {
	return d.tableName
}

// FieldName returns the indexed field.
func (d CreateIndexData) FieldName() string {
	return d.fieldName
}
