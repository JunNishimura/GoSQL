package transaction

import (
	"errors"
	"testing"
	"time"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
)

const (
	testLogFile    = "test.log"
	testDataFile   = "test.tbl"
	testBlockSize  = 400
	testNumBuffers = 3
)

// newTestDeps builds the dependencies of a transaction: the ones it keeps,
// and the log manager and lock table that it only uses to create its own
// managers.
func newTestDeps(t *testing.T) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager, *concurrencymanager.LockTable) {
	t.Helper()
	return newTestDepsWithPool(t, testNumBuffers)
}

// newTestDepsWithPool is newTestDeps with a pool of a known size, for tests
// that need to detect that a transaction holds a buffer.
func newTestDepsWithPool(t *testing.T, numBuffers int) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager, *concurrencymanager.LockTable) {
	t.Helper()
	fm, err := filemanager.NewFileManager(t.TempDir(), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	bm, err := buffermanager.NewBufferManager(fm, lm, numBuffers)
	if err != nil {
		t.Fatalf("NewBufferManager() error = %v", err)
	}
	return fm, lm, bm, concurrencymanager.NewLockTable()
}

func TestNewTransaction(t *testing.T) {
	tests := []struct {
		name  string
		txNum int
	}{
		{
			name:  "it keeps the transaction number it was given and builds a manager of each kind",
			txNum: 1,
		},
		{
			name:  "given a transaction number of more than one digit, it keeps the whole of it",
			txNum: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm, lt := newTestDeps(t)

			tx, err := NewTransaction(fm, lm, bm, lt, tt.txNum)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}

			if tx == nil {
				t.Fatal("NewTransaction() = nil, want non-nil")
			}
			if tx.txNum != tt.txNum {
				t.Errorf("txNum = %d, want %d", tx.txNum, tt.txNum)
			}
			if tx.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", tx.fileManager, fm)
			}
			if tx.bufferManager != bm {
				t.Errorf("bufferManager = %v, want %v", tx.bufferManager, bm)
			}
			if tx.recoveryManager == nil {
				t.Error("recoveryManager is nil, want non-nil")
			}
			if tx.concurrencyManager == nil {
				t.Error("concurrencyManager is nil, want non-nil")
			}
			if tx.buffers == nil {
				t.Error("buffers is nil, want non-nil")
			}
		})
	}

	// The start record puts the transaction on the log from the moment it
	// begins. It is also the only evidence that txNum reached the recovery
	// manager, whose own copy is not accessible from here.
	t.Run("it puts a start record on the log carrying the transaction number", func(t *testing.T) {
		const txNum = 7

		fm, lm, bm, lt := newTestDeps(t)

		if _, err := NewTransaction(fm, lm, bm, lt, txNum); err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		it, err := lm.Iterator()
		if err != nil {
			t.Fatalf("Iterator() error = %v", err)
		}
		if !it.HasNext() {
			t.Fatal("HasNext() = false, want a start record")
		}
		bytes, err := it.Next()
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		rec, err := recoverymanager.CreateLogRecord(bytes)
		if err != nil {
			t.Fatalf("CreateLogRecord() error = %v", err)
		}

		if got := rec.Op(); got != recoverymanager.Start {
			t.Errorf("Op() = %d, want %d (Start)", got, recoverymanager.Start)
		}
		if got := rec.TxNumber(); got != txNum {
			t.Errorf("TxNumber() = %d, want %d", got, txNum)
		}
	})

	// Transactions share the pool and the lock table, and coordinate through
	// them. Each transaction tracks its own pins and locks. If these were
	// shared, one transaction could release what another one holds.
	t.Run("given two transactions over the same pool and lock table, each keeps its own pins and locks", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)

		first, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		second, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		if first.bufferManager != second.bufferManager {
			t.Error("the two transactions point at different buffer managers, want the same one")
		}
		if first.buffers == second.buffers {
			t.Error("the two transactions share a buffer list, want one each")
		}
		if first.concurrencyManager == second.concurrencyManager {
			t.Error("the two transactions share a concurrency manager, want one each")
		}
		if first.recoveryManager == second.recoveryManager {
			t.Error("the two transactions share a recovery manager, want one each")
		}
	})
}

