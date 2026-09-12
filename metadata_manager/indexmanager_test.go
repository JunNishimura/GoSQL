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

// The index catalog's own width. Its three fields are all names, so it is the
// widest of the catalogs after the view catalog.
//
//	index_catalog  4 + (4 + 64) + (4 + 64) + (4 + 64) = 208 bytes
const indexCatalogSlotSize = 208

// newTestIndexManager builds an index manager over a database that already
// holds the catalogs, which is where every index starts from.
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

	// The layout is read back rather than the rows, because reading it back is
	// what every later call does first: the index catalog is an ordinary table,
	// so writing or reading an index means opening a scan on it.
	//
	// Two of its three fields are the ones the other catalogs already name. An
	// index catalog row says that an index of some name is on a field of a
	// table, and the field and the table are named here the way they are named
	// everywhere else.
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

// The second index the tests need, on the other field of the test table. Two
// indexes of one table is the ordinary case: a table is indexed on whichever
// of its fields queries ask about.
const (
	otherIndexName      = "name_index"
	otherIndexFieldName = "name"
)

// newTestIndexManagerWithCatalog builds an index manager whose catalog exists,
// which is where creating an index starts from.
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

	// A table may be indexed on more than one of its fields, so a second index
	// has to sit alongside the first rather than replace it.
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
}

// All three of what an index is registered under are names, and the catalog
// holds each of them to the same length. The index's own name is held to it
// twice over, since an index is kept as a table and that table is named by it.
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

// The other table the read tests need, so that asking for one table's indexes
// is a matter of picking rows out rather than reading every row there is.
const (
	indexedOtherTableName = "other_table"
	otherTableIndexName   = "title_index"
	otherTableFieldName   = "title"
)

// newTestIndexManagerWithIndexes builds a database holding two indexed tables:
// the test table, indexed on both of its fields, and another table indexed on
// its one field.
//
// The test table is given records, since what an index costs is worked out from
// the measurements of the table it is on rather than from the index itself.
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

	// The catalog holds every table's indexes together, so reading one table's
	// means picking its rows out of all of them.
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

	// A table with no index is the usual case, not a fault: most tables have
	// none, and a planner asks about every table it reads.
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

	// What comes back has to be costed from the table it is on. Nothing about
	// an index says how many records it holds; that is the table's to say, and
	// the whole reason this manager needs the statistics manager.
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
