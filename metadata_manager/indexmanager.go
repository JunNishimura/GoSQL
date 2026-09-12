package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The index catalog is a table like the others, and holds one row per index.
const indexCatalogName = "index_catalog"

// indexNameField is what an index is called. It is the only field of the index
// catalog this package has to name for itself: a row of that catalog says an
// index is on a field of a table, and the other two of those are the table and
// the field, which are named here the way they are named in every catalog.
const indexNameField = "index_name"

// IndexManager is what the database knows about its indexes: which field of
// which table each one is on, and what going through it would cost.
//
// It keeps the first part in a catalog of its own, which is an ordinary table
// made through the table manager. The second part is not kept anywhere. What an
// index costs follows from how many records the indexed table holds and how
// varied the indexed field is among them, and those are the statistics
// manager's to say.
//
// That is what makes this the first layer here to stand on another. The table
// and view managers answer out of their own catalogs; this one reads its
// catalog to find that an index exists, and then has to ask elsewhere what the
// index is worth. Neither manager alone could answer, because the catalog knows
// of the index and the statistics know of the table, and a cost needs both.
//
// No layout is held alongside them, for the reason the view manager gives: the
// index catalog is an ordinary table, so its layout comes back out of the table
// catalog like any other. Nor is there a lock, since nothing is held here that
// two transactions could write over each other.
type IndexManager struct {
	// tableManager is how the index catalog is made and read, and how the
	// schema of an indexed table is found.
	tableManager *TableManager
	// statisticsManager is how the indexed table's measurements are found,
	// which is what an index is costed from.
	statisticsManager *StatisticsManager
}

// NewIndexManager returns an index manager that keeps its catalog through
// tableManager and costs what it finds through statisticsManager.
//
// It touches no storage, so it takes no transaction and cannot fail. Creating
// the index catalog is a call of its own, which the caller makes on a database
// that has none yet.
func NewIndexManager(tableManager *TableManager, statisticsManager *StatisticsManager) *IndexManager {
	return &IndexManager{
		tableManager:      tableManager,
		statisticsManager: statisticsManager,
	}
}

// CreateCatalogTable makes the index catalog, which is an ordinary table and so
// is made the way any other one is.
//
// It is apart from NewIndexManager for the reason the other catalog calls are
// apart from their constructors: whether the database is new is the caller's to
// know. Calling it on a database that already has an index catalog would write
// a second row describing it.
func (im *IndexManager) CreateCatalogTable(tx *transaction.Transaction) error {
	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here, since the three names are fixed by this
	// package and no two of them are the same. The error is returned rather than
	// dropped so that this stays true if AddField grows another reason to refuse
	// a field.
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
