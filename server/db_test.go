package server

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	// testBlockSize must fit a slot of the largest catalog, which is the view
	// catalog with 476 bytes. A record page rejects a layout whose slot does
	// not fit in a block.
	testBlockSize  = 800
	testNumBuffers = 8
	// tableCatalogName is the catalog that lists every table, including the
	// catalogs. The metadata manager defines this name but does not export it,
	// so it is repeated here.
	tableCatalogName = "table_catalog"
	// unfinishedTxNum is the number of the transaction that a test writes to
	// the log as unfinished. It is far above every number that the database
	// assigns, so the log never has a commit record of a real transaction
	// with the same number.
	unfinishedTxNum = 1000
)

// countTables returns the number of tables in the table catalog.
func countTables(t *testing.T, db *DB, tx *transaction.Transaction) int {
	t.Helper()

	layout, err := db.MetadataManager().GetLayout(tx, tableCatalogName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", tableCatalogName, err)
	}
	ts, err := query.NewTableScan(tx, tableCatalogName, layout)
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}
	defer ts.Close()

	count := 0
	for {
		ok, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return count
		}
		count++
	}
}

func TestOpen(t *testing.T) {
	t.Run("given a database directory that does not exist, when it is opened, then the directory is created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "newdb")

		if _, err := Open(dir, testBlockSize, testNumBuffers); err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Stat(%q) error = %v, want the directory to exist", dir, err)
		}
	})

	t.Run("when a database is opened with a block size, then its file manager uses that block size", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		if got := db.FileManager().BlockSize(); got != testBlockSize {
			t.Errorf("FileManager().BlockSize() = %d, want %d", got, testBlockSize)
		}
	})

	t.Run("given a database directory that does not exist, when it is opened, then the catalogs are made and list themselves", func(t *testing.T) {
		db, err := Open(filepath.Join(t.TempDir(), "newdb"), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if got := countTables(t, db, tx); got != 4 {
			t.Errorf("the table catalog lists %d tables, want 4 (itself and the field, view and index catalogs)", got)
		}
	})

	// Creating the catalogs again would list every catalog twice. A database
	// that was wrongly opened as new would show this.
	t.Run("given a database that holds a table, when it is opened again, then the catalogs are left as they were", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		schema := recordmanager.NewSchema()
		if err := schema.AddIntField("id"); err != nil {
			t.Fatalf("AddIntField() error = %v", err)
		}
		if err := db.MetadataManager().CreateTable(tx, "test", schema); err != nil {
			t.Fatalf("CreateTable() error = %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		reopened, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		tx, err = reopened.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if got := countTables(t, reopened, tx); got != 5 {
			t.Errorf("the table catalog lists %d tables, want 5 (the four catalogs and the table)", got)
		}
	})

	// The state after a crash is written directly: a value on disk from a
	// transaction with no end record in the log, and its pre-image in the log.
	t.Run("given a database that a transaction left unfinished, when it is opened again, then what the transaction wrote is undone", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		fm := db.FileManager()
		blk, err := fm.Append("data.tbl")
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		page := filemanager.NewPageByBlockSize(testBlockSize)
		if err := page.SetInt(0, 200); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := fm.Write(blk, page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
		lsn, err := recoverymanager.WriteSetIntRecordToLog(db.LogManager(), unfinishedTxNum, blk, 0, 100)
		if err != nil {
			t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
		}
		if err := db.LogManager().Flush(lsn); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}

		reopened, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		got := filemanager.NewPageByBlockSize(testBlockSize)
		if err := reopened.FileManager().Read(blk, got); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if v := got.GetInt(0); v != 100 {
			t.Errorf("the block holds %d, want 100 (the value from before the unfinished write)", v)
		}
	})
}

// startedTxNums returns the number of every transaction with a start record
// in the log, including the transaction that Open ran.
func startedTxNums(t *testing.T, db *DB) []int {
	t.Helper()

	it, err := db.LogManager().Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}
	var txNums []int
	for it.HasNext() {
		bytes, err := it.Next()
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		rec, err := recoverymanager.CreateLogRecord(bytes)
		if err != nil {
			t.Fatalf("CreateLogRecord() error = %v", err)
		}
		if rec.Op() == recoverymanager.Start {
			txNums = append(txNums, rec.TxNumber())
		}
	}

	return txNums
}

