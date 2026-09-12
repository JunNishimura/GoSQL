package metadatamanager

import (
	"errors"
	"slices"
	"strings"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
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

// The views the tests below create. The definitions are not parsed by anything
// here, so what they say matters only in that the two differ.
const (
	testViewName        = "active_users"
	testViewDefinition  = "select id, name from users where active = 1"
	otherViewName       = "recent_orders"
	otherViewDefinition = "select id from orders where placed_at > 0"
)

// newTestViewManager builds a view manager over catalogs that already exist, so
// that a test can go straight to the views.
func newTestViewManager(t *testing.T, tx *transaction.Transaction) *ViewManager {
	t.Helper()

	vm := NewViewManager(mustNewTableManager(t))
	if err := vm.CreateCatalogTable(tx); err != nil {
		t.Fatalf("CreateCatalogTable() error = %v", err)
	}

	return vm
}

func TestViewManagerCreateView(t *testing.T) {
	t.Run("when a view is created, then the view catalog holds its name and its definition", func(t *testing.T) {
		tx := newTestTransaction(t)
		vm := newTestViewManager(t, tx)

		if err := vm.CreateView(tx, testViewName, testViewDefinition); err != nil {
			t.Fatalf("CreateView() error = %v", err)
		}

		want := []viewCatalogRow{
			{
				viewName:   testViewName,
				definition: testViewDefinition,
			},
		}
		if got := readViewCatalog(t, tx, vm); !slices.Equal(got, want) {
			t.Errorf("the view catalog holds %+v, want %+v", got, want)
		}
	})

	// A second view has to sit alongside the first rather than replace it. The
	// catalog is written through MoveToNewRecord, which claims a free slot, so
	// the two land in separate records and reading gives both back in the order
	// they were written.
	t.Run("given a view the catalog already holds, when a second one is created, then the catalog holds both", func(t *testing.T) {
		tx := newTestTransaction(t)
		vm := newTestViewManager(t, tx)

		if err := vm.CreateView(tx, testViewName, testViewDefinition); err != nil {
			t.Fatalf("CreateView(%q) error = %v", testViewName, err)
		}
		if err := vm.CreateView(tx, otherViewName, otherViewDefinition); err != nil {
			t.Fatalf("CreateView(%q) error = %v", otherViewName, err)
		}

		want := []viewCatalogRow{
			{
				viewName:   testViewName,
				definition: testViewDefinition,
			},
			{
				viewName:   otherViewName,
				definition: otherViewDefinition,
			},
		}
		if got := readViewCatalog(t, tx, vm); !slices.Equal(got, want) {
			t.Errorf("the view catalog holds %+v, want %+v", got, want)
		}
	})

	t.Run("given a definition of exactly as many characters as the catalog holds, when the view is created, then it is recorded", func(t *testing.T) {
		tx := newTestTransaction(t)
		vm := newTestViewManager(t, tx)

		definition := strings.Repeat("a", maxViewDefinitionLength)

		if err := vm.CreateView(tx, testViewName, definition); err != nil {
			t.Fatalf("CreateView() error = %v", err)
		}

		rows := readViewCatalog(t, tx, vm)
		if len(rows) != 1 || rows[0].definition != definition {
			t.Errorf("the view catalog holds %+v, want the one definition of %d characters", rows, maxViewDefinitionLength)
		}
	})
}

// A name or a definition the catalog cannot hold is refused here rather than
// left to the record page. The record page would refuse it too, but its message
// names a field of the view catalog, and what the caller passed was a view.
func TestViewManagerRejectsWhatTheCatalogCannotHold(t *testing.T) {
	tests := []struct {
		name       string
		viewName   string
		definition string
		wantErr    error
	}{
		{
			name:       "given a view name one character over what the catalog holds, when the view is created, then it reports ErrNameTooLong",
			viewName:   strings.Repeat("a", maxNameLength+1),
			definition: testViewDefinition,
			wantErr:    ErrNameTooLong,
		},
		{
			name:       "given a definition one character over what the catalog holds, when the view is created, then it reports ErrViewDefinitionTooLong",
			viewName:   testViewName,
			definition: strings.Repeat("a", maxViewDefinitionLength+1),
			wantErr:    ErrViewDefinitionTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			vm := newTestViewManager(t, tx)

			if err := vm.CreateView(tx, tt.viewName, tt.definition); !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}

			if rows := readViewCatalog(t, tx, vm); len(rows) != 0 {
				t.Errorf("the view catalog holds %+v, want nothing: the refused view was written anyway", rows)
			}
		})
	}
}
