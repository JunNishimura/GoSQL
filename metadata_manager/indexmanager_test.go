package metadatamanager

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The slot size of the index catalog. Its three fields are all names, so it
// is the largest catalog after the view catalog.
//
//	index_catalog  4 + (4 + 64) + (4 + 64) + (4 + 64) = 208 bytes
const indexCatalogSlotSize = 208

// newTestIndexManager builds an index manager over a database that already
// has the catalogs, which every index operation requires.
func newTestIndexManager(t *testing.T, tx *transaction.Transaction) *IndexManager {
	t.Helper()

	tm := mustNewTableManager(t)
	if err := tm.CreateCatalogTables(tx); err != nil {
		t.Fatalf("CreateCatalogTables() error = %v", err)
	}

	return NewIndexManager(tm, NewStatisticsManager(tm))
}

func TestNewIndexManager(t *testing.T) {
	t.Run("it carries the table manager and the statistics manager it was made from", func(t *testing.T) {
		tableManager := mustNewTableManager(t)
		statisticsManager := NewStatisticsManager(tableManager)

		im := NewIndexManager(tableManager, statisticsManager)

		if im.tableManager != tableManager {
			t.Errorf("tableManager = %p, want %p", im.tableManager, tableManager)
		}
		if im.statisticsManager != statisticsManager {
			t.Errorf("statisticsManager = %p, want %p", im.statisticsManager, statisticsManager)
		}
	})
}

func TestIndexManagerCreateCatalogTable(t *testing.T) {
	t.Run("when the index catalog is created, then the table catalog holds a row for it", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManager(t, tx)

		if err := im.CreateCatalogTable(tx); err != nil {
			t.Fatalf("CreateCatalogTable() error = %v", err)
		}

		rows := readTableCatalog(t, tx, im.tableManager)
		want := tableCatalogRow{
			tableName: indexCatalogName,
			slotSize:  indexCatalogSlotSize,
		}
		if !slices.Contains(rows, want) {
			t.Errorf("the table catalog holds %+v, want one of them to be %+v", rows, want)
		}
	})

	// The test reads the layout and not the rows, because every later call
	// starts by reading the layout. The index catalog is an ordinary table,
	// so reading or writing an index opens a scan of it.
	//
	// Two of its three fields have names that the other catalogs already
	// define. A row of the index catalog states that an index with some name
	// is on a field of a table, and the field and the table use the same
	// field names as everywhere else.
	t.Run("when the index catalog is created, then its layout can be read back through the table manager", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManager(t, tx)

		if err := im.CreateCatalogTable(tx); err != nil {
			t.Fatalf("CreateCatalogTable() error = %v", err)
		}

		layout, err := im.tableManager.GetLayout(tx, indexCatalogName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		assertLayout(t, layout, layoutDescription{
			slotSize: indexCatalogSlotSize,
			fields: []layoutField{
				{
					name:      indexNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
					offset:    4,
				},
				{
					name:      tableNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
					offset:    72,
				},
				{
					name:      fieldNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
					offset:    140,
				},
			},
		})
	})
}

// The second index that the tests need, on the other field of the test table.
// Two indexes on one table is the normal case: a table is indexed on the
// fields that queries search by.
const (
	otherIndexName      = "name_index"
	otherIndexFieldName = "name"
)

// newTestIndexManagerWithCatalog builds an index manager whose catalog
// exists, which the creation of an index requires.
func newTestIndexManagerWithCatalog(t *testing.T, tx *transaction.Transaction) *IndexManager {
	t.Helper()

	im := newTestIndexManager(t, tx)
	if err := im.CreateCatalogTable(tx); err != nil {
		t.Fatalf("CreateCatalogTable() error = %v", err)
	}

	return im
}

