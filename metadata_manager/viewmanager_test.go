package metadatamanager

import "testing"

func TestNewViewManager(t *testing.T) {
	t.Run("it carries the table manager it was made from", func(t *testing.T) {
		tableManager := mustNewTableManager(t)

		vm := NewViewManager(tableManager)

		if vm.tableManager != tableManager {
			t.Errorf("tableManager = %p, want %p", vm.tableManager, tableManager)
		}
	})
}
