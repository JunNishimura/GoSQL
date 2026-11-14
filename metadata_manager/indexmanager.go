package metadatamanager

import (
	"errors"
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The index catalog is a table like the other catalogs. It has one row per
// index.
const indexCatalogName = "index_catalog"

// indexNameField holds the name of an index. It is the only field name that
// this package defines for the index catalog. A catalog row states that an
// index is on a field of a table, and the table and field names use the same
// field names as in every other catalog.
const indexNameField = "index_name"

// IndexManager manages the metadata of the indexes of the database: the table
// and field of each index, and the estimated cost of using it.
//
// The first part is stored in a catalog of its own, which is an ordinary
// table created through the table manager. The second part is not stored. The
// cost of an index depends on the number of records in the indexed table and
// on the number of distinct values of the indexed field. The statistics
// manager provides both.
//
// This is the first manager in this package that depends on another manager.
// The table and view managers answer from their own catalogs. The index
// manager reads its catalog to learn that an index exists, and then asks the
// statistics manager for the data needed to estimate its cost. Neither
// manager could answer alone: the catalog knows the index, the statistics
// know the table, and a cost estimate needs both.
//
// The manager holds no layout, for the same reason as the view manager: the
// index catalog is an ordinary table, so its layout is read from the table
// catalog. The manager has no lock either, because it holds no state that two
// transactions could modify concurrently.
type IndexManager struct {
	// tableManager creates and reads the index catalog, and provides the
	// schema of an indexed table.
	tableManager *TableManager
	// statisticsManager provides the statistics of the indexed table, which
	// the cost of an index is estimated from.
	statisticsManager *StatisticsManager
}

// NewIndexManager returns an index manager that stores its catalog through
// tableManager and estimates costs through statisticsManager.
//
// It accesses no storage, so it takes no transaction and cannot fail. The
// index catalog is created by a separate call, which the caller makes for a
// database that does not have one yet.
func NewIndexManager(tableManager *TableManager, statisticsManager *StatisticsManager) *IndexManager {
	return &IndexManager{
		tableManager:      tableManager,
		statisticsManager: statisticsManager,
	}
}

// CreateCatalogTable creates the index catalog. It is an ordinary table and
// is created like any other table.
//
// It is separate from NewIndexManager for the same reason as the other
// catalog creation methods: only the caller knows whether the database is
// new. On a database that already has an index catalog, this call would write
// a second row that describes the catalog.
func (im *IndexManager) CreateCatalogTable(tx *transaction.Transaction) error {
	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here, because this package defines the three
	// names and they are all different. The error is still returned, so that
	// the code stays correct if AddField gains another failure case.
	if err := schema.AddStringField(indexNameField, maxNameLength); err != nil {
		return err
	}
	if err := schema.AddStringField(tableNameField, maxNameLength); err != nil {
		return err
	}
	if err := schema.AddStringField(fieldNameField, maxNameLength); err != nil {
		return err
	}

	return im.tableManager.CreateTable(tx, indexCatalogName, schema)
}

// ErrFieldAlreadyIndexed is returned when an index is created on a field that
// already has an index.
//
// The indexes of a table are looked up by field, with one index per field. A
// second index on the same field would never be found. No statement would
// maintain it when the table changes, so it would keep the contents that the
// table had when the index was created.
var ErrFieldAlreadyIndexed = errors.New("field already indexed")

// CreateIndex records that an index named indexName is on fieldName of
// tableName.
//
// The catalog row is the whole registration of an index. Building the index
// and maintaining it when records are written are not tasks of this layer.
// This layer only records that the index exists, so that a planner that asks
// for the indexes of a table finds it.
//
// A second index on a field that already has one is rejected with
// ErrFieldAlreadyIndexed, before anything is written.
func (im *IndexManager) CreateIndex(tx *transaction.Transaction, indexName string, tableName string, fieldName string) error {
	if err := checkIndexFits(indexName, tableName, fieldName); err != nil {
		return err
	}

	layout, err := im.tableManager.GetLayout(tx, indexCatalogName)
	if err != nil {
		return err
	}

	ts, err := query.NewTableScan(tx, indexCatalogName, layout)
	if err != nil {
		return err
	}
	defer ts.Close()

	if err := refuseIndexedField(ts, tableName, fieldName); err != nil {
		return err
	}

	if err := ts.MoveToNewRecord(); err != nil {
		return err
	}
	if err := ts.SetString(indexNameField, indexName); err != nil {
		return err
	}
	if err := ts.SetString(tableNameField, tableName); err != nil {
		return err
	}

	return ts.SetString(fieldNameField, fieldName)
}

// refuseIndexedField scans the index catalog through ts from its first row.
// It returns ErrFieldAlreadyIndexed if a row states that fieldName of
// tableName already has an index.
//
// The whole catalog is scanned, as in GetIndexInfo, because the rows of one
// table are not adjacent.
func refuseIndexedField(ts *query.TableScan, tableName string, fieldName string) error {
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil || !hasNext {
			return err
		}

		rowTable, err := ts.GetString(tableNameField)
		if err != nil {
			return err
		}
		rowField, err := ts.GetString(fieldNameField)
		if err != nil {
			return err
		}
		if rowTable != tableName || rowField != fieldName {
			continue
		}

		existing, err := ts.GetString(indexNameField)
		if err != nil {
			return err
		}

		return fmt.Errorf("index field %q of table %q, which index %q is already on: %w", fieldName, tableName, existing, ErrFieldAlreadyIndexed)
	}
}

