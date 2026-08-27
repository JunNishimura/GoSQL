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

// newTestDeps builds the pieces a transaction is given: the ones it keeps, and
// the log manager and lock table it only needs to build its own managers.
func newTestDeps(t *testing.T) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager, *concurrencymanager.LockTable) {
	t.Helper()
	fm, err := filemanager.NewFileManager(t.TempDir(), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	bm, err := buffermanager.NewBufferManager(fm, lm, testNumBuffers)
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
			name:  "keeps txNum 1 for the transaction it serves",
			txNum: 1,
		},
		{
			name:  "keeps a multi-digit txNum without truncation",
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
}

// The start record is what puts the transaction on the log from the moment it
// begins. It is also the only thing that shows txNum reached the recovery
// manager, whose own copy is not reachable from here.
func TestNewTransactionWritesStartRecord(t *testing.T) {
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
}

// Transactions share the pool and the lock table, which is how they coordinate,
// but each tracks its own pins and locks. Sharing those would let one
// transaction release what another one holds.
func TestNewTransactionKeepsItsOwnPinsAndLocks(t *testing.T) {
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
}

// appendBlock adds a block to the data file so that a transaction has something
// real to pin.
func appendBlock(t *testing.T, fm *filemanager.FileManager) *filemanager.BlockId {
	t.Helper()
	blk, err := fm.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	return blk
}

// newTestTransaction returns a started transaction along with the pieces a test
// needs to check what it did: the file to read blocks back from, the log to
// find its records in, and the pool to flush through.
func newTestTransaction(t *testing.T) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager, *Transaction) {
	t.Helper()
	fm, lm, bm, lt := newTestDeps(t)
	tx, err := NewTransaction(fm, lm, bm, lt, 1)
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}
	return fm, lm, bm, tx
}

// lastLogRecord rebuilds the most recently appended record, since the log
// iterator walks the log backwards.
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

// undonePage applies a record's pre-image to an empty page, which is how a test
// reads the value the record captured.
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

// Pinning has to go through the transaction's own list rather than straight to
// the pool, or the pin would not be released when the transaction ends.
func TestTransactionPin(t *testing.T) {
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
}

func TestTransactionUnpin(t *testing.T) {
	fm, _, _, tx := newTestTransaction(t)
	blk := appendBlock(t, fm)
	if err := tx.Pin(blk); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}

	tx.Unpin(blk)

	if got := tx.buffers.Buffer(blk); got != nil {
		t.Errorf("the transaction still records %v, want the pin to be gone", got)
	}
}

// A pin that failed must be reported rather than swallowed, and must leave
// nothing recorded for the transaction to release later.
func TestTransactionPinReportsAFailure(t *testing.T) {
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
}

func TestTransactionSetAndGet(t *testing.T) {
	tests := []struct {
		name string
		set  func(*Transaction, *filemanager.BlockId) error
		get  func(*Transaction, *filemanager.BlockId) (any, error)
		want any
	}{
		{
			name: "round trips an int",
			set: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetInt(blk, 0, 99)
			},
			get: func(tx *Transaction, blk *filemanager.BlockId) (any, error) {
				return tx.GetInt(blk, 0)
			},
			want: int32(99),
		},
		{
			name: "round trips a string",
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

// The record has to capture what the value was, which means logging it before
// the write. Writing first would log the new value and make undo a no-op.
func TestTransactionSetIntLogsThePreImage(t *testing.T) {
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
}

func TestTransactionSetStringLogsThePreImage(t *testing.T) {
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
}

// The buffer has to be stamped with this transaction's number, or the pool will
// not write it out when the transaction commits and the change is lost.
func TestTransactionSetIntMarksTheBufferModified(t *testing.T) {
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
}

// Reading or writing a block the transaction never pinned is a bug in the
// caller. There is no buffer to work through, so it has to be reported rather
// than reaching for whatever the pool happens to hold.
func TestTransactionValueAccessWithoutPinning(t *testing.T) {
	tests := []struct {
		name string
		use  func(*Transaction, *filemanager.BlockId) error
	}{
		{
			name: "GetInt reports that the block is not pinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				_, err := tx.GetInt(blk, 0)
				return err
			},
		},
		{
			name: "SetInt reports that the block is not pinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				return tx.SetInt(blk, 0, 99)
			},
		},
		{
			name: "GetString reports that the block is not pinned",
			use: func(tx *Transaction, blk *filemanager.BlockId) error {
				_, err := tx.GetString(blk, 0)
				return err
			},
		},
		{
			name: "SetString reports that the block is not pinned",
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

// A write takes an exclusive lock, so nobody else may read the block until this
// transaction releases it.
func TestTransactionSetIntKeepsOtherTransactionsOut(t *testing.T) {
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
	for _, tx := range []*Transaction{writer, reader} {
		if err := tx.Pin(blk); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
	}
	if err := writer.SetInt(blk, 0, 99); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := reader.GetInt(blk, 0)
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("GetInt() returned %v while the block was locked for writing, want it to wait", err)
	case <-time.After(50 * time.Millisecond):
		// Still waiting, which is the expected behaviour.
	}

	writer.concurrencyManager.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("GetInt() did not return after the writer released its locks")
	}
}

// A read takes a shared lock, so several transactions may read the same block
// at once. An exclusive lock here would serialise readers for no reason.
func TestTransactionGetIntLetsAnotherTransactionRead(t *testing.T) {
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

	done := make(chan error, 1)
	go func() {
		_, err := second.GetInt(blk, 0)
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("GetInt() blocked while another transaction was only reading")
	}
}
