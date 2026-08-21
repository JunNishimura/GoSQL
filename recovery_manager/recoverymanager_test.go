package recoverymanager

import (
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const testNumBuffers = 3

// newTestRecoveryManagerDeps builds a log manager and a buffer manager backed by
// the same file manager, which is how they are paired in a running database.
func newTestRecoveryManagerDeps(t *testing.T) (*logmanager.LogManager, *buffermanager.BufferManager) {
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
	return lm, bm
}

// lastLogRecord reads back the most recently appended record, since the log
// iterator walks the log backwards.
func lastLogRecord(t *testing.T, lm *logmanager.LogManager) LogRecord {
	t.Helper()
	it, err := lm.Iterator()
	if err != nil {
		t.Fatalf("Iterator() error = %v", err)
	}
	if !it.HasNext() {
		t.Fatal("HasNext() = false, want true")
	}
	bytes, err := it.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	rec, err := CreateLogRecord(bytes)
	if err != nil {
		t.Fatalf("CreateLogRecord() error = %v", err)
	}
	return rec
}

func TestNewRecoveryManager(t *testing.T) {
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
			txNum: 123456,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lm, bm := newTestRecoveryManagerDeps(t)

			rm, err := NewRecoveryManager(lm, bm, tt.txNum)
			if err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}

			if rm.logManager != lm {
				t.Error("logManager is not the log manager passed to the constructor")
			}
			if rm.bufferManager != bm {
				t.Error("bufferManager is not the buffer manager passed to the constructor")
			}
			if rm.txNum != tt.txNum {
				t.Errorf("txNum = %d, want %d", rm.txNum, tt.txNum)
			}
		})
	}
}

func TestNewRecoveryManagerWritesStartRecord(t *testing.T) {
	// The start record must be on the log by the time the constructor returns,
	// so that recovery can tell which transactions were in progress.
	tests := []struct {
		name  string
		txNum int
	}{
		{
			name:  "appends a start record for txNum 1",
			txNum: 1,
		},
		{
			name:  "appends a start record for txNum 42",
			txNum: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lm, bm := newTestRecoveryManagerDeps(t)

			if _, err := NewRecoveryManager(lm, bm, tt.txNum); err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}

			rec := lastLogRecord(t, lm)
			if got := rec.Op(); got != Start {
				t.Errorf("Op() = %d, want %d (Start)", got, Start)
			}
			if got := rec.TxNumber(); got != tt.txNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.txNum)
			}
		})
	}
}