// appendBlock adds a block to the data file, so that a transaction has a real
// block to pin.
func appendBlock(t *testing.T, fm *filemanager.FileManager) *filemanager.BlockId {
	t.Helper()
	blk, err := fm.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	return blk
}

// newTestTransaction returns a started transaction and the components that a
// test needs to check its effects: the file manager to read blocks back, the
// log manager to find its records, and the buffer manager to flush through.
func newTestTransaction(t *testing.T) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager, *Transaction) {
	t.Helper()
	fm, lm, bm, lt := newTestDeps(t)
	tx, err := NewTransaction(fm, lm, bm, lt, 1)
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	return fm, lm, bm, tx
}

// lastLogRecord returns the most recently appended record. The log iterator
// reads the log backwards, so this is the first record it returns.
func lastLogRecord(t *testing.T, lm *logmanager.LogManager) recoverymanager.LogRecord {
	t.Helper()
	it, err := lm.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}
	if !it.HasNext() {
		t.Fatal("HasNext() = false, want a record")
	}
	bytes, err := it.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	rec, err := recoverymanager.CreateLogRecord(bytes)
	if err != nil {
		t.Fatalf("CreateLogRecord() error = %v", err)
	}
	return rec
}

// undonePage applies the pre-image of a record to an empty page. A test uses
// it to read the value that the record captured.
func undonePage(t *testing.T, rec recoverymanager.LogRecord) *filemanager.Page {
	t.Helper()
	undoable, ok := rec.(recoverymanager.Undoable)
	if !ok {
		t.Fatalf("record %v does not implement Undoable", rec)
	}
	p := filemanager.NewPageByBlockSize(testBlockSize)
	if err := undoable.Undo(p); err != nil {
		t.Fatalf("Undo() error = %v", err)
	}
	return p
}

// runInBackground calls f in a goroutine and returns a channel that receives
// its error. A test uses it to distinguish a call that waits for a lock from
// one that returns.
func runInBackground(f func() error) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- f()
	}()
	return done
}

func assertStillWaiting(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("%s returned %v while the block was locked, want it to wait", what, err)
	case <-time.After(50 * time.Millisecond):
		// Still waiting, which is the expected behaviour.
	}
}

func assertReturns(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s error = %v", what, err)
		}
	case <-time.After(time.Second):
		t.Fatalf("%s did not return", what)
	}
}

func TestTransactionPin(t *testing.T) {
	// A pin must go through the transaction's own list and not directly to the
	// pool. Otherwise the pin would not be released when the transaction ends.
	t.Run("it records the pin against the transaction, not only in the pool", func(t *testing.T) {
		fm, _, _, tx := newTestTransaction(t)
		blk := appendBlock(t, fm)

		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		buf := tx.buffers.Buffer(blk)
		if buf == nil {
			t.Fatal("the transaction did not record the pin, want the buffer holding the block")
		}
		if got := buf.Block(); !got.Equals(blk) {
			t.Errorf("the recorded buffer holds %v, want %v", got, blk)
		}
	})

	// A failed pin must be reported, not ignored. It must also leave no record
	// that the transaction would release later.
	t.Run("given a block that cannot be read, it reports the failure and records nothing", func(t *testing.T) {
		_, _, _, tx := newTestTransaction(t)
		// The data file has no blocks, so reading this one cannot succeed.
		blk := filemanager.NewBlockId(testDataFile, 0)

		err := tx.Pin(blk)

		if err == nil {
			t.Fatal("Pin() error = nil, want an error")
		}
		if got := tx.buffers.Buffer(blk); got != nil {
			t.Errorf("the transaction recorded %v, want nothing", got)
		}
	})
}

func TestTransactionUnpin(t *testing.T) {
	t.Run("when a pinned block is unpinned, then the transaction no longer records it", func(t *testing.T) {
		fm, _, _, tx := newTestTransaction(t)
		blk := appendBlock(t, fm)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		tx.Unpin(blk)

		if got := tx.buffers.Buffer(blk); got != nil {
			t.Errorf("the transaction still records %v, want the pin to be gone", got)
		}
	})
}

