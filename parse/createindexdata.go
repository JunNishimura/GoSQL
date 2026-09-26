package parse

var _ UpdateCommand = CreateIndexData{}

// CreateIndexData is what a create index statement asks for: an index of the
// given name on one field of a table.
type CreateIndexData struct {
	indexName string
	tableName string
	fieldName string
}

func (CreateIndexData) isUpdateCommand() {}

// IndexName is the index to create.
func (d CreateIndexData) IndexName() string {
	return d.indexName
}

// TableName is the table whose field is indexed.
func (d CreateIndexData) TableName() string {
	return d.tableName
}

// FieldName is the field the index is on.
func (d CreateIndexData) FieldName() string {
	return d.fieldName
}