func runInBackground(f func() error) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- f()
	}()
	return done
}

func TestDBNewTransaction(t *testing.T) {
	// The log distinguishes transactions only by number. Two transactions
	// with the same number would be treated as one: the commit of either
	// would make recovery keep the unfinished changes of the other.
	t.Run("given transactions started from many goroutines at once, then no two are given the same number", func(t *testing.T) {
		const numTxs = 20
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}

		var wg sync.WaitGroup
		errs := make(chan error, numTxs)
		for range numTxs {
			wg.Go(func() {
				if _, err := db.NewTransaction(); err != nil {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		txNums := startedTxNums(t, db)
		if len(txNums) != numTxs+1 {
			t.Fatalf("the log shows %d transactions started, want %d (those started here and the one Open ran)", len(txNums), numTxs+1)
		}
		seen := make(map[int]bool)
		for _, txNum := range txNums {
			if seen[txNum] {
				t.Errorf("transaction number %d was given out more than once", txNum)
			}
			seen[txNum] = true
		}
	})

	// Locks isolate transactions only if all of them use the same lock table,
	// so a transaction must be started on the lock table of the database.
	t.Run("given a transaction that has written a block, when another on the same database pins it, then the second waits until the first commits", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		writer, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		reader, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk, err := writer.Append("data.tbl")
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if err := writer.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := writer.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		done := runInBackground(func() error {
			return reader.Pin(blk)
		})

		select {
		case err := <-done:
			t.Fatalf("Pin() returned %v while the block was locked, want it to wait", err)
		case <-time.After(50 * time.Millisecond):
			// Still waiting, which is the expected behaviour.
		}

		if err := writer.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Pin() did not return after the writer committed")
		}
	})
}

// selectIDs runs sql through the planner of db and returns the id of every
// record in the result.
func selectIDs(t *testing.T, db *DB, tx *transaction.Transaction, sql string) []int32 {
	t.Helper()

	p, err := db.Planner().CreateQueryPlan(sql, tx)
	if err != nil {
		t.Fatalf("CreateQueryPlan(%q) error = %v", sql, err)
	}
	s, err := p.Open()
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer s.Close()

	var ids []int32
	for {
		ok, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !ok {
			return ids
		}
		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt(%q) error = %v", "id", err)
		}
		ids = append(ids, id)
	}
}

// executeUpdates runs every statement in sqls through the planner of db, in
// tx.
func executeUpdates(t *testing.T, db *DB, tx *transaction.Transaction, sqls ...string) {
	t.Helper()

	for _, sql := range sqls {
		if _, err := db.Planner().ExecuteUpdate(sql, tx); err != nil {
			t.Fatalf("ExecuteUpdate(%q) error = %v", sql, err)
		}
	}
}

func TestDBPlanner(t *testing.T) {
	t.Run("given a new database, when a table is made and a record put in through its planner, then a query through the planner reads the record back", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		executeUpdates(t, db, tx,
			"create table test (id int)",
			"insert into test (id) values (7)",
		)

		if got := selectIDs(t, db, tx, "select id from test"); !slices.Equal(got, []int32{7}) {
			t.Errorf("the query gives back ids %v, want [7]", got)
		}
	})

	// The planner reads tables through the metadata manager of the database,
	// so it knows a table that was created before the database was reopened.
	t.Run("given a database whose table and record were committed, when it is opened again, then a query through its planner reads the record back", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "db")
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		executeUpdates(t, db, tx,
			"create table test (id int)",
			"insert into test (id) values (7)",
		)
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		reopened, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err = reopened.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		if got := selectIDs(t, reopened, tx, "select id from test"); !slices.Equal(got, []int32{7}) {
			t.Errorf("the query gives back ids %v, want [7]", got)
		}
	})
}

