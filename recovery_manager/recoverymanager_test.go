package recoverymanager

import (
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const (
	testNumBuffers = 3
	testDataFile   = "test.tbl"
)

// newTestRecoveryManagerDeps builds a log manager and a buffer manager backed by
// the same file manager, which is how they are paired in a running database.
// The file manager is returned as well so that tests can inspect what actually
// reached the disk.
func newTestRecoveryManagerDeps(t *testing.T) (*filemanager.FileManager, *logmanager.LogManager, *buffermanager.BufferManager) {
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
	return fm, lm, bm
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

// lastLogRecordOnDisk reads the most recently appended record straight from the
// log file, bypassing the log manager's in-memory page so that a record that was
// only appended but never flushed is not visible.
func lastLogRecordOnDisk(t *testing.T, fm *filemanager.FileManager) LogRecord {
	t.Helper()
	numBlocks, err := fm.Length(testLogFile)
	if err != nil {
		t.Fatalf("Length() error = %v", err)
	}
	blk := filemanager.NewBlockId(testLogFile, numBlocks-1)
	p := filemanager.NewPageByBlockSize(fm.BlockSize())
	if err := fm.Read(blk, p); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	boundary := int(p.GetInt(0))
	if boundary >= fm.BlockSize() {
		t.Fatal("the last log block on disk holds no record")
	}
	rec, err := CreateLogRecord(p.GetBytes(boundary))
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
			_, lm, bm := newTestRecoveryManagerDeps(t)

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
			_, lm, bm := newTestRecoveryManagerDeps(t)

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

func TestRecoveryManagerCommit(t *testing.T) {
	// The commit record must be on disk when Commit returns, since that record
	// is what tells recovery the transaction finished. Reading the block through
	// the file manager checks exactly that: the log manager's own iterator would
	// flush the page first and hide a missing flush.
	tests := []struct {
		name  string
		txNum int
	}{
		{
			name:  "writes a commit record for txNum 1 to disk",
			txNum: 1,
		},
		{
			name:  "writes a commit record for txNum 42 to disk",
			txNum: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm := newTestRecoveryManagerDeps(t)
			rm, err := NewRecoveryManager(lm, bm, tt.txNum)
			if err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}

			if err := rm.Commit(); err != nil {
				t.Fatalf("Commit() error = %v", err)
			}

			rec := lastLogRecordOnDisk(t, fm)
			if got := rec.Op(); got != Commit {
				t.Errorf("Op() = %d, want %d (Commit)", got, Commit)
			}
			if got := rec.TxNumber(); got != tt.txNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.txNum)
			}
		})
	}
}

func TestRecoveryManagerCommitFlushesBuffers(t *testing.T) {
	const txNum = 1

	tests := []struct {
		name        string
		bufferTxNum int
		wantFlushes int
	}{
		{
			name:        "flushes a buffer modified by the committing transaction",
			bufferTxNum: txNum,
			wantFlushes: 1,
		},
		{
			name:        "leaves a buffer modified by another transaction alone",
			bufferTxNum: txNum + 1,
			wantFlushes: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm := newTestRecoveryManagerDeps(t)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			buf, err := bm.Pin(blk)
			if err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
			buf.SetModified(tt.bufferTxNum, -1)

			rm, err := NewRecoveryManager(lm, bm, txNum)
			if err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}
			before := bm.GetStats().Flushes()

			if err := rm.Commit(); err != nil {
				t.Fatalf("Commit() error = %v", err)
			}

			if got := bm.GetStats().Flushes() - before; got != tt.wantFlushes {
				t.Errorf("Flushes() increased by %d, want %d", got, tt.wantFlushes)
			}
		})
	}
}
