package metadatamanager

import (
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// newTestMetadataManager builds a metadata manager over a database whose
// catalogs already exist, which is the state every call below starts from.
func newTestMetadataManager(t *testing.T, tx *transaction.Transaction) *MetadataManager {
	t.Helper()

	mm, err := NewMetadataManager()
	if err != nil {
		t.Fatalf("NewMetadataManager() error = %v", err)
	}
	if err := mm.CreateCatalogTables(tx); err != nil {
		t.Fatalf("CreateCatalogTables() error = %v", err)
	}

	return mm
}

// The four managers have to be built over one table manager rather than one
// each. A table manager holds no state, so two of them would answer alike; a
// statistics manager does, and two of those would be two sets of measurements,
// each gathered on its own schedule and neither seeing the other's work.
func TestNewMetadataManager(t *testing.T) {
	t.Run("it builds the four managers it stands in front of, over one table manager and one set of statistics", func(t *testing.T) {
		mm, err := NewMetadataManager()
		if err != nil {
			t.Fatalf("NewMetadataManager() error = %v", err)
		}

		if mm.tableManager == nil {
			t.Fatal("tableManager = nil, want a table manager")
		}
		if mm.viewManager == nil {
			t.Fatal("viewManager = nil, want a view manager")
		}
		if mm.statisticsManager == nil {
			t.Fatal("statisticsManager = nil, want a statistics manager")
		}
		if mm.indexManager == nil {
			t.Fatal("indexManager = nil, want an index manager")
		}

		if mm.viewManager.tableManager != mm.tableManager {
			t.Error("the view manager keeps a table manager of its own, want the one this manager holds")
		}
		if mm.statisticsManager.tableManager != mm.tableManager {
			t.Error("the statistics manager keeps a table manager of its own, want the one this manager holds")
		}
		if mm.indexManager.tableManager != mm.tableManager {
			t.Error("the index manager keeps a table manager of its own, want the one this manager holds")
		}
		if mm.indexManager.statisticsManager != mm.statisticsManager {
			t.Error("the index manager keeps a statistics manager of its own, want the one this manager holds")
		}
	})
}

// Three of the four managers keep a catalog, and all three are made here. A
// database that had some of them would be one where a call went through to a
// table that was not there, and which of the calls that is depends on which
// catalog was missed.
func TestMetadataManagerCreateCatalogTables(t *testing.T) {
	t.Run("when the catalogs are created, then every catalog the managers read through can be read", func(t *testing.T) {
		tx := newTestTransaction(t)

		mm, err := NewMetadataManager()
		if err != nil {
			t.Fatalf("NewMetadataManager() error = %v", err)
		}
		if err := mm.CreateCatalogTables(tx); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}

		for _, catalogName := range []string{
			tableCatalogName,
			fieldCatalogName,
			viewCatalogName,
			indexCatalogName,
		} {
			if _, err := mm.GetLayout(tx, catalogName); err != nil {
				t.Errorf("GetLayout(%q) error = %v", catalogName, err)
			}
		}
	})
}

// Each pair below goes out through one call and back through another, because
// what this manager adds is the wiring rather than any working of its own. What
// each manager does with a call is held down by that manager's own tests; what
// is left to say here is that a call reaches the one that can answer it.
func TestMetadataManagerCreateTableAndGetLayout(t *testing.T) {
	t.Run("given a table created through this manager, when its layout is asked for, then it is the one the table was created with", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		if err := mm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}

		layout, err := mm.GetLayout(tx, testTableName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		assertLayout(t, layout, layoutDescription{
			slotSize: testTableSlotSize,
			fields: []layoutField{
				{
					name:      "id",
					fieldType: recordmanager.FieldTypeInt,
					length:    0,
					offset:    4,
				},
				{
					name:      "name",
					fieldType: recordmanager.FieldTypeVarchar,
					length:    testStringFieldLength,
					offset:    8,
				},
			},
		})
	})
}

func TestMetadataManagerCreateViewAndGetViewDefinition(t *testing.T) {
	t.Run("given a view created through this manager, when its definition is asked for, then it is the text the view was created with", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		if err := mm.CreateView(tx, testViewName, testViewDefinition); err != nil {
			t.Fatalf("CreateView() error = %v", err)
		}

		definition, found, err := mm.GetViewDefinition(tx, testViewName)
		if err != nil {
			t.Fatalf("GetViewDefinition() error = %v", err)
		}
		if !found {
			t.Fatalf("GetViewDefinition() found = false, want true: the view was just created")
		}
		if definition != testViewDefinition {
			t.Errorf("GetViewDefinition() = %q, want %q", definition, testViewDefinition)
		}
	})
}

func TestMetadataManagerCreateIndexAndGetIndexInfo(t *testing.T) {
	t.Run("given an index created through this manager, when the table's indexes are asked for, then it is among them, under the field it is on", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		if err := mm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}
		if err := mm.CreateIndex(tx, testIndexName, testTableName, testIndexFieldName); err != nil {
			t.Fatalf("CreateIndex() error = %v", err)
		}

		indexes, err := mm.GetIndexInfo(tx, testTableName)
		if err != nil {
			t.Fatalf("GetIndexInfo() error = %v", err)
		}

		info, ok := indexes[testIndexFieldName]
		if !ok {
			t.Fatalf("the indexes are %v, want one filed under %q", indexes, testIndexFieldName)
		}
		if info.indexName != testIndexName {
			t.Errorf("indexName = %q, want %q", info.indexName, testIndexName)
		}
	})
}

func TestMetadataManagerGetStatistics(t *testing.T) {
	t.Run("given a table with records created through this manager, when its statistics are asked for, then they are what a scan of it finds", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		if err := mm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}
		insertTestRecords(t, tx, mm.tableManager, testTableRecordCount)

		stats, err := mm.GetStatistics(tx, testTableName)
		if err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		if got := stats.RecordsOutput(); got != testTableRecordCount {
			t.Errorf("RecordsOutput() = %d, want %d", got, testTableRecordCount)
		}
		if got := stats.BlocksAccessed(); got != 2 {
			t.Errorf("BlocksAccessed() = %d, want 2", got)
		}
	})
}
