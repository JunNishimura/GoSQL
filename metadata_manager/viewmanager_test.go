package metadatamanager

import (
	"errors"
	"slices"
	"strings"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The slot size of the view catalog, from the same sizes as the other two:
//
//	view_catalog  4 + (4 + 64) + (4 + 400) = 476 bytes
//
// The definition field makes the slot large. It also determines
// testBlockSize, because no smaller block can hold a view catalog record.
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

	// The test reads the layout and not the rows, because every later view
	// call starts by reading the layout. The view catalog is an ordinary
	// table, so writing a view opens a scan of it, and the scan needs the
	// layout that the table catalog stores for it.
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

// The views that the tests below create. Nothing here parses the definitions,
// so their content only matters in that the two differ.
const (
	testViewName        = "active_users"
	testViewDefinition  = "select id, name from users where active = 1"
	otherViewName       = "recent_orders"
	otherViewDefinition = "select id from orders where placed_at > 0"
)

// newTestViewManager builds a view manager over existing catalogs, so that a
// test can work with views directly.
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

	// A second view must be added to the catalog and must not replace the
	// first. The catalog is written through MoveToNewRecord, which claims a
	// free slot. The two views are therefore separate records, and a read
	// returns both in the order in which they were written.
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

// createTestViews adds two views to the catalog. Reading one of them then
// requires selecting a row, not reading the only row.
func createTestViews(t *testing.T, tx *transaction.Transaction, vm *ViewManager) {
	t.Helper()

	if err := vm.CreateView(tx, testViewName, testViewDefinition); err != nil {
		t.Fatalf("CreateView(%q) error = %v", testViewName, err)
	}
	if err := vm.CreateView(tx, otherViewName, otherViewDefinition); err != nil {
		t.Fatalf("CreateView(%q) error = %v", otherViewName, err)
	}
}

func TestViewManagerGetViewDefinition(t *testing.T) {
	tests := []struct {
		name     string
		viewName string
		want     string
	}{
		{
			name:     "given two views in the catalog, when the first one is asked for, then its own definition comes back",
			viewName: testViewName,
			want:     testViewDefinition,
		},
		{
			name:     "given two views in the catalog, when the second one is asked for, then its own definition comes back",
			viewName: otherViewName,
			want:     otherViewDefinition,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			vm := newTestViewManager(t, tx)
			createTestViews(t, tx, vm)

			got, found, err := vm.GetViewDefinition(tx, tt.viewName)
			if err != nil {
				t.Fatalf("GetViewDefinition() error = %v", err)
			}
			if !found {
				t.Fatalf("GetViewDefinition() found = false, want true: the catalog holds %q", tt.viewName)
			}
			if got != tt.want {
				t.Errorf("GetViewDefinition() = %q, want %q", got, tt.want)
			}
		})
	}

	// A name with no row in the catalog is not an error. A query that names a
	// table calls this method first, and for every table that is not a view
	// the answer is false. If that were an error, the caller would have to
	// handle the usual case as a failure.
	t.Run("given a catalog that holds other views, when a name it has no row for is asked for, then it reports that there is no such view and no error", func(t *testing.T) {
		tx := newTestTransaction(t)
		vm := newTestViewManager(t, tx)
		createTestViews(t, tx, vm)

		got, found, err := vm.GetViewDefinition(tx, "no_such_view")
		if err != nil {
			t.Fatalf("GetViewDefinition() error = %v, want nil: a name that is not a view is not a failure", err)
		}
		if found {
			t.Errorf("GetViewDefinition() found = true, want false")
		}
		if got != "" {
			t.Errorf("GetViewDefinition() = %q, want the empty string when there is no view", got)
		}
	})
}

// A name or a definition that is too long for the catalog is rejected here
// and not by the record page. The record page would reject it too, but its
// error names a field of the view catalog, while the caller passed a view.
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
