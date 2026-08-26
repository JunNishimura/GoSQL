package transaction

import (
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	recoverymanager "github.com/JunNishimura/GoSQL/recovery_manager"
)

const (
	testLogFile    = "test.log"
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