func TestTransactionAvailableBuffers(t *testing.T) {
	const poolSize = 4

	tests := []struct {
		name string
		// pinsByOther is the number of blocks that another transaction pins
		// before this one calls AvailableBuffers.
		pinsByOther int
		// pinsBySelf is the number of blocks that this transaction pins first.
		pinsBySelf int
		want       int
	}{
		{
			name:        "given a pool of 4 buffers, when the transaction has pinned 2 blocks, then 2 are available",
			pinsByOther: 0,
			pinsBySelf:  2,
			want:        poolSize - 2,
		},
		{
			name:        "given a pool of 4 buffers, when another transaction has pinned 1 block, then 3 are available because the pool is shared",
			pinsByOther: 1,
			pinsBySelf:  0,
			want:        poolSize - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm, lt := newTestDepsWithPool(t, poolSize)
			other, err := NewTransaction(fm, lm, bm, lt, 1)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			tx, err := NewTransaction(fm, lm, bm, lt, 2)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}

			for range tt.pinsByOther {
				if err := other.Pin(appendBlock(t, fm)); err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}
			for range tt.pinsBySelf {
				if err := tx.Pin(appendBlock(t, fm)); err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}

			if got := tx.AvailableBuffers(); got != tt.want {
				t.Errorf("AvailableBuffers() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTransactionSetAndGet(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Transaction, *filemanager.BlockId) error
		get  func(*Transaction, *filemanager.BlockId) (any, error)
		want any
	}{
		{
			name: "when an int is written to a pinned block and read back, then it is unchanged",
			set: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetInt(blk, 0, 99)
			},
			get: func(tx *Transaction, blk *filemanager.BlockId) (any, error) {
				return tx.GetInt(blk, 0)
			},
			want: int32(99),
		},
		{
			name: "when a string is written to a pinned block and read back, then it is unchanged",
			set: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetString(blk, 0, "hello")
			},
			get: func(tx *Transaction, blk *filemanager.BlockId) (any, error) {
				return tx.GetString(blk, 0)
			},
			want: "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _, _, tx := newTestTransaction(t)
			blk := appendBlock(t, fm)
			if err := tx.Pin(blk); err != nil {
				t.Fatalf("Pin() error = %v", err)
			}

			if err := tt.set(tx, blk); err != nil {
				t.Fatalf("setting the value: %v", err)
			}
			got, err := tt.get(tx, blk)
			if err != nil {
				t.Fatalf("reading the value: %v", err)
			}

			if got != tt.want {
				t.Errorf("read back %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransactionSetInt(t *testing.T) {
	// The record must capture the previous value, so the value is logged
	// before the write. If the write came first, the record would hold the new
	// value and an undo would change nothing.
	t.Run("when a value is written over another, then the record on the log holds the one that was there before", func(t *testing.T) {
		fm, lm, _, tx := newTestTransaction(t)
		blk := appendBlock(t, fm)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := tx.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		if err := tx.SetInt(blk, 0, 100); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		rec := lastLogRecord(t, lm)
		if got := rec.Op(); got != recoverymanager.SetInt {
			t.Fatalf("Op() = %d, want %d (SetInt)", got, recoverymanager.SetInt)
		}
		if got := undonePage(t, rec).GetInt(0); got != 99 {
			t.Errorf("the logged value is %d, want 99 (the value before the write)", got)
		}
	})

	// The buffer must be marked with this transaction's number. Otherwise the
	// pool does not write it when the transaction commits, and the change is
	// lost.
	t.Run("it stamps the buffer with the transaction, so a flush of that transaction writes the block out", func(t *testing.T) {
		fm, _, bm, tx := newTestTransaction(t)
		blk := appendBlock(t, fm)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := tx.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		if err := bm.FlushAll(tx.txNum); err != nil {
			t.Fatalf("FlushAll() error = %v", err)
		}

		page := filemanager.NewPageByBlockSize(testBlockSize)
		if err := fm.Read(blk, page); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got := page.GetInt(0); got != 99 {
			t.Errorf("the block on disk holds %d, want 99", got)
		}
	})

	// A write takes an exclusive lock, so no other transaction can read the
	// block until this transaction releases it. The other transaction waits in
	// Pin: a transaction that cannot read a block must not hold a buffer for it
	// either.
	t.Run("given a block this transaction has written, another transaction waits to pin it until the lock is released", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		writer, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		reader, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk := appendBlock(t, fm)
		if err := writer.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := writer.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		done := runInBackground(func() error {
			return reader.Pin(blk)
		})

		assertStillWaiting(t, done, "Pin()")

		writer.concurrencyManager.Release()

		assertReturns(t, done, "Pin()")
	})
}

func TestTransactionSetString(t *testing.T) {
	t.Run("when a value is written over another, then the record on the log holds the one that was there before", func(t *testing.T) {
		fm, lm, _, tx := newTestTransaction(t)
		blk := appendBlock(t, fm)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := tx.SetString(blk, 0, "old"); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}

		if err := tx.SetString(blk, 0, "new"); err != nil {
			t.Fatalf("SetString() error = %v", err)
		}

		rec := lastLogRecord(t, lm)
		if got := rec.Op(); got != recoverymanager.SetString {
			t.Fatalf("Op() = %d, want %d (SetString)", got, recoverymanager.SetString)
		}
		if got := undonePage(t, rec).GetString(0); got != "old" {
			t.Errorf("the logged value is %q, want %q (the value before the write)", got, "old")
		}
	})
}

// A read or write of a block that the transaction never pinned is a bug in
// the caller. The transaction has no buffer for the block, so it must report
// an error and must not use an arbitrary buffer from the pool.
func TestTransactionValueAccessWithoutPinning(t *testing.T) {
	tests := []struct {
		name string
		use  func(*Transaction, *filemanager.BlockId) error
	}{
		{
			name: "given a block the transaction never pinned, when GetInt reads it, then it reports ErrBlockNotPinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				_, err := tx.GetInt(blk, 0)
				return err
			},
		},
		{
			name: "given a block the transaction never pinned, when SetInt writes to it, then it reports ErrBlockNotPinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetInt(blk, 0, 99)
			},
		},
		{
			name: "given a block the transaction never pinned, when GetString reads it, then it reports ErrBlockNotPinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				_, err := tx.GetString(blk, 0)
				return err
			},
		},
		{
			name: "given a block the transaction never pinned, when SetString writes to it, then it reports ErrBlockNotPinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetString(blk, 0, "hello")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _, _, tx := newTestTransaction(t)
			blk := appendBlock(t, fm)

			err := tt.use(tx, blk)

			if !errors.Is(err, ErrBlockNotPinned) {
				t.Errorf("error = %v, want %v", err, ErrBlockNotPinned)
			}
		})
	}
}

