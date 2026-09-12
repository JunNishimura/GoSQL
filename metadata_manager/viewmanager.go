package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The view catalog is a table like the two the table manager keeps, and holds
// one row per view.
const viewCatalogName = "view_catalog"

// The fields of the view catalog.
const (
	// viewNameField is the name the view is known by. It is held to the same
	// length as a table name, since a query naming a view has that name where
	// it would have a table's and cannot tell from it which of the two it got.
	viewNameField = "view_name"
	// viewDefinitionField is the query the view stands for, kept as its text.
	// Nothing here reads it, so nothing here needs to know any SQL: whoever
	// asks for a definition is the one that makes sense of it.
	viewDefinitionField = "view_definition"
)

// maxViewDefinitionLength is the longest view definition the catalog can hold.
//
// It is what makes the view catalog's records wide: one of its slots is 476
// bytes, against 76 for the table catalog. Since a record page refuses a layout
// whose slot does not fit in a block, that width is also a floor on the block
// size the database as a whole can be built with.
const maxViewDefinitionLength = 100

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

// CreateCatalogTable makes the view catalog, which is an ordinary table and so
// is made the way any other one is.
//
// It is apart from NewViewManager for the reason CreateCatalogTables is apart
// from NewTableManager: whether the database is new is the caller's to know.
// Calling it on a database that already has a view catalog would write a second
// row describing it.
func (vm *ViewManager) CreateCatalogTable(tx *transaction.Transaction) error {
	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here, since the two names are fixed by this
	// package and are not the same. The error is returned rather than dropped so
	// that this stays true if AddField grows another reason to refuse a field.
	if err := schema.AddStringField(viewNameField, maxNameLength); err != nil {
		return err
	}
	if err := schema.AddStringField(viewDefinitionField, maxViewDefinitionLength); err != nil {
		return err
	}

	return vm.tableManager.CreateTable(tx, viewCatalogName, schema)
}
