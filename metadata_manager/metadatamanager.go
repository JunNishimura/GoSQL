package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// MetadataManager is the one way in to everything the database knows about
// itself: its tables, its views, what its tables measure, and its indexes.
//
// Behind it are four managers, and a caller planning a query needs all of them.
// Sorting out which one answers what is work that would otherwise be done at
// every call site, and done the same way each time; so is wiring the four
// together, which has to come out right for any of them to agree with the
// others.
//
// The methods here carry nothing of their own. Each is the call it stands for,
// and what it does is written where that call is.
type MetadataManager struct {
	tableManager      *TableManager
	viewManager       *ViewManager
	statisticsManager *StatisticsManager
	indexManager      *IndexManager
}

// NewMetadataManager builds the four managers and ties them to one another.
//
// One table manager serves all four, and one statistics manager serves both the
// caller and the index manager. The first matters little, since a table manager
// holds nothing and two of them would answer alike. The second matters: a
// statistics manager holds what it has measured, and a second one would be a
// second set of measurements, gathered on its own schedule and blind to the
// first. An index costed through one and a table costed through the other would
// then be costed by numbers taken at different times.
//
// It reads nothing, so it takes no transaction. Making the catalogs is a call
// of its own.
func NewMetadataManager() (*MetadataManager, error) {
	tableManager, err := NewTableManager()
	if err != nil {
		return nil, err
	}

	statisticsManager := NewStatisticsManager(tableManager)

	return &MetadataManager{
		tableManager:      tableManager,
		viewManager:       NewViewManager(tableManager),
		statisticsManager: statisticsManager,
		indexManager:      NewIndexManager(tableManager, statisticsManager),
	}, nil
}

// CreateCatalogTables makes every catalog the managers behind this one read
// through, which is what a database has to have before any of them can answer.
//
// The table manager's two come first, and not by preference: the view and index
// catalogs are ordinary tables, so making them means writing rows into the
// table and field catalogs, and those have to be there to be written to.
//
// The statistics manager has no catalog. What it knows is measured rather than
// recorded, which is why it is the one of the four that writes nothing down.
func (mm *MetadataManager) CreateCatalogTables(tx *transaction.Transaction) error {
	if err := mm.tableManager.CreateCatalogTables(tx); err != nil {
		return err
	}
	if err := mm.viewManager.CreateCatalogTable(tx); err != nil {
		return err
	}

	return mm.indexManager.CreateCatalogTable(tx)
}

// CreateTable records a table's definition. See TableManager.CreateTable.
func (mm *MetadataManager) CreateTable(tx *transaction.Transaction, tableName string, schema *recordmanager.Schema) error {
	return mm.tableManager.CreateTable(tx, tableName, schema)
}

// GetLayout reads a table's definition back. See TableManager.GetLayout.
func (mm *MetadataManager) GetLayout(tx *transaction.Transaction, tableName string) (*recordmanager.Layout, error) {
	return mm.tableManager.GetLayout(tx, tableName)
}

// CreateView records a view under a name. See ViewManager.CreateView.
func (mm *MetadataManager) CreateView(tx *transaction.Transaction, viewName string, definition string) error {
	return mm.viewManager.CreateView(tx, viewName, definition)
}

// GetViewDefinition reads back the query a view stands for, and whether there
// is a view of that name at all. See ViewManager.GetViewDefinition.
func (mm *MetadataManager) GetViewDefinition(tx *transaction.Transaction, viewName string) (string, bool, error) {
	return mm.viewManager.GetViewDefinition(tx, viewName)
}

// CreateIndex records that an index is on a field of a table. See
// IndexManager.CreateIndex.
func (mm *MetadataManager) CreateIndex(tx *transaction.Transaction, indexName string, tableName string, fieldName string) error {
	return mm.indexManager.CreateIndex(tx, indexName, tableName, fieldName)
}

// GetIndexInfo reads the indexes a table has, by the field each is on. See
// IndexManager.GetIndexInfo.
func (mm *MetadataManager) GetIndexInfo(tx *transaction.Transaction, tableName string) (map[string]*IndexInfo, error) {
	return mm.indexManager.GetIndexInfo(tx, tableName)
}

// GetStatistics reads what a table measures, gathering if what is held has
// grown old. See StatisticsManager.GetStatistics.
func (mm *MetadataManager) GetStatistics(tx *transaction.Transaction, tableName string) (*TableStatistics, error) {
	return mm.statisticsManager.GetStatistics(tx, tableName)
}