func TestTransactionGetInt(t *testing.T) {
	// A read takes a shared lock, so several transactions can read the same
	// block at the same time. An exclusive lock here would serialize readers
	// without need.
	t.Run("given a block this transaction has read, another transaction may read it too rather than waiting", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		first, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		second, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk := appendBlock(t, fm)
		for _, tx := range []*Transaction{first, second} {
			if err := tx.Pin(blk); err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
		}
		if _, err := first.GetInt(blk, 0); err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}

		done := runInBackground(func() error {
			_, err := second.GetInt(blk, 0)
			return err
		})

		assertReturns(t, done, "GetInt()")
	})
}

func TestTransactionSize(t *testing.T) {
	tests := []struct {
		name   string
		blocks int
	}{
		{
			name:   "given a file with no blocks, it reports zero",
			blocks: 0,
		},
		{
			name:   "given a file of one block, it reports one",
			blocks: 1,
		},
		{
			name:   "given a file of several blocks, it reports how many",
			blocks: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _, _, tx := newTestTransaction(t)
			for range tt.blocks {
				appendBlock(t, fm)
			}

			got, err := tx.Size(testDataFile)
			if err != nil {
				t.Fatalf("Size() error = %v", err)
			}

			if got != tt.blocks {
				t.Errorf("Size(%q) = %d, want %d", testDataFile, got, tt.blocks)
			}
		})
	}

	// Reading the length takes a shared lock, so several transactions can read
	// it at the same time.
	t.Run("given a transaction that has read the length, another may read it too rather than waiting", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		first, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		second, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if _, err := first.Size(testDataFile); err != nil {
			t.Fatalf("Size() error = %v", err)
		}

		done := runInBackground(func() error {
			_, err := second.Size(testDataFile)
			return err
		})

		assertReturns(t, done, "Size()")
	})
}

