package metadatamanager

import "testing"

func TestNewStatisticsManager(t *testing.T) {
	t.Run("it carries the table manager it was made from, holding no statistics yet", func(t *testing.T) {
		tableManager := mustNewTableManager(t)

		sm := NewStatisticsManager(tableManager)

		if sm.tableManager != tableManager {
			t.Errorf("tableManager = %p, want %p", sm.tableManager, tableManager)
		}

		// An empty map rather than a nil one: what is held here is written to
		// as tables are asked about, and a nil map cannot be written to.
		if sm.statistics == nil {
			t.Fatal("statistics = nil, want an empty map")
		}
		if len(sm.statistics) != 0 {
			t.Errorf("statistics = %v, want nothing gathered yet", sm.statistics)
		}

		if sm.callsSinceRefresh != 0 {
			t.Errorf("callsSinceRefresh = %d, want 0", sm.callsSinceRefresh)
		}
	})
}