// The record with id 7 is written before the index is created, and the record
// with id 8 after it. The index must therefore be built from the table and
// also be maintained by later statements. The record with id 9 is deleted
// again, so its entry must be removed. No query uses an index yet, so the
// test searches the index directly.
func TestDBPlannerKeepsIndexes(t *testing.T) {
	t.Run("given a new database, when a table is made, a record put in, an index made on id, two more put in and one of them deleted through its planner, then the index finds each record left under its id and not the one deleted", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		executeUpdates(t, db, tx,
			"create table test (id int)",
			"insert into test (id) values (7)",
			"create index id_index on test (id)",
			"insert into test (id) values (8)",
			"insert into test (id) values (9)",
			"delete from test where id = 9",
		)

		for id, want := range map[int32]int{7: 1, 8: 1, 9: 0} {
			if got := countIndexed(t, db, tx, "test", "id", query.NewIntConstant(id)); got != want {
				t.Errorf("the index on id finds %d records for %d, want %d", got, id, want)
			}
		}
	})
}

// The test cannot see the plan of a query, so it infers the plan from the
// result. A record written without the planner is in the table but not in the
// index, and a query that uses the index does not return it.
func TestDBPlannerReadsThroughIndexes(t *testing.T) {
	t.Run("given a table indexed on id holding a record of id 7 put in through the planner and another written past it, when a query where id is 7 is run through the planner, then it reads the table through the index and finds only the record the index was told of", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		executeUpdates(t, db, tx,
			"create table test (id int)",
			"create index id_index on test (id)",
			"insert into test (id) values (7)",
			"insert into test (id) values (8)",
		)
		writePastPlanner(t, db, tx, "test", 7)

		if got := selectIDs(t, db, tx, "select id from test where id = 7"); !slices.Equal(got, []int32{7}) {
			t.Errorf("the query gives back ids %v, want [7], the one record the index files under 7", got)
		}
	})
}

// As for a select, the test infers from the result that a join uses an index.
// The record with id 1, written without the planner, is in the table but not
// in the index, so a join through the index does not pair it with anything.
//
// The planner chooses the index join here because of its statistics, not
// because an index join is cheaper in general. The tables are too small for
// the statistics to count any record. The index join and the product are
// therefore both estimated at one block, and the index wins a tie. With about
// fifty records in test, the product is estimated at fewer blocks and is
// chosen. This test must be reworked if the tables grow, or if the statistics
// start to count records written in the same transaction.
func TestDBPlannerJoinsThroughIndexes(t *testing.T) {
	t.Run("given a table of oids 1 and 2 and a table indexed on id holding ids 1 and 2 put in through the planner and another id 1 written past it, when a query joining them on oid = id is run through the planner, then it joins through the index and pairs each oid with only the record the index was told of", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		executeUpdates(t, db, tx,
			"create table other (oid int)",
			"create table test (id int)",
			"create index id_index on test (id)",
			"insert into other (oid) values (1)",
			"insert into other (oid) values (2)",
			"insert into test (id) values (1)",
			"insert into test (id) values (2)",
		)
		writePastPlanner(t, db, tx, "test", 1)

		if got := selectIDs(t, db, tx, "select id, oid from other, test where oid = id"); !slices.Equal(got, []int32{1, 2}) {
			t.Errorf("the query gives back ids %v, want [1 2], one pair for each oid", got)
		}
	})
}

// A planner that only places one select over the whole query would accept a
// term on a field that no table has, and would fail only when a record is
// tested against it. A planner that places each term where its fields are
// read has no place for such a term, and reports it before any read.
func TestDBPlannerRefusesUnknownFields(t *testing.T) {
	t.Run("given a table of id, when a query where a field it does not have is 1 is planned through the planner, then planning reports ErrFieldNotFound", func(t *testing.T) {
		db, err := Open(t.TempDir(), testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		executeUpdates(t, db, tx, "create table test (id int)")

		sql := "select id from test where missing = 1"
		if _, err := db.Planner().CreateQueryPlan(sql, tx); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("CreateQueryPlan(%q) error = %v, want %v", sql, err, recordmanager.ErrFieldNotFound)
		}
	})
}

