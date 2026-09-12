package metadatamanager

import (
	"errors"
	"fmt"
	"maps"
	"sync"
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

func TestStatisticsManagerGetStatistics(t *testing.T) {
	t.Run("given a table the statistics do not hold yet, when it is asked about, then it is measured and kept", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)
		insertTestRecords(t, tx, sm.tableManager, testTableRecordCount)

		stats, err := sm.GetStatistics(tx, testTableName)
		if err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		if got := stats.RecordsOutput(); got != testTableRecordCount {
			t.Errorf("RecordsOutput() = %d, want %d", got, testTableRecordCount)
		}
		if _, ok := sm.statistics[testTableName]; !ok {
			t.Error("the statistics do not hold the table that was just measured, want it kept for the next caller")
		}
	})

	// Measuring costs a read of the whole table, so what is held is handed back
	// rather than gathered again. That the answer is out of date is the point
	// of holding it: a plan costed by it may be slower than the best one, and
	// is not a wrong answer.
	t.Run("given a table already measured, when it has grown and is asked about again, then the numbers already held come back", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)

		if _, err := sm.GetStatistics(tx, testTableName); err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		insertTestRecords(t, tx, sm.tableManager, testTableRecordCount)

		stats, err := sm.GetStatistics(tx, testTableName)
		if err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		if got := stats.RecordsOutput(); got != 0 {
			t.Errorf("RecordsOutput() = %d, want 0: the table was measured again rather than read from what is held", got)
		}
	})

	t.Run("when a table is asked about, then the count of calls since the last refresh goes up by one", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)

		if _, err := sm.GetStatistics(tx, testTableName); err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		if sm.callsSinceRefresh != 1 {
			t.Errorf("callsSinceRefresh = %d, want 1", sm.callsSinceRefresh)
		}
	})

	// The held numbers are never thrown away for being wrong, since nothing
	// here is told when a table changes. They are thrown away for being old,
	// and how old is counted in calls.
	t.Run("given as many calls as go between refreshes, when one more is made, then every table is measured over and the count starts again", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)

		if _, err := sm.GetStatistics(tx, testTableName); err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		insertTestRecords(t, tx, sm.tableManager, testTableRecordCount)
		sm.callsSinceRefresh = callsBetweenRefreshes

		stats, err := sm.GetStatistics(tx, testTableName)
		if err != nil {
			t.Fatalf("GetStatistics() error = %v", err)
		}

		if got := stats.RecordsOutput(); got != testTableRecordCount {
			t.Errorf("RecordsOutput() = %d, want %d: the statistics were not gathered again", got, testTableRecordCount)
		}
		if sm.callsSinceRefresh != 0 {
			t.Errorf("callsSinceRefresh = %d, want 0", sm.callsSinceRefresh)
		}
	})

	t.Run("given a table the catalogs do not hold, when it is asked about, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		sm := newTestStatisticsManager(t, tx)

		if _, err := sm.GetStatistics(tx, "no_such_table"); !errors.Is(err, ErrTableNotFound) {
			t.Errorf("error = %v, want %v", err, ErrTableNotFound)
		}
	})
}

// How many callers ask at once, and how many times each of them asks. The two
// multiply out to more than callsBetweenRefreshes, so at least one gathering
// happens while the other callers are waiting to be let in.
const (
	concurrentCallers = 4
	callsPerCaller    = callsBetweenRefreshes/concurrentCallers + 10
)

// askForStatisticsRepeatedly asks about the test table over and over on a
// transaction of its own, and reports by returning.
//
// It runs on a goroutine other than the test's, where *testing.T must not be
// failed: a Fatal there stops that goroutine and leaves the test passing.
func askForStatisticsRepeatedly(db *testDatabase, sm *StatisticsManager) error {
	tx, err := db.newTransaction()
	if err != nil {
		return err
	}

	for range callsPerCaller {
		stats, err := sm.GetStatistics(tx, testTableName)
		if err != nil {
			return err
		}
		if got := stats.RecordsOutput(); got != testTableRecordCount {
			return fmt.Errorf("RecordsOutput() = %d, want %d", got, testTableRecordCount)
		}
	}

	return tx.Commit()
}

// One statistics manager serves the whole database, so several transactions
// reach into the same map at once. Two goroutines writing a Go map is not a
// race to be reasoned about afterwards but a crash, which is what the lock in
// the manager is there for, and this is what says so.
//
// The table does not change while this runs, so every answer has to be the same
// one. What the case is watching for is not a wrong number but a torn map, and
// the run under -race is where that shows.
func TestStatisticsManagerGetStatisticsUnderConcurrentCallers(t *testing.T) {
	t.Run("given several transactions of one database asking at once, when between them they ask often enough to set a gathering off, then each of them is answered and all the answers agree", func(t *testing.T) {
		db := newTestDatabase(t)
		tm := mustNewTableManager(t)

		setup := db.mustNewTransaction(t)
		if err := tm.CreateCatalogTables(setup); err != nil {
			t.Fatalf("CreateCatalogTables() error = %v", err)
		}
		if err := tm.CreateTable(setup, testTableName, newTestSchema(t)); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}
		insertTestRecords(t, setup, tm, testTableRecordCount)

		// The writes are committed before anyone else reads, so that the locks
		// they took are given back. Left open, the readers below would wait on
		// them until the lock table gave up on their behalf.
		if err := setup.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		sm := NewStatisticsManager(tm)

		errs := make([]error, concurrentCallers)
		var wg sync.WaitGroup
		for i := range concurrentCallers {
			wg.Add(1)
			go func() {
				defer wg.Done()

				errs[i] = askForStatisticsRepeatedly(db, sm)
			}()
		}
		wg.Wait()

		for i, err := range errs {
			if err != nil {
				t.Errorf("caller %d: %v", i, err)
			}
		}
	})
}
