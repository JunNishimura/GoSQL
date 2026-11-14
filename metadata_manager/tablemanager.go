package metadatamanager

import (
	"errors"
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrTableNotFound is returned for a table that has no row in the catalogs.
var ErrTableNotFound = errors.New("table not found")

// The catalogs are ordinary tables. They are stored under their own names,
// together with the tables that they describe.
const (
	tableCatalogName = "table_catalog"
	fieldCatalogName = "field_catalog"
)

// The fields of the two catalogs.
//
// One row of the table catalog describes one table, and one row of the field
// catalog describes one field of one table. Two catalogs are needed because a
// table has a variable number of fields and a record has a fixed number. The
// fields of a table therefore cannot be stored in the row of the table.
const (
	// tableNameField holds the name of the table that a row describes. Both
	// catalogs have this field, and it links the fields of a table to the
	// table.
	tableNameField = "table_name"
	// slotSizeField holds the slot size of that table.
	slotSizeField = "slot_size"
	// fieldNameField holds the name of the field that a field catalog row
	// describes.
	fieldNameField = "field_name"
	// fieldTypeField holds the type of that field, as the numeric value of a
	// FieldType.
	fieldTypeField = "field_type"
	// fieldLengthField holds the character limit of that field. It is 0 for
	// an int, whose size is fixed.
	fieldLengthField = "field_length"
	// fieldOffsetField holds the offset of that field within a slot. The
	// offset is stored and not computed again on each read, so that existing
	// records on disk are always read at the offsets they were written with.
	fieldOffsetField = "field_offset"
)

// TableManager manages the metadata of the tables of the database: which
// tables exist, which fields each table has, and where the fields are located
// in a record.
//
// It stores this metadata in two tables of its own, the table catalog and the
// field catalog. A table definition therefore survives a restart in the same
// way as any other record. Because the catalogs are tables, they are accessed
// through a TableScan in a transaction, and the same log recovers them.
//
// The two layouts in this type are the layouts of the catalogs. They are
// stored and not built on each call. Reading the definition of any table
// starts with a scan of a catalog, and a scan needs the layout of the table
// that it scans. The layouts of the catalogs cannot be read from a catalog
// for that reason. They are built in code from schemas that this package
// defines.
type TableManager struct {
	tableCatalogLayout *recordmanager.Layout
	fieldCatalogLayout *recordmanager.Layout
}

// NewTableManager builds the layouts of the two catalogs.
//
// It accesses no storage. It therefore takes no transaction and does not need
// to know whether the database is new. CreateCatalogTables creates the
// catalogs, and the caller invokes it for a database that has none yet.
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

// CreateTable writes the definition of a table to the catalogs: one row with
// the table name and the slot size, and one row for each field.
//
// The layout is computed here and stored. It is not computed again when the
// table is read back. The stored offsets are therefore the offsets that the
// records of the table are written with for as long as the table exists.
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

// CreateCatalogTables writes the definitions of the two catalogs into the
// catalogs themselves. The rest of the database can then read them like any
// other table.
//
// It is separate from NewTableManager so that the caller decides whether a
// database is new, and no flag has to be passed to a constructor. On a
// database that already has catalogs, this call would write their definitions
// a second time.
func (tm *TableManager) CreateCatalogTables(tx *transaction.Transaction) error {
	if err := tm.CreateTable(tx, tableCatalogName, tm.tableCatalogLayout.Schema()); err != nil {
		return err
	}

	return tm.CreateTable(tx, fieldCatalogName, tm.fieldCatalogLayout.Schema())
}

// GetLayout reads the definition of a table from the catalogs.
//
// The layout is restored from the stored values and not computed again from
// the schema. Its offsets are therefore the offsets that the records of the
// table were written with. Otherwise a version of this code that placed
// fields differently would silently read every record of every older table at
// the wrong offsets.
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

// checkNames returns an error for a table or field name that is too long for
// the catalogs.
//
// Every name is checked before any of them is written, so that an error
// leaves the catalogs unchanged. A record page would reject the same name,
// but only when it writes the row with that name. The rows written before it
// would then describe a table that is only partly in the catalogs, and no
// later code would remove them.
func checkNames(tableName string, schema *recordmanager.Schema) error {
	if err := checkNameFits("table", tableName); err != nil {
		return err
	}

	for _, fieldName := range schema.Fields() {
		if err := checkNameFits("field", fieldName); err != nil {
			return err
		}
	}

	return nil
}

// recordTable writes the row of the table catalog for this table.
func (tm *TableManager) recordTable(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) error {
	ts, err := query.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
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

// recordFields writes the rows of the field catalog for this table, one per
// field, in schema order.
func (tm *TableManager) recordFields(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) error {
	ts, err := query.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
	if err != nil {
		return err
	}
	defer ts.Close()

	schema := layout.Schema()
	for _, fieldName := range schema.Fields() {
		// None of these three calls can fail, because the field name comes
		// from the schema's own list and the layout was built from that
		// schema. The errors are still returned, so that the code stays
		// correct if one of the calls gains another failure case.
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

// readSlotSize finds the row of the table catalog for this table and returns
// its slot size. A table with no row there does not exist in the database.
func (tm *TableManager) readSlotSize(tx *transaction.Transaction, tableName string) (int, error) {
	ts, err := query.NewTableScan(tx, tableCatalogName, tm.tableCatalogLayout)
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

// readFields scans the whole field catalog and collects the rows of this
// table. It returns them as a schema and the matching offsets.
//
// Every row is read, because the rows of one table are not adjacent. A row is
// written to any free slot of the field catalog. For a table created after
// others, these can be slots that deleted rows left free.
//
// The fields are returned in the order in which their rows were written,
// which is the schema order at the time the table was created. That order is
// part of the record format. A layout with the fields in another order would
// be a different layout even with the same offsets.
func (tm *TableManager) readFields(tx *transaction.Transaction, tableName string) (*recordmanager.Schema, map[string]int, error) {
	ts, err := query.NewTableScan(tx, fieldCatalogName, tm.fieldCatalogLayout)
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

// newTableCatalogSchema returns the schema of the table catalog. The catalog
// has one row per table, with the table name and the slot size.
//
// Adding a field cannot fail here, because this package defines the names and
// they are all different. The error is still returned, so that the code stays
// correct if AddField gains another failure case.
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

// newFieldCatalogSchema returns the schema of the field catalog. The catalog
// has one row per field of each table, with the table name and the
// description of the field.
//
// Adding a field cannot fail here, for the same reason as in
// newTableCatalogSchema.
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
