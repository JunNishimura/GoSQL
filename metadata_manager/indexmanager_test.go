package metadatamanager

import "testing"

func TestNewIndexManager(t *testing.T) {
	t.Run("it carries the table manager and the statistics manager it was made from", func(t *testing.T) {
		tableManager := mustNewTableManager(t)
		statisticsManager := NewStatisticsManager(tableManager)

		im := NewIndexManager(tableManager, statisticsManager)

		if im.tableManager != tableManager {
			t.Errorf("tableManager = %p, want %p", im.tableManager, tableManager)
		}
		if im.statisticsManager != statisticsManager {
			t.Errorf("statisticsManager = %p, want %p", im.statisticsManager, statisticsManager)
		}
	})
}