func TestTransactionAppend(t *testing.T) {
	t.Run("it returns each new block in turn, numbered from the end of the file", func(t *testing.T) {
		fm, _, _, tx := newTestTransaction(t)

		first, err := tx.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		second, err := tx.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		if got := first.Number(); got != 0 {
			t.Errorf("the first appended block is %d, want 0", got)
		}
		if got := second.Number(); got != 1 {
			t.Errorf("the second appended block is %d, want 1", got)
		}
		length, err := fm.Length(testDataFile)
		if err != nil {
			t.Fatalf("Length() error = %v", err)
		}
		if length != 2 {
			t.Errorf("the file holds %d blocks, want 2", length)
		}
	})

	// The length of a file is protected by a lock on a dummy block that does
	// not exist in the file. Append changes the length, so it takes that lock
	// exclusively. No other transaction can read the length until the
	// transaction ends.
	t.Run("given a transaction that has appended, another waits to read the length until the lock is released", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		appender, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		reader, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if _, err := appender.Append(testDataFile); err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		done := runInBackground(func() error {
			_, err := reader.Size(testDataFile)
			return err
		})

		assertStillWaiting(t, done, "Size()")

		appender.concurrencyManager.Release()

		assertReturns(t, done, "Size()")
	})

	// The dummy block that protects the length is not a block of the file,
	// so extending the file does not block reads of the existing blocks.
	t.Run("when a transaction extends a file, then the blocks already in it stay free to read", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		appender, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		reader, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk := appendBlock(t, fm)
		if err := reader.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if _, err := appender.Append(testDataFile); err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		done := runInBackground(func() error {
			_, err := reader.GetInt(blk, 0)
			return err
		})

		assertReturns(t, done, "GetInt()")
	})
}

// writeInt writes a value directly to a block on disk. It represents data
// that existed before a transaction modified the block.
func writeInt(t *testing.T, fm *filemanager.FileManager, blk *filemanager.BlockId, val int32) {
	t.Helper()
	page := filemanager.NewPageByBlockSize(testBlockSize)
	if err := page.SetInt(0, val); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	if err := fm.Write(blk, page); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func readInt(t *testing.T, fm *filemanager.FileManager, blk *filemanager.BlockId) int32 {
	t.Helper()
	page := filemanager.NewPageByBlockSize(testBlockSize)
	if err := fm.Read(blk, page); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return page.GetInt(0)
}

// The end of a transaction lets other transactions proceed. However it ends,
// its locks and its pinned buffers must be released. Otherwise the pool and
// the lock table would leak for the rest of the run.
func TestTransactionEndReleasesLocksAndPins(t *testing.T) {
	tests := []struct {
		name string
		end  func(*Transaction) error
	}{
		{
			name: "when a transaction commits, then it gives up every lock and pin it held",
			end:  (*Transaction).Commit,
		},
		{
			name: "when a transaction rolls back, then it gives up every lock and pin it held",
			end:  (*Transaction).Rollback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm, lt := newTestDeps(t)
			tx, err := NewTransaction(fm, lm, bm, lt, 1)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			other, err := NewTransaction(fm, lm, bm, lt, 2)
			if err != nil {
				t.Fatalf("NewTransaction() error = %v", err)
			}
			blk := appendBlock(t, fm)
			if err := tx.Pin(blk); err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
			if err := tx.SetInt(blk, 0, 99); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			if err := tt.end(tx); err != nil {
				t.Fatalf("ending the transaction: %v", err)
			}

			if got := tx.buffers.Buffer(blk); got != nil {
				t.Errorf("the transaction still records %v, want its pins to be gone", got)
			}
			if err := other.Pin(blk); err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
			done := runInBackground(func() error {
				_, err := other.GetInt(blk, 0)
				return err
			})
			assertReturns(t, done, "GetInt()")
		})
	}
}