// writePastPlanner writes a record with id to tableName through a scan of the
// table only, so that no index of the table is updated.
func writePastPlanner(t *testing.T, db *DB, tx *transaction.Transaction, tableName string, id int32) {
	t.Helper()

	layout, err := db.MetadataManager().GetLayout(tx, tableName)
	if err != nil {
		t.Fatalf("GetLayout(%q) error = %v", tableName, err)
	}
	ts, err := query.NewTableScan(tx, tableName, layout)
	if err != nil {
		t.Fatalf("NewTableScan(%q) error = %v", tableName, err)
	}
	defer ts.Close()

	if err := ts.MoveToNewRecord(); err != nil {
		t.Fatalf("MoveToNewRecord() error = %v", err)
	}
	if err := ts.SetInt("id", id); err != nil {
		t.Fatalf("SetInt(%q) error = %v", "id", err)
	}
}

// countIndexed searches the index on field of tableName for value and returns
// the number of entries found.
func countIndexed(t *testing.T, db *DB, tx *transaction.Transaction, tableName, field string, value query.Constant) int {
	t.Helper()

	indexes, err := db.MetadataManager().GetIndexInfo(tx, tableName)
	if err != nil {
		t.Fatalf("GetIndexInfo(%q) error = %v", tableName, err)
	}
	info, ok := indexes[field]
	if !ok {
		t.Fatalf("table %q has no index on %s", tableName, field)
	}

	idx, err := info.Open()
	if err != nil {
		t.Fatalf("Open() of the index on %s error = %v", field, err)
	}
	defer idx.Close()

	if err := idx.MoveBeforeFirstMatch(value); err != nil {
		t.Fatalf("MoveBeforeFirstMatch(%s) error = %v", value, err)
	}

	count := 0
	for {
		ok, err := idx.MoveToNextMatch()
		if err != nil {
			t.Fatalf("MoveToNextMatch() error = %v", err)
		}
		if !ok {
			return count
		}
		count++
	}
}

// openFDs returns the number of files under dir that the test process has
// open. It skips the test on a system that does not list them under /proc.
//
// Only files under dir are counted. The other tests leave databases open, and
// the garbage collector closes their files at unpredictable times. A count of
// all open files of the process can therefore drop at any time.
func openFDs(t *testing.T, dir string) int {
	t.Helper()

	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("cannot count open files: %v", err)
	}

	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", dir, err)
	}

	count := 0
	for _, entry := range entries {
		// A file that was closed after the directory was read has no link to
		// read, and is no longer open.
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(target, root+string(filepath.Separator)) {
			count++
		}
	}

	return count
}

func TestDBClose(t *testing.T) {
	t.Run("given a database that has had a table made in it, when it is closed, then every file it opened is closed", func(t *testing.T) {
		dir := t.TempDir()
		before := openFDs(t, dir)
		db, err := Open(dir, testBlockSize, testNumBuffers)
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		tx, err := db.NewTransaction()
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		executeUpdates(t, db, tx, "create table test (id int)")
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		// If no files were open at the start, the count after Close would
		// match whether or not Close closed anything.
		if opened := openFDs(t, dir); opened <= before {
			t.Fatalf("the process has %d files open before Close, want more than the %d before Open", opened, before)
		}

		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if after := openFDs(t, dir); after != before {
			t.Errorf("the process has %d files open, want %d as before Open", after, before)
		}
	})

	// A failed Open returns no database, so the caller has nothing to close.
	// Open must therefore close the files before it returns.
	t.Run("given a block size too small for a catalog record, when a database is opened, then it fails and every file it opened is closed", func(t *testing.T) {
		const tooSmallBlockSize = 100
		dir := t.TempDir()
		before := openFDs(t, dir)

		if _, err := Open(dir, tooSmallBlockSize, testNumBuffers); err == nil {
			t.Fatal("Open() error = nil, want an error")
		}

		if after := openFDs(t, dir); after != before {
			t.Errorf("the process has %d files open, want %d as before Open", after, before)
		}
	})
}