func TestIndexManagerCreateIndex(t *testing.T) {
	t.Run("when an index is created, then the index catalog holds its name, the table it is on and the field", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithCatalog(t, tx)

		if err := im.CreateIndex(tx, testIndexName, testTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex() error = %v", err)
		}

		want := []indexCatalogRow{
			{
				indexName: testIndexName,
				tableName: testTableName,
				fieldName: testIndexFieldName,
			},
		}
		if got := readIndexCatalog(t, tx, im); !slices.Equal(got, want) {
			t.Errorf("the index catalog holds %+v, want %+v", got, want)
		}
	})

	// A table can be indexed on more than one field, so a second index must
	// be added to the catalog and must not replace the first.
	t.Run("given an index the catalog already holds, when a second one on the same table is created, then the catalog holds both", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithCatalog(t, tx)

		if err := im.CreateIndex(tx, testIndexName, testTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", testIndexName, err)
		}
		if err := im.CreateIndex(tx, otherIndexName, testTableName, otherIndexFieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", otherIndexName, err)
		}

		want := []indexCatalogRow{
			{
				indexName: testIndexName,
				tableName: testTableName,
				fieldName: testIndexFieldName,
			},
			{
				indexName: otherIndexName,
				tableName: testTableName,
				fieldName: otherIndexFieldName,
			},
		}
		if got := readIndexCatalog(t, tx, im); !slices.Equal(got, want) {
			t.Errorf("the index catalog holds %+v, want %+v", got, want)
		}
	})

	t.Run("given an index on a field of a table, when another index on the same field of the same table is created, then it reports ErrFieldAlreadyIndexed and the catalog holds only the first", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithCatalog(t, tx)

		if err := im.CreateIndex(tx, testIndexName, testTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", testIndexName, err)
		}

		err := im.CreateIndex(tx, otherIndexName, testTableName, testIndexFieldName)
		if !errors.Is(err, ErrFieldAlreadyIndexed) {
			t.Errorf("CreateIndex(%q) error = %v, want %v", otherIndexName, err, ErrFieldAlreadyIndexed)
		}

		want := []indexCatalogRow{
			{
				indexName: testIndexName,
				tableName: testTableName,
				fieldName: testIndexFieldName,
			},
		}
		if got := readIndexCatalog(t, tx, im); !slices.Equal(got, want) {
			t.Errorf("the index catalog holds %+v, want %+v", got, want)
		}
	})

	// The field has the same name but belongs to another table, so it is a
	// different field. Its index does not affect the first index.
	t.Run("given an index on a field of a table, when an index on a field of the same name of another table is created, then the catalog holds both", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithCatalog(t, tx)

		const otherTableName = "other_table"
		if err := im.CreateIndex(tx, testIndexName, testTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", testIndexName, err)
		}
		if err := im.CreateIndex(tx, otherIndexName, otherTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", otherIndexName, err)
		}

		want := []indexCatalogRow{
			{
				indexName: testIndexName,
				tableName: testTableName,
				fieldName: testIndexFieldName,
			},
			{
				indexName: otherIndexName,
				tableName: otherTableName,
				fieldName: testIndexFieldName,
			},
		}
		if got := readIndexCatalog(t, tx, im); !slices.Equal(got, want) {
			t.Errorf("the index catalog holds %+v, want %+v", got, want)
		}
	})
}

// An index is registered with three names, and the catalog limits each of
// them to the same length. The index name must fit in two catalogs, because
// an index is stored as a table with the name of the index.
func TestIndexManagerRejectsANameLongerThanTheCatalogHolds(t *testing.T) {
	overlongName := strings.Repeat("a", maxNameLength+1)

	tests := []struct {
		name      string
		indexName string
		tableName string
		fieldName string
	}{
		{
			name:      "given an index name one character over what the catalog holds, when the index is created, then it reports ErrNameTooLong",
			indexName: overlongName,
			tableName: testTableName,
			fieldName: testIndexFieldName,
		},
		{
			name:      "given a table name one character over what the catalog holds, when an index on it is created, then it reports ErrNameTooLong",
			indexName: testIndexName,
			tableName: overlongName,
			fieldName: testIndexFieldName,
		},
		{
			name:      "given a field name one character over what the catalog holds, when an index on it is created, then it reports ErrNameTooLong",
			indexName: testIndexName,
			tableName: testTableName,
			fieldName: overlongName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			im := newTestIndexManagerWithCatalog(t, tx)

			err := im.CreateIndex(tx, tt.indexName, tt.tableName, tt.fieldName)
			if !errors.Is(err, ErrNameTooLong) {
				t.Errorf("error = %v, want %v", err, ErrNameTooLong)
			}

			if rows := readIndexCatalog(t, tx, im); len(rows) != 0 {
				t.Errorf("the index catalog holds %+v, want nothing: the refused index was written anyway", rows)
			}
		})
	}
}

