package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The catalogs are ordinary tables, so they are stored under names of their
// own, alongside the tables they describe.
const (
	tableCatalogName = "table_catalog"
	fieldCatalogName = "field_catalog"
)

// The fields of the two catalogs.
//
// One row of the table catalog is one table, and one row of the field catalog
// is one field of one table. That is why there are two catalogs rather than
// one: a table has any number of fields, and a record has a fixed number of
// them, so the fields of a table cannot be written into the row that names it.
const (
	// tableNameField is the table a row is about. Both catalogs carry it, and
	// it is what ties a table's fields back to the table.
	tableNameField = "table_name"
	// slotSizeField is how wide one slot of that table is.
	slotSizeField = "slot_size"
	// fieldNameField is the field a field catalog row is about.
	fieldNameField = "field_name"
	// fieldTypeField is that field's type, held as the number a FieldType is.
	fieldTypeField = "field_type"
	// fieldLengthField is that field's character limit, and is 0 for an int,
	// whose width does not vary.
	fieldLengthField = "field_length"
	// fieldOffsetField is where that field sits within a slot. It is written
	// down rather than worked out again on every read, so that records already
	// on disk keep being read at the offsets they were written to.
	fieldOffsetField = "field_offset"
)

// maxNameLength is the longest table or field name the catalogs can hold.
//
// The catalogs are tables, so a name in them is a varchar, and a varchar takes
// up the room its limit allows rather than the room its value needs. Raising
// this is therefore paid for by every catalog record, whether or not any name
// is that long.
const maxNameLength = 16

// TableManager is what the database knows about its own tables: which tables
// exist, what fields each of them has, and where those fields sit in a record.
//
// It keeps that knowledge in two tables of its own, the table catalog and the
// field catalog, so that a table definition survives a restart the same way any
// other record does. Storing catalogs as tables means they are read and written
// through a TableScan, under a transaction, and are recovered by the same log.
//
// The two layouts here are the catalogs' own. They are held rather than rebuilt
// per call because reading any table's definition starts by scanning a catalog,
// and a scan cannot begin without the layout of the table it scans. Those two
// layouts are the point where that regress stops: they are built in code from
// schemas fixed by this package, not looked up from a catalog.
type TableManager struct {
	tableCatalogLayout *recordmanager.Layout
	fieldCatalogLayout *recordmanager.Layout
}

// NewTableManager builds the layouts of the two catalogs.
//
// It touches no storage, so it neither takes a transaction nor says whether the
// database is new. Creating the catalogs is CreateCatalogTables, which the
// caller invokes on a database that has none yet.
func NewTableManager() (*TableManager, error) {
	tableCatalogSchema, err := newTableCatalogSchema()
	if err != nil {
		return nil, err
	}

	fieldCatalogSchema, err := newFieldCatalogSchema()
	if err != nil {
		return nil, err
	}

	return &TableManager{
		tableCatalogLayout: recordmanager.NewLayout(tableCatalogSchema),
		fieldCatalogLayout: recordmanager.NewLayout(fieldCatalogSchema),
	}, nil
}

// newTableCatalogSchema is the schema of the table catalog: one row per table,
// naming it and saying how wide its slots are.
//
// Adding a field cannot fail here, since the names are fixed by this package
// and no two of them are the same. The error is returned rather than dropped so
// that this stays true if AddField grows another reason to refuse a field.
func newTableCatalogSchema() (*recordmanager.Schema, error) {
	schema := recordmanager.NewSchema()

	if err := schema.AddStringField(tableNameField, maxNameLength); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(slotSizeField); err != nil {
		return nil, err
	}

	return schema, nil
}

// newFieldCatalogSchema is the schema of the field catalog: one row per field
// of per table, naming the table it belongs to and describing the field.
//
// Adding a field cannot fail here, for the reason newTableCatalogSchema gives.
func newFieldCatalogSchema() (*recordmanager.Schema, error) {
	schema := recordmanager.NewSchema()

	if err := schema.AddStringField(tableNameField, maxNameLength); err != nil {
		return nil, err
	}
	if err := schema.AddStringField(fieldNameField, maxNameLength); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(fieldTypeField); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(fieldLengthField); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(fieldOffsetField); err != nil {
		return nil, err
	}

	return schema, nil
}
