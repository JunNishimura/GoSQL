package metadatamanager

// ViewManager is what the database knows about its views: the name of each one
// and the query it stands for.
//
// A view is a query kept under a name, so that a later query can name it where
// it would name a table. Nothing of a view is stored but its text: it has no
// records of its own, only those of whatever its query reads.
//
// It keeps that in a catalog of its own, which is an ordinary table made
// through the table manager. That is what the table manager is held here for.
//
// No layout is held alongside it. The table manager keeps its own two because
// reading any table's definition starts by scanning a catalog, and a scan needs
// the layout of the table it scans, so those two have to come from somewhere
// other than a catalog. The view catalog is under no such obligation: its
// layout is read back out of the table catalog like any other table's. The
// price is that reading or writing a view scans the catalogs first.
type ViewManager struct {
	tableManager *TableManager
}

// NewViewManager returns a view manager that keeps its catalog through
// tableManager.
//
// It touches no storage, so it takes no transaction and cannot fail. Creating
// the view catalog is a call of its own, which the caller makes on a database
// that has none yet.
func NewViewManager(tableManager *TableManager) *ViewManager {
	return &ViewManager{
		tableManager: tableManager,
	}
}
