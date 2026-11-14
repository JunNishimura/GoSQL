package metadatamanager

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrViewDefinitionTooLong is returned for a view definition with more
// characters than the view catalog can store.
var ErrViewDefinitionTooLong = errors.New("view definition too long")

// The view catalog is a table like the two catalogs of the table manager. It
// has one row per view.
const viewCatalogName = "view_catalog"

// The fields of the view catalog.
const (
	// viewNameField holds the name of the view. It has the same maximum
	// length as a table name. A query uses a view name in the same position
	// as a table name, and the name does not show which of the two it is.
	viewNameField = "view_name"
	// viewDefinitionField holds the query of the view as text. This package
	// does not interpret it and therefore needs no knowledge of SQL. The
	// caller that requests a definition parses it.
	viewDefinitionField = "view_definition"
)

// maxViewDefinitionLength is the maximum length of a view definition in the
// catalog.
//
// It makes the records of the view catalog large: a slot is 476 bytes,
// compared with 76 for the table catalog. A record page rejects a layout
// whose slot does not fit in a block, so this size is also the minimum block
// size of the whole database.
const maxViewDefinitionLength = 100

// ViewManager manages the metadata of the views of the database: the name of
// each view and its query.
//
// A view is a query stored under a name, so that a later query can use the
// name like a table name. Only the text of a view is stored. A view has no
// records of its own. Its records come from the tables that its query reads.
//
// The manager stores this in a catalog of its own, which is an ordinary table
// created through the table manager. This is why it holds a table manager.
//
// The manager holds no layout. The table manager stores its two layouts
// because reading the definition of any table starts with a scan of a
// catalog, and a scan needs the layout of the table that it scans. Those two
// layouts therefore cannot come from a catalog. The view catalog has no such
// constraint: its layout is read from the table catalog like that of any
// other table. The cost is that reading or writing a view scans the catalogs
// first.
type ViewManager struct {
	tableManager *TableManager
}

// NewViewManager returns a view manager that stores its catalog through
// tableManager.
//
// It accesses no storage, so it takes no transaction and cannot fail. The
// view catalog is created by a separate call, which the caller makes for a
// database that does not have one yet.
func NewViewManager(tableManager *TableManager) *ViewManager {
	return &ViewManager{
		tableManager: tableManager,
	}
}

// CreateCatalogTable creates the view catalog. It is an ordinary table and is
// created like any other table.
//
// It is separate from NewViewManager for the same reason that
// CreateCatalogTables is separate from NewTableManager: only the caller knows
// whether the database is new. On a database that already has a view catalog,
// this call would write a second row that describes the catalog.
func (vm *ViewManager) CreateCatalogTable(tx *transaction.Transaction) error {
	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here, because this package defines the two
	// names and they are different. The error is still returned, so that the
	// code stays correct if AddField gains another failure case.
	if err := schema.AddStringField(viewNameField, maxNameLength); err != nil {
		return err
	}
	if err := schema.AddStringField(viewDefinitionField, maxViewDefinitionLength); err != nil {
		return err
	}

	return vm.tableManager.CreateTable(tx, viewCatalogName, schema)
}

// CreateView records that viewName is defined by definition.
//
// The catalog row is the whole view. A view has no records of its own, so
// only the name and the text have to be stored. The data of the view is read
// from the tables in its definition when a query that uses the view runs.
//
// The definition is not parsed or validated. Only its length is checked.
// This layer has no knowledge of SQL, and the caller finds out that a
// definition does not parse when it uses the view.
func (vm *ViewManager) CreateView(tx *transaction.Transaction, viewName string, definition string) error {
	if err := checkViewFits(viewName, definition); err != nil {
		return err
	}

	layout, err := vm.tableManager.GetLayout(tx, viewCatalogName)
	if err != nil {
		return err
	}

	ts, err := query.NewTableScan(tx, viewCatalogName, layout)
	if err != nil {
		return err
	}
	defer ts.Close()

	if err := ts.MoveToNewRecord(); err != nil {
		return err
	}
	if err := ts.SetString(viewNameField, viewName); err != nil {
		return err
	}

	return ts.SetString(viewDefinitionField, definition)
}

// GetViewDefinition returns the query of viewName, and reports whether the
// catalog has a row for that name.
//
// A name with no row is not an error, so the method reports it as false. A
// query that names a table calls this method first, and for every name that
// is a table and not a view the answer is false. If that were an error, the
// caller would have to handle the usual case as a failure, and could not
// distinguish it from an unreadable catalog.
//
// The whole catalog is scanned, because the rows have no order: a view is
// written to any free slot of the catalog.
func (vm *ViewManager) GetViewDefinition(tx *transaction.Transaction, viewName string) (string, bool, error) {
	layout, err := vm.tableManager.GetLayout(tx, viewCatalogName)
	if err != nil {
		return "", false, err
	}

	ts, err := query.NewTableScan(tx, viewCatalogName, layout)
	if err != nil {
		return "", false, err
	}
	defer ts.Close()

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return "", false, err
		}
		if !hasNext {
			return "", false, nil
		}

		name, err := ts.GetString(viewNameField)
		if err != nil {
			return "", false, err
		}
		if name != viewName {
			continue
		}

		definition, err := ts.GetString(viewDefinitionField)
		if err != nil {
			return "", false, err
		}

		return definition, true, nil
	}
}

// checkViewFits returns an error for a name or a definition that is too long
// for the view catalog.
//
// A record page would reject either one, and nothing would be partly written
// even then, because a view is a single row. The check here gives the caller
// a better error. The record page reports a field of the view catalog, while
// the caller passed a view.
//
// The definition is not a name and has its own limit, so it is checked here
// and not through checkNameFits.
func checkViewFits(viewName string, definition string) error {
	if err := checkNameFits("view", viewName); err != nil {
		return err
	}

	if count := utf8.RuneCountInString(definition); count > maxViewDefinitionLength {
		return fmt.Errorf("the definition of view %q is %d characters, and the catalog holds %d: %w", viewName, count, maxViewDefinitionLength, ErrViewDefinitionTooLong)
	}

	return nil
}
