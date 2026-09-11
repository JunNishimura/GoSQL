package metadatamanager

import (
	"errors"
	"fmt"
	"unicode/utf8"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNameTooLong reports a table or field name of more characters than the
// catalogs were built to hold.
var ErrNameTooLong = errors.New("name too long")

// ErrTableNotFound reports a table the catalogs have no row for.
var ErrTableNotFound = errors.New("table not found")

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

// CreateTable writes a table's definition into the catalogs: one row naming the
// table and giving the width of its records, and one row for each of its fields.
//
// The layout is worked out here and written down, rather than left to be worked
// out again when the table is read back. That is what makes the offsets saved
// here the ones the table's records are written to for as long as the table
// exists.
func (tm *TableManager) CreateTable(tx *transaction.Transaction, tableName string, schema *recordmanager.Schema) error {
	if err := checkNames(tableName, schema); err != nil {
		return err
	}

	layout := recordmanager.NewLayout(schema)

	if err := tm.recordTable(tx, tableName, layout); err != nil {
		return err
	}

	return tm.recordFields(tx, tableName, layout)
}

// CreateCatalogTables writes the two catalogs into themselves, which is what
// makes them tables the rest of the database can read like any other.
//
// It is separate from NewTableManager so that whether a database is new is the
// caller's to know rather than a flag handed down to a constructor. Calling it
// on a database that already has catalogs would describe them a second time.
func (tm *TableManager) CreateCatalogTables(tx *transaction.Transaction) error {
	if err := tm.CreateTable(tx, tableCatalogName, tm.tableCatalogLayout.Schema()); err != nil {
		return err
	}

	return tm.CreateTable(tx, fieldCatalogName, tm.fieldCatalogLayout.Schema())
}

// GetLayout reads a table's definition back out of the catalogs.
//
// The layout is rebuilt from what was written down rather than worked out again
// from the schema, so that the offsets it gives are the ones the table's
// records were written to. A version of this code that laid fields out
// differently would otherwise read every record of every older table at the
// wrong place, and say nothing about it.
func (tm *TableManager) GetLayout(tx *transaction.Transaction, tableName string) (*recordmanager.Layout, error) {
	slotSize, err := tm.readSlotSize(tx, tableName)
	if err != nil {
		return nil, err
	}

	schema, offsets, err := tm.readFields(tx, tableName)
	if err != nil {
		return nil, err
	}

	return recordmanager.NewLayoutFromCatalog(schema, offsets, slotSize)
}

// readSlotSize finds the table catalog's row for this table and returns the
// slot size on it. A table with no row there is one the database does not have.
func (tm *TableManager) readSlotSize(tx *transaction.Transaction, tableName string) (int, error) {
	ts, err := recordmanager.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
	if err != nil {
		return 0, err
	}
	defer ts.Close()

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return 0, err
		}
		if !hasNext {
			return 0, fmt.Errorf("read the layout of table %q: %w", tableName, ErrTableNotFound)
		}

		name, err := ts.GetString(tableNameField)
		if err != nil {
			return 0, err
		}
		if name != tableName {
			continue
		}

		slotSize, err := ts.GetInt(slotSizeField)
		if err != nil {
			return 0, err
		}

		return int(slotSize), nil
	}
}

