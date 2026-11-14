package metadatamanager

import (
	"sync"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// callsBetweenRefreshes is the number of GetStatistics calls after which the
// statistics of every table are gathered again.
//
// The limit counts calls and not time or writes, because calls are the only
// events that this layer sees. Nothing notifies it when a table grows, so the
// error of the statistics is never known. Only their age is known, and this
// constant defines how old is too old.
//
// A higher value means that plans are costed with older statistics. A lower
// value means that every table is scanned more often, and those scans are the
// whole cost of this manager.
const callsBetweenRefreshes = 100

// StatisticsManager holds the statistics that a planner uses to cost tables,
// one set per table, and decides when to gather them again.
//
// Unlike the table and view managers, it stores nothing on disk. Those two
// keep their data in catalogs, because a table with a lost definition could
// not be read. The statistics can be lost and rebuilt at any time. They are
// only used to choose between plans, and wrong statistics lead to a slow
// plan, not to a wrong result. Gathering them requires a scan of every table,
// so keeping them exact would cost more than their inaccuracy does.
//
// This is also why this manager has a lock and the other two do not. Those
// two hold no state of their own. They only access tables, which the
// transaction and the lock table already protect. This manager is a single
// object shared by all transactions, with a map that each of them writes to.
// Concurrent writes to a Go map cause a panic.
type StatisticsManager struct {
	mu sync.Mutex
	// tableManager provides the layout of a table when its statistics are
	// gathered, because gathering scans the table.
	tableManager *TableManager
	// statistics holds the gathered statistics by table name. It holds the
	// result of the last refresh, which can be empty.
	statistics map[string]*TableStatistics
	// callsSinceRefresh counts the GetStatistics calls since the last
	// refresh, and determines when the next refresh is due. It is reset to
	// zero at every refresh, and its name reflects that.
	callsSinceRefresh int
}

// NewStatisticsManager returns a statistics manager with no statistics yet.
//
// It reads no tables, so it takes no transaction. The first GetStatistics
// call triggers the first gathering. Until then the manager holds a table
// manager and an empty map.
func NewStatisticsManager(tableManager *TableManager) *StatisticsManager {
	return &StatisticsManager{
		tableManager: tableManager,
		statistics:   map[string]*TableStatistics{},
	}
}

// GetStatistics returns the statistics of tableName.
//
// The result can be out of date, and this is by design. This manager is not
// notified when a table changes, so exact statistics would require a scan of
// the table on every call. That would cost more than the better plan saves.
// A plan costed with stale statistics can be slower than the best plan, but
// it never gives a wrong result.
//
// If the manager holds no statistics for the table, it computes them for
// that table alone and does not refresh everything. Such a table was created
// after the last refresh, and a scan of every other table would not help the
// caller.
//
// The lock is held for the whole call, including a refresh. This has two
// consequences, both caused by the single shared set of statistics. The
// caller whose call triggers the refresh pays for a scan of every table,
// however small its own query is. During that scan, every other planner in
// the database waits for the lock.
func (sm *StatisticsManager) GetStatistics(tx *transaction.Transaction, tableName string) (*TableStatistics, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.callsSinceRefresh++
	if sm.callsSinceRefresh > callsBetweenRefreshes {
		if err := sm.refreshStatistics(tx); err != nil {
			return nil, err
		}
	}

	if stats, ok := sm.statistics[tableName]; ok {
		return stats, nil
	}

	layout, err := sm.tableManager.GetLayout(tx, tableName)
	if err != nil {
		return nil, err
	}

	stats, err := sm.calculateTableStatistics(tx, tableName, layout)
	if err != nil {
		return nil, err
	}
	sm.statistics[tableName] = stats

	return stats, nil
}

// refreshStatistics computes the statistics of every table in the table
// catalog and replaces the stored statistics with them.
//
// The result reflects the current catalogs only, and is not merged with the
// old statistics. A dropped table is no longer in the catalog, so its
// statistics are removed. Otherwise they would stay for the lifetime of the
// process, and a later table with the same name would be costed with them.
//
// The stored statistics are replaced only after all tables have been
// processed. If a refresh fails partway, the old statistics stay. They are
// stale but consistent, unlike a mix of old and new statistics.
//
// The catalogs are processed like every other table, because the table
// catalog lists them too. A plan that reads them is costed like any other.
//
// This method takes no lock. The caller already holds it, and a Go mutex
// cannot be locked twice by the same goroutine.
func (sm *StatisticsManager) refreshStatistics(tx *transaction.Transaction) error {
	layout, err := sm.tableManager.GetLayout(tx, tableCatalogName)
	if err != nil {
		return err
	}

	ts, err := query.NewTableScan(tx, tableCatalogName, layout)
	if err != nil {
		return err
	}
	defer ts.Close()

	gathered := map[string]*TableStatistics{}
	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return err
		}
		if !hasNext {
			sm.statistics = gathered
			sm.callsSinceRefresh = 0

			return nil
		}

		tableName, err := ts.GetString(tableNameField)
		if err != nil {
			return err
		}

		tableLayout, err := sm.tableManager.GetLayout(tx, tableName)
		if err != nil {
			return err
		}

		stats, err := sm.calculateTableStatistics(tx, tableName, tableLayout)
		if err != nil {
			return err
		}

		gathered[tableName] = stats
	}
}

// calculateTableStatistics scans a table to count its blocks and records.
//
// No cheaper method exists. Nothing counts records when they are written, so
// the only way to count them is to scan all of them. This is why a refresh is
// expensive and is done rarely.
//
// The block count comes from the last record found, not from the file size.
// A table whose last blocks have become empty is therefore counted by the
// blocks that still hold records. The count starts at one, because a table
// scan opens one block even for an empty table, and reading it costs that
// block.
//
// This method takes no lock, for the same reason as refreshStatistics.
func (sm *StatisticsManager) calculateTableStatistics(tx *transaction.Transaction, tableName string, layout *recordmanager.Layout) (*TableStatistics, error) {
	ts, err := query.NewTableScan(tx, tableName, layout)
	if err != nil {
		return nil, err
	}
	defer ts.Close()

	numRecords := 0
	numBlocks := 1

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil {
			return nil, err
		}
		if !hasNext {
			return NewTableStatistics(numBlocks, numRecords), nil
		}

		numRecords++

		rid, err := ts.CurrentRecordID()
		if err != nil {
			return nil, err
		}
		if blocks := rid.BlockNumber() + 1; blocks > numBlocks {
			numBlocks = blocks
		}
	}
}
