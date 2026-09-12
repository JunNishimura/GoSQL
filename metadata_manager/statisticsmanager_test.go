package metadatamanager

import (
	"maps"
	"testing"

	"github.com/JunNishimura/GoSQL/transaction"
)

// How many records of the test table go in a block, and a count that runs past
// that, so a measured table is one that reaches into a second block.
//
//	a block             800 bytes
//	a test_table slot    92 bytes
//	records to a block    8, so the ninth starts block 1
const (
	testTableRecordsPerBlock = testBlockSize / testTableSlotSize
	testTableRecordCount     = testTableRecordsPerBlock + 1
)

// tableCounts is what was measured about one table, flattened so that a whole
// refresh can be compared in one go.
type tableCounts struct {
	blocks  int
	records int
}

// newTestStatisticsManager builds a statistics manager over a database that
// already holds the catalogs and the test table, which is the state every
// measurement starts from.
func newTestStatisticsManager(t *testing.T, tx *transaction.Transaction) *StatisticsManager {
	t.Helper()

	tm := mustNewTableManager(t)
	if err := tm.CreateCatalogTables(tx); err != nil {
		t.Fatalf("CreateCatalogTables() error = %v", err)
	}
	if err := tm.CreateTable(tx, testTableName, newTestSchema(t)); err != nil {
		t.Fatalf("CreateTable(%q) error = %v", testTableName, err)
	}

	return NewStatisticsManager(tm)
}

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

func TestStatisticsManagerCalculateTableStatistics(t *testing.T) {
	t.Run("given a table whose records run past the end of one block, when it is measured, then both blocks and all of the records are counted", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)
		insertTestRecords(t, tx, sm.tableManager, testTableRecordCount)

		layout, err := sm.tableManager.GetLayout(tx, testTableName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		stats, err := sm.calculateTableStatistics(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("calculateTableStatistics() error = %v", err)
		}

		if got := stats.BlocksAccessed(); got != 2 {
			t.Errorf("BlocksAccessed() = %d, want 2", got)
		}
		if got := stats.RecordsOutput(); got != testTableRecordCount {
			t.Errorf("RecordsOutput() = %d, want %d", got, testTableRecordCount)
		}
	})

	// A table nothing has been written to still has the block a scan of it
	// opens, and reading it still costs that block. Counting the blocks from
	// the last record found would call it nothing at all, which would make an
	// empty table the cheapest thing a plan could read and free to read twice.
	t.Run("given a table with no records in it, when it is measured, then it is one block holding nothing", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)

		layout, err := sm.tableManager.GetLayout(tx, testTableName)
		if err != nil {
			t.Fatalf("GetLayout() error = %v", err)
		}

		stats, err := sm.calculateTableStatistics(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("calculateTableStatistics() error = %v", err)
		}

		if got := stats.BlocksAccessed(); got != 1 {
			t.Errorf("BlocksAccessed() = %d, want 1", got)
		}
		if got := stats.RecordsOutput(); got != 0 {
			t.Errorf("RecordsOutput() = %d, want 0", got)
		}
	})
}

// A refresh measures every table the table catalog names, which is the two
// catalogs as well as the tables a user made. The numbers below are what the
// catalogs hold once the test table has been created:
//
//	table_catalog  3 rows, one per table, 10 to a block  -> 1 block
//	field_catalog  9 rows, 2 + 5 for the catalogs and 2  -> 2 blocks
//	               for the test table, 5 to a block
func TestStatisticsManagerRefreshStatistics(t *testing.T) {
	t.Run("given the catalogs and a table with records, when the statistics are refreshed, then every table the catalogs name is measured", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)
		insertTestRecords(t, tx, sm.tableManager, testTableRecordCount)

		if err := sm.refreshStatistics(tx); err != nil {
			t.Fatalf("refreshStatistics() error = %v", err)
		}

		want := map[string]tableCounts{
			tableCatalogName: {
				blocks:  1,
				records: 3,
			},
			fieldCatalogName: {
				blocks:  2,
				records: 9,
			},
			testTableName: {
				blocks:  2,
				records: testTableRecordCount,
			},
		}

		got := map[string]tableCounts{}
		for tableName, stats := range sm.statistics {
			got[tableName] = tableCounts{
				blocks:  stats.BlocksAccessed(),
				records: stats.RecordsOutput(),
			}
		}

		if !maps.Equal(got, want) {
			t.Errorf("the statistics hold %+v, want %+v", got, want)
		}
	})

	t.Run("when the statistics are refreshed, then the count of calls since the last refresh goes back to zero", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)
		sm.callsSinceRefresh = 42

		if err := sm.refreshStatistics(tx); err != nil {
			t.Fatalf("refreshStatistics() error = %v", err)
		}

		if sm.callsSinceRefresh != 0 {
			t.Errorf("callsSinceRefresh = %d, want 0", sm.callsSinceRefresh)
		}
	})

	// What a refresh holds afterwards is what the catalogs say now, not that
	// with whatever was there before left underneath it. A table that has been
	// dropped would otherwise keep its numbers for as long as the database ran,
	// and a plan reading a table of that name later would be costed by them.
	t.Run("given statistics for a table the catalogs do not name, when the statistics are refreshed, then that table is no longer among them", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)
		sm.statistics["dropped_table"] = NewTableStatistics(9, 99)

		if err := sm.refreshStatistics(tx); err != nil {
			t.Fatalf("refreshStatistics() error = %v", err)
		}

		if stats, ok := sm.statistics["dropped_table"]; ok {
			t.Errorf("the statistics still hold dropped_table as %d blocks and %d records, want it gone", stats.BlocksAccessed(), stats.RecordsOutput())
		}
	})
}
