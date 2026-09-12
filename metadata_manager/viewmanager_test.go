package metadatamanager

import (
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// The view catalog's own width, built from the same numbers as the other two:
//
//	view_catalog  4 + (4 + 64) + (4 + 400) = 476 bytes
//
// The definition field is what makes it wide. It is also why testBlockSize is
// what it is, since no block narrower than this can hold a view catalog record.
const viewCatalogSlotSize = 476

func TestNewViewManager(t *testing.T) {
	t.Run("it carries the table manager it was made from", func(t *testing.T) {
		tableManager := mustNewTableManager(t)

		vm := NewViewManager(tableManager)

		if vm.tableManager != tableManager {
			t.Errorf("tableManager = %p, want %p", vm.tableManager, tableManager)
		}
	})
}

func TestViewManagerCreateCatalogTable(t *testing.T) {
	t.Run("when the view catalog is created, then the table catalog holds a row for it", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)
		vm := NewViewManager(tm)

		if err := vm.CreateCatalogTable(tx); err != nil {
			t.Fatalf("CreateCatalogTable() error = %v", err)
		}

		want := []tableCatalogRow{
			{
				tableName: viewCatalogName,
				slotSize:  viewCatalogSlotSize,
			},
		}
		if got := readTableCatalog(t, tx, tm); !slices.Equal(got, want) {
			t.Errorf("the table catalog holds %+v, want %+v", got, want)
		}
	})

	// The layout is read back rather than the rows, because reading it back is
	// what every later view call does first: the view catalog is an ordinary
	// table, so writing a view means opening a scan on it, and a scan needs the
	// layout the table catalog holds for it.
	t.Run("when the view catalog is created, then its layout can be read back through the table manager", func(t *testing.T) {
		tx := newTestTransaction(t)
		tm := mustNewTableManager(t)
		vm := NewViewManager(tm)

		if err := vm.CreateCatalogTable(tx); err != nil {
			t.Fatalf("CreateCatalogTable() error = %v", err)
		}

		layout, err := tm.GetLayout(tx, viewCatalogName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		assertLayout(t, layout, layoutDescription{
			slotSize: viewCatalogSlotSize,
			fields: []layoutField{
				{
					name:      viewNameField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxNameLength,
					offset:    4,
				},
				{
					name:      viewDefinitionField,
					fieldType: recordmanager.FieldTypeVarchar,
					length:    maxViewDefinitionLength,
					offset:    72,
				},
			},
		})
	})
}