// readFields walks the whole field catalog and collects the rows belonging to
// this table, as a schema and the offsets that go with it.
//
// Every row is looked at, because the rows of one table are not kept together:
// a row is written wherever the field catalog has a free slot, which for a
// table created after others is wherever their deleted rows left room.
//
// The fields come out in the order their rows were written, which is the order
// the schema listed them when the table was created. That order is part of the
// record format, so a layout that had them in another one would not be the same
// layout even with the same offsets.
func (tm *TableManager) readFields(tx *transaction.Transaction, tableName string) (*recordmanager.Schema, map[string]int, error) {
	ts, err := recordmanager.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
	if err != nil {
		return nil, nil, err
	}
	defer ts.Close()

	schema := recordmanager.NewSchema()
	offsets := map[string]int{}

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return nil, nil, err
		}
		if !hasNext {
			return schema, offsets, nil
		}

		name, err := ts.GetString(tableNameField)
		if err != nil {
			return nil, nil, err
		}
		if name != tableName {
			continue
		}

		fieldName, err := ts.GetString(fieldNameField)
		if err != nil {
			return nil, nil, err
		}
		fieldType, err := ts.GetInt(fieldTypeField)
		if err != nil {
			return nil, nil, err
		}
		length, err := ts.GetInt(fieldLengthField)
		if err != nil {
			return nil, nil, err
		}
		offset, err := ts.GetInt(fieldOffsetField)
		if err != nil {
			return nil, nil, err
		}

		if err := schema.AddField(fieldName, recordmanager.FieldType(fieldType), int(length)); err != nil {
			return nil, nil, fmt.Errorf("rebuild the schema of table %q: %w", tableName, err)
		}
		offsets[fieldName] = int(offset)
	}
}

// checkNames refuses a table or field name the catalogs cannot hold.
//
// Every name is looked at before any of them is written, so that a refusal
// leaves the catalogs as they were. A record page would refuse the same name on
// its own, but only once it reached the row carrying it, and by then the rows
// ahead of it describe a table the catalogs half know about, which nothing
// later has any reason to clean up.
//
// The count is of characters because that is what the catalogs' varchar fields
// are measured in.
func checkNames(tableName string, schema *recordmanager.Schema) error {
	if count := utf8.RuneCountInString(tableName); count > maxNameLength {
		return fmt.Errorf("create a table named %q, which is %d characters and the catalogs hold %d: %w", tableName, count, maxNameLength, ErrNameTooLong)
	}

	for _, fieldName := range schema.Fields() {
		if count := utf8.RuneCountInString(fieldName); count > maxNameLength {
			return fmt.Errorf("create a field named %q, which is %d characters and the catalogs hold %d: %w", fieldName, count, maxNameLength, ErrNameTooLong)
		}
	}

	return nil
}

// recordTable writes the table catalog's row for this table.
func (tm *TableManager) recordTable(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) error {
	ts, err := recordmanager.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
	if err != nil {
		return err
	}
	defer ts.Close()

	if err := ts.MoveToNewRecord(); err != nil {
		return err
	}
	if err := ts.SetString(tableNameField, tableName); err != nil {
		return err
	}

	return ts.SetInt(slotSizeField, int32(layout.SlotSize()))
}

// recordFields writes the field catalog's rows for this table, one per field,
// in the order the schema lists them.
func (tm *TableManager) recordFields(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) error {
	ts, err := recordmanager.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
	if err != nil {
		return err
	}
	defer ts.Close()

	schema := layout.Schema()
	for _, fieldName := range schema.Fields() {
		// None of these three can fail: the field came from the schema's own
		// list, and the layout was built from that schema. They are returned
		// rather than dropped so that this stays true if they grow another
		// reason to refuse a field.
		fieldType, err := schema.Type(fieldName)
		if err != nil {
			return err
		}
		length, err := schema.Length(fieldName)
		if err != nil {
			return err
		}
		offset, err := layout.Offset(fieldName)
		if err != nil {
			return err
		}

		if err := ts.MoveToNewRecord(); err != nil {
			return err
		}
		if err := ts.SetString(tableNameField, tableName); err != nil {
			return err
		}
		if err := ts.SetString(fieldNameField, fieldName); err != nil {
			return err
		}
		if err := ts.SetInt(fieldTypeField, int32(fieldType)); err != nil {
			return err
		}
		if err := ts.SetInt(fieldLengthField, int32(length)); err != nil {
			return err
		}
		if err := ts.SetInt(fieldOffsetField, int32(offset)); err != nil {
			return err
		}
	}

	return nil
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
