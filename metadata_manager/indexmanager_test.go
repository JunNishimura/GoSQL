package metadatamanager

import (
	"slices"
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