// The other table that the read tests need. With it, reading the indexes of
// one table requires selecting rows, not reading every row.
const (
	indexedOtherTableName = "other_table"
	otherTableIndexName   = "title_index"
	otherTableFieldName   = "title"
)

// newTestIndexManagerWithIndexes builds a database with two indexed tables:
// the test table, indexed on both its fields, and another table, indexed on
// its only field.
//
// The test table gets records, because the cost of an index is estimated
// from the statistics of its table and not from the index itself.
func newTestIndexManagerWithIndexes(t *testing.T, tx *transaction.Transaction) *IndexManager {
	t.Helper()

	im := newTestIndexManagerWithCatalog(t, tx)

	if err := im.tableManager.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", testTableName, err)
	}
	insertTestRecords(t, tx, im.tableManager, testTableRecordCount)

	otherSchema := recordmanager.NewSchema()
	mustAddStringField(t, otherSchema, otherTableFieldName, 30)
	if err := im.tableManager.CreateTable(tx, indexedOtherTableName, otherSchema); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", indexedOtherTableName, err)
	}

	for _, index := range []struct {
		indexName string
		tableName string
		fieldName string
	}{
		{testIndexName, testTableName, testIndexFieldName},
		{otherIndexName, testTableName, otherIndexFieldName},
		{otherTableIndexName, indexedOtherTableName, otherTableFieldName},
	} {
		if err := im.CreateIndex(tx, index.indexName, index.tableName, index.fieldName); err != nil {
			t.Fatalf("CreateIndex(%q) error = %v", index.indexName, err)
		}
	}

	return im
}

func TestIndexManagerGetIndexInfo(t *testing.T) {
	t.Run("given a table indexed on two of its fields, when its indexes are asked for, then there is one for each field, filed under that field's name", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithIndexes(t, tx)

		indexes, err := im.GetIndexInfo(tx, testTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo() error = %v", err)
		}

		want := map[string]string{
			testIndexFieldName:  testIndexName,
			otherIndexFieldName: otherIndexName,
		}

		got := map[string]string{}
		for fieldName, info := range indexes {
			got[fieldName] = info.indexName
		}

		if !maps.Equal(got, want) {
			t.Errorf("the indexes are %v, want %v", got, want)
		}
	})

	// The catalog holds the indexes of all tables together, so reading the
	// indexes of one table means selecting its rows from all rows.
	t.Run("given another table that is indexed too, when one table's indexes are asked for, then the other table's are not among them", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithIndexes(t, tx)

		indexes, err := im.GetIndexInfo(tx, indexedOtherTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo() error = %v", err)
		}

		if len(indexes) != 1 {
			t.Fatalf("the indexes are %v, want the one on %q", indexes, otherTableFieldName)
		}
		info, ok := indexes[otherTableFieldName]
		if !ok {
			t.Fatalf("the indexes are %v, want one filed under %q", indexes, otherTableFieldName)
		}
		if info.indexName != otherTableIndexName {
			t.Errorf("indexName = %q, want %q", info.indexName, otherTableIndexName)
		}
	})

	// A table with no index is the usual case and not an error: most tables
	// have none, and a planner asks about every table it reads.
	t.Run("given a table nothing indexes, when its indexes are asked for, then none come back and no error", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithCatalog(t, tx)

		if err := im.tableManager.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}

		indexes, err := im.GetIndexInfo(tx, testTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo() error = %v, want nil: a table with no index is not a failure", err)
		}
		if len(indexes) != 0 {
			t.Errorf("the indexes are %v, want none", indexes)
		}
	})

	// The returned info must be costed from its table. An index does not
	// record how many records it holds. The table statistics provide that,
	// which is why this manager needs the statistics manager.
	//
	//	records                  9
	//	distinct values          1 + 9/3 = 4
	//	records for one search   9/4 = 2
	t.Run("given a table with records, when its indexes are asked for, then what a search through one would give back is worked out from that table's measurements", func(t *testing.T) {
		tx := newTestTransaction(t)
		im := newTestIndexManagerWithIndexes(t, tx)

		indexes, err := im.GetIndexInfo(tx, testTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo() error = %v", err)
		}

		info, ok := indexes[testIndexFieldName]
		if !ok {
			t.Fatalf("the indexes are %v, want one filed under %q", indexes, testIndexFieldName)
		}

		distinct := 1 + testTableRecordCount/distinctValuesDivisor
		if got, want := info.RecordsOutput(), testTableRecordCount/distinct; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}