// checkIndexFits returns an error if any of the three names is too long for
// the index catalog.
//
// All three names are checked before the row is written. An error then names
// the value that the caller must change, not the first field that the record
// page happens to write.
//
// The index name must fit in two catalogs. An index is stored as a table with
// the name of the index, so the name is written to the table catalog as well
// as to the index catalog.
func checkIndexFits(indexName string, tableName string, fieldName string) error {
	if err := checkNameFits("index", indexName); err != nil {
		return err
	}
	if err := checkNameFits("table", tableName); err != nil {
		return err
	}

	return checkNameFits("field", fieldName)
}

// GetIndexInfo returns the indexes on tableName as a map from the indexed
// field to its index. A field has at most one index.
//
// A table with no index is not an error. It is the usual case: most tables
// have none, and a planner asks about every table it reads. The result is
// then an empty map. The result is also an empty map for a table that does
// not exist, because neither has a row in the index catalog.
//
// The whole catalog is scanned. The catalog holds the indexes of all tables,
// and the rows of one table are not adjacent.
//
// The schema and statistics of the indexed table are read once and used for
// every index on it. They cannot differ between two rows for the same table,
// and reading the statistics can require a scan of the table.
func (im *IndexManager) GetIndexInfo(tx *transaction.Transaction, tableName string) (map[string]*IndexInfo, error) {
	layout, err := im.tableManager.GetLayout(tx, indexCatalogName)
	if err != nil {
		return nil, err
	}

	ts, err := query.NewTableScan(tx, indexCatalogName, layout)
	if err != nil {
		return nil, err
	}
	defer ts.Close()

	indexes := map[string]*IndexInfo{}

	// These stay nil until the first row for this table is found, so that a
	// table with no index needs no read of the table. A nil schema means that
	// they have not been read yet. Both are read together, so the schema is
	// never set without the statistics.
	var tableSchema *recordmanager.Schema
	var tableStatistics *TableStatistics

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return nil, err
		}
		if !hasNext {
			return indexes, nil
		}

		name, err := ts.GetString(tableNameField)
		if err != nil {
			return nil, err
		}
		if name != tableName {
			continue
		}

		if tableSchema == nil {
			tableSchema, tableStatistics, err = im.describeIndexedTable(tx, tableName)
			if err != nil {
				return nil, err
			}
		}

		indexName, err := ts.GetString(indexNameField)
		if err != nil {
			return nil, err
		}
		fieldName, err := ts.GetString(fieldNameField)
		if err != nil {
			return nil, err
		}

		info, err := NewIndexInfo(tx, indexName, fieldName, tableSchema, tableStatistics)
		if err != nil {
			return nil, err
		}

		indexes[fieldName] = info
	}
}

// describeIndexedTable reads the two inputs that an index on tableName is
// described and costed from: the schema of the table and its statistics.
//
// Both describe the table, not the index. The index catalog only records that
// an index exists. How much of the table an index search saves depends on the
// table, which is why this manager holds a statistics manager as well as a
// table manager.
//
// This call can trigger a refresh of the statistics, which scans every table
// in the database, while the caller has a scan of the index catalog open.
// This uses more buffers but does not affect correctness: this manager has no
// lock of its own, so no lock of this package is held during the refresh.
func (im *IndexManager) describeIndexedTable(tx *transaction.Transaction, tableName string) (*recordmanager.Schema, *TableStatistics, error) {
	layout, err := im.tableManager.GetLayout(tx, tableName)
	if err != nil {
		return nil, nil, err
	}

	statistics, err := im.statisticsManager.GetStatistics(tx, tableName)
	if err != nil {
		return nil, nil, err
	}

	return layout.Schema(), statistics, nil
}
