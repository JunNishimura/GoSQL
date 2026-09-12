package metadatamanager

import "sync"

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
