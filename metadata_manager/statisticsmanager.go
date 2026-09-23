package metadatamanager

import (
	"sync"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// callsBetweenRefreshes is how many tables may be asked about before every
// table is measured over again.
//
// It counts calls rather than time or writes, because calls are the one thing
// this layer sees. Nothing tells it when a table has grown, so how wrong the
// numbers are is never known; how long they have been held is all there is to
// go on, and this says how long is long enough.
//
// Raising it means costing plans by older numbers. Lowering it means reading
// every table more often, and reading them is the whole of the cost here.
const callsBetweenRefreshes = 100

// StatisticsManager keeps the statistics a planner costs tables by, one set per
// table, and works out when they are old enough to gather again.
//
// Unlike the table and view managers, it writes nothing down. Those two keep
// what they know in catalogs, because a table whose definition were lost would
// be a table nobody could read. What is kept here can be lost and rebuilt at
// any time: it is read to choose between plans, and a plan chosen from numbers
// that are wrong is a slow plan rather than a wrong answer. Gathering the
// numbers means reading every table through, so paying to keep them true would
// cost more than being wrong about them does.
//
// That is also why there is a lock here where the other two have none. They
// hold no state of their own, and what they touch is a table, so the
// transaction and the lock table already see to it. This one is a single
// object shared by every transaction in the database, holding a map that each
// of them writes to, and two of them writing to a Go map at once is not a race
// to be reasoned about but a panic.
type StatisticsManager struct {
	mu sync.Mutex
	// tableManager is how a table's layout is found when its statistics have
	// to be gathered, since gathering means scanning the table.
	tableManager *TableManager
	// statistics is what has been gathered so far, by table name. It holds
	// whatever the last gathering found, which may be nothing at all.
	statistics map[string]*TableStatistics
	// callsSinceRefresh counts how many tables have been asked about since the
	// last gathering, and is what decides when the next one is due. It is named
	// for what it is counted for rather than for what it counts, because it
	// goes back to zero at every gathering and the name has to say why.
	callsSinceRefresh int
}

// NewStatisticsManager returns a statistics manager that has gathered nothing
// yet.
//
// It reads no tables, so it takes no transaction. The first table asked about
// is what sets the gathering off, and until then the manager holds a table
// manager and an empty map.
func NewStatisticsManager(tableManager *TableManager) *StatisticsManager {
	return &StatisticsManager{
		tableManager: tableManager,
		statistics:   map[string]*TableStatistics{},
	}
}

// GetStatistics returns what is known about tableName.
//
// What comes back may be out of date, and that is what it is for. Nothing here
// is told when a table changes, so keeping the numbers true would mean reading
// the table on every call, which costs more than the plan it improves saves. A
// plan costed by numbers that have drifted is a plan that may be slower than
// the best one, never one that gives a wrong answer.
//
// A table nothing is held for is measured on its own rather than by gathering
// everything. It is a table made since the last gathering, and reading every
// other table again would tell the caller nothing it asked for.
//
// The lock is held across the whole call, a gathering included. Two things
// follow, and both are the price of holding one set of numbers for the whole
// database. The caller whose call happens to be the one that tips the count
// pays for reading every table, however small its own query. And while that
// runs, every other planner in the database waits here.
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

// refreshStatistics measures every table the table catalog names and keeps what
// it finds, throwing away whatever was held before.
//
// What is kept afterwards is what the catalogs say now, rather than that laid
// over what was there. A table that has been dropped is out of the catalog, so
// it falls out here too; left in, its numbers would last as long as the process
// did, and a table made under the same name later would be costed by them.
//
// Nothing is kept until all of it has been gathered. A run that fails partway
// leaves the old numbers, which are merely old, rather than a set that is half
// one measurement and half another.
//
// The catalogs are measured along with everything else, since the table catalog
// names them too. A plan that reads them is costed like any other.
//
// It takes no lock. The caller holds one already, and a Go mutex cannot be
// taken twice by the same goroutine.
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

// calculateTableStatistics reads a table through to count what is in it.
//
// There is no cheaper way to it. Nothing counts records as they are written, so
// the only way to know how many there are is to walk past all of them, which is
// what makes gathering expensive enough to be worth doing rarely.
//
// The blocks are counted from the last record found rather than from the file's
// length, so a table whose last blocks were emptied is counted by what is left
// in it. The count starts at one because a table has the block its scan opens
// even with nothing in it, and reading it costs that block.
//
// It takes no lock, for the reason refreshStatistics gives.
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