func TestTransactionCommit(t *testing.T) {
	// The commit record tells recovery that the transaction finished, so a
	// transaction that reports a commit must have appended one to the log.
	t.Run("it puts a commit record on the log carrying the transaction number", func(t *testing.T) {
		const txNum = 1

		fm, lm, bm, lt := newTestDeps(t)
		tx, err := NewTransaction(fm, lm, bm, lt, txNum)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk := appendBlock(t, fm)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := tx.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		rec := lastLogRecord(t, lm)
		if got := rec.Op(); got != recoverymanager.Commit {
			t.Errorf("Op() = %d, want %d (Commit)", got, recoverymanager.Commit)
		}
		if got := rec.TxNumber(); got != txNum {
			t.Errorf("TxNumber() = %d, want %d", got, txNum)
		}
	})
}

func TestTransactionRollback(t *testing.T) {
	t.Run("when a transaction that wrote to a block rolls back, then the block holds what it did beforehand", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		tx, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		blk := appendBlock(t, fm)
		writeInt(t, fm, blk, 42)
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := tx.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		if got := readInt(t, fm, blk); got != 42 {
			t.Errorf("the block on disk holds %d, want 42 (the value before the transaction)", got)
		}
	})
}

func TestTransactionRecover(t *testing.T) {
	// Recovery runs as its own transaction and undoes the changes of every
	// transaction that the log shows as unfinished.
	t.Run("given a transaction the log shows as unfinished, its writes are undone", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)
		blk := appendBlock(t, fm)
		writeInt(t, fm, blk, 42)

		crashed, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if err := crashed.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := crashed.SetInt(blk, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		// The change reached disk before the crash, but the transaction never
		// committed, so recovery must undo it.
		if err := bm.FlushAll(1); err != nil {
			t.Fatalf("FlushAll() error = %v", err)
		}

		recoverer, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		if err := recoverer.Recover(); err != nil {
			t.Fatalf("Recover() error = %v", err)
		}

		if got := readInt(t, fm, blk); got != 42 {
			t.Errorf("the block on disk holds %d, want 42 (the value before the unfinished transaction)", got)
		}
	})
}

// Pin takes the lock before the buffer, which is the reason for locking in
// Pin. A transaction blocked on a lock holds no buffer, so contention on one
// block cannot exhaust the pool for other transactions.
//
// The writer unpins but keeps its lock. This is the normal state after a
// write: the buffer returns to the pool, and the lock is held until the
// transaction ends.
func TestTransactionPinDoesNotHoldABufferWhileWaiting(t *testing.T) {
	t.Run("given a pool of one buffer and a transaction waiting for a lock, a third transaction can still take that buffer", func(t *testing.T) {
		fm, lm, bm, lt := newTestDepsWithPool(t, 1)
		writer, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		reader, err := NewTransaction(fm, lm, bm, lt, 2)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		other, err := NewTransaction(fm, lm, bm, lt, 3)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}
		locked := appendBlock(t, fm)
		free := appendBlock(t, fm)

		if err := writer.Pin(locked); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if err := writer.SetInt(locked, 0, 99); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		writer.Unpin(locked)

		blocked := runInBackground(func() error {
			return reader.Pin(locked)
		})
		assertStillWaiting(t, blocked, "Pin()")

		// The pool holds one buffer and it is unpinned, so a transaction that
		// does not use the locked block must still be able to work.
		done := runInBackground(func() error {
			return other.Pin(free)
		})
		assertReturns(t, done, "Pin()")

		// The buffer is unpinned before the lock is released. Otherwise the
		// waiting pin would stop waiting for the lock and start waiting for a
		// buffer.
		other.Unpin(free)

		writer.concurrencyManager.Release()
		assertReturns(t, blocked, "Pin()")
	})
}

func TestTransactionBlockSize(t *testing.T) {
	t.Run("it reports the block size the database was created with", func(t *testing.T) {
		fm, lm, bm, lt := newTestDeps(t)

		tx, err := NewTransaction(fm, lm, bm, lt, 1)
		if err != nil {
			t.Fatalf("NewTransaction() error = %v", err)
		}

		if got := tx.BlockSize(); got != testBlockSize {
			t.Errorf("BlockSize() = %d, want %d", got, testBlockSize)
		}
	})
}
