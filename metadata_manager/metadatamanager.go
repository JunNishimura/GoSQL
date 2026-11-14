package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// MetadataManager is the single entry point to all metadata of the database:
// its tables, its views, the statistics of its tables, and its indexes.
//
// It wraps four managers, and a caller that plans a query needs all of them.
// Without this type, every call site would have to choose the right manager,
// in the same way each time. The four managers would also have to be wired
// together by the caller, and they only give consistent answers if the wiring
// is correct.
//
// The methods here add no behavior. Each one forwards to the method it names,
// and that method documents the behavior.
type MetadataManager struct {
	tableManager      *TableManager
	viewManager       *ViewManager
	statisticsManager *StatisticsManager
	indexManager      *IndexManager
}

// NewMetadataManager creates the four managers and connects them.
//
// One table manager serves all four managers, and one statistics manager
// serves both the caller and the index manager. Sharing the table manager
// matters little, because a table manager holds no state and two of them
// would give the same answers. Sharing the statistics manager matters. A
// statistics manager holds the statistics it has gathered. A second one
// would hold a second set, refreshed on its own schedule and independent of
// the first. An index costed through one and a table costed through the other
// would then use statistics from different times.
//
// It reads nothing, so it takes no transaction. The catalogs are created by a
// separate call.
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

// CreateCatalogTables creates every catalog that the wrapped managers read. A
// database needs these catalogs before any manager can answer.
//
// The two catalogs of the table manager must be created first. The view and
// index catalogs are ordinary tables, so creating them writes rows to the
// table and field catalogs, which must already exist.
//
// The statistics manager has no catalog. Its data is computed and not stored,
// so it is the only one of the four that writes nothing.
func (mm *MetadataManager) CreateCatalogTables(tx *transaction.Transaction) error {
	if err := mm.tableManager.CreateCatalogTables(tx); err != nil {
		return err
	}
	if err := mm.viewManager.CreateCatalogTable(tx); err != nil {
		return err
	}

	return mm.indexManager.CreateCatalogTable(tx)
}

// CreateTable records the definition of a table. See TableManager.CreateTable.
func (mm *MetadataManager) CreateTable(tx *transaction.Transaction, tableName string, schema *recordmanager.Schema) error {
	return mm.tableManager.CreateTable(tx, tableName, schema)
}

// GetLayout reads the definition of a table. See TableManager.GetLayout.
func (mm *MetadataManager) GetLayout(tx *transaction.Transaction, tableName string) (*recordmanager.Layout, error) {
	return mm.tableManager.GetLayout(tx, tableName)
}

// CreateView records a view under a name. See ViewManager.CreateView.
func (mm *MetadataManager) CreateView(tx *transaction.Transaction, viewName string, definition string) error {
	return mm.viewManager.CreateView(tx, viewName, definition)
}

// GetViewDefinition reads the query of a view, and reports whether a view
// with that name exists. See ViewManager.GetViewDefinition.
func (mm *MetadataManager) GetViewDefinition(tx *transaction.Transaction, viewName string) (string, bool, error) {
	return mm.viewManager.GetViewDefinition(tx, viewName)
}

// CreateIndex records that an index is on a field of a table. See
// IndexManager.CreateIndex.
func (mm *MetadataManager) CreateIndex(tx *transaction.Transaction, indexName string, tableName string, fieldName string) error {
	return mm.indexManager.CreateIndex(tx, indexName, tableName, fieldName)
}

// GetIndexInfo reads the indexes of a table, keyed by the indexed field. See
// IndexManager.GetIndexInfo.
func (mm *MetadataManager) GetIndexInfo(tx *transaction.Transaction, tableName string) (map[string]*IndexInfo, error) {
	return mm.indexManager.GetIndexInfo(tx, tableName)
}

// GetStatistics reads the statistics of a table, and refreshes them first if
// they are too old. See StatisticsManager.GetStatistics.
func (mm *MetadataManager) GetStatistics(tx *transaction.Transaction, tableName string) (*TableStatistics, error) {
	return mm.statisticsManager.GetStatistics(tx, tableName)
}
