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

// blockOnDisk reads a block straight from the file manager, so that a value the
// buffer pool still holds in memory does not pass for one that was written out.
func blockOnDisk(t *testing.T, fm *filemanager.FileManager, blk *filemanager.BlockId) *filemanager.Page {
	t.Helper()
	p := filemanager.NewPageByBlockSize(testBlockSize)
	if err := fm.Read(blk, p); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return p
}

// writeBlockOnDisk puts a block into the state it would be in after the
// transaction's change had already reached disk.
func writeBlockOnDisk(t *testing.T, fm *filemanager.FileManager, blk *filemanager.BlockId, set func(*filemanager.Page) error) {
	t.Helper()
	p := filemanager.NewPageByBlockSize(testBlockSize)
	if err := set(p); err != nil {
		t.Fatalf("setting up the block: %v", err)
	}
	if err := fm.Write(blk, p); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func TestRecoveryManagerRollback(t *testing.T) {
	// Like the commit record, the rollback record must be on disk when Rollback
	// returns, since that record is what tells recovery the transaction is over.
	tests := []struct {
		name  string
		txNum int
	}{
		{
			name:  "writes a rollback record for txNum 1 to disk",
			txNum: 1,
		},
		{
			name:  "writes a rollback record for txNum 42 to disk",
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

			if err := rm.Rollback(); err != nil {
				t.Fatalf("Rollback() error = %v", err)
			}

			rec := lastLogRecordOnDisk(t, fm)
			if got := rec.Op(); got != Rollback {
				t.Errorf("Op() = %d, want %d (Rollback)", got, Rollback)
			}
			if got := rec.TxNumber(); got != tt.txNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.txNum)
			}
		})
	}
}

func TestRecoveryManagerRollbackRestoresPreImage(t *testing.T) {
	const txNum = 1

	tests := []struct {
		name string
		// setCurrent puts the value the transaction wrote into the block.
		setCurrent func(*filemanager.Page) error
		// logPreImage records what the value had been before that write.
		logPreImage func(*logmanager.LogManager, *filemanager.BlockId) (int, error)
		verify      func(*testing.T, *filemanager.Page)
	}{
		{
			name:       "restores an int the transaction overwrote",
			setCurrent: func(p *filemanager.Page) error { return p.SetInt(0, 100) },
			logPreImage: func(lm *logmanager.LogManager, blk *filemanager.BlockId) (int, error) {
				return WriteSetIntRecordToLog(lm, txNum, blk, 0, 42)
			},
			verify: func(t *testing.T, p *filemanager.Page) {
				if got := p.GetInt(0); got != 42 {
					t.Errorf("GetInt(0) = %d, want 42", got)
				}
			},
		},
		{
			name:       "restores a string the transaction overwrote",
			setCurrent: func(p *filemanager.Page) error { return p.SetString(0, "new") },
			logPreImage: func(lm *logmanager.LogManager, blk *filemanager.BlockId) (int, error) {
				return WriteSetStringRecordToLog(lm, txNum, blk, 0, "old")
			},
			verify: func(t *testing.T, p *filemanager.Page) {
				if got := p.GetString(0); got != "old" {
					t.Errorf("GetString(0) = %q, want %q", got, "old")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm := newTestRecoveryManagerDeps(t)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			writeBlockOnDisk(t, fm, blk, tt.setCurrent)

			rm, err := NewRecoveryManager(lm, bm, txNum)
			if err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}
			if _, err := tt.logPreImage(lm, blk); err != nil {
				t.Fatalf("logging the pre-image: %v", err)
			}

			if err := rm.Rollback(); err != nil {
				t.Fatalf("Rollback() error = %v", err)
			}

			// Rollback flushes the transaction's buffers, so the restored value
			// must be readable straight from the file.
			tt.verify(t, blockOnDisk(t, fm, blk))
		})
	}
}

func TestRecoveryManagerRollbackLeavesOtherTransactionsAlone(t *testing.T) {
	const (
		txNum      = 1
		otherTxNum = 2
		mineOffset = 0
		theirs     = 8
	)

	fm, lm, bm := newTestRecoveryManagerDeps(t)
	blk, err := fm.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	writeBlockOnDisk(t, fm, blk, func(p *filemanager.Page) error {
		if err := p.SetInt(mineOffset, 100); err != nil {
			return err
		}
		return p.SetInt(theirs, 200)
	})

	rm, err := NewRecoveryManager(lm, bm, txNum)
	if err != nil {
		t.Fatalf("NewRecoveryManager() error = %v", err)
	}
	if _, err := WriteSetIntRecordToLog(lm, txNum, blk, mineOffset, 42); err != nil {
		t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
	}
	if _, err := WriteSetIntRecordToLog(lm, otherTxNum, blk, theirs, 84); err != nil {
		t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
	}

	if err := rm.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	p := blockOnDisk(t, fm, blk)
	if got := p.GetInt(mineOffset); got != 42 {
		t.Errorf("GetInt(%d) = %d, want 42 (own change should be undone)", mineOffset, got)
	}
	if got := p.GetInt(theirs); got != 200 {
		t.Errorf("GetInt(%d) = %d, want 200 (another transaction's change should stand)", theirs, got)
	}
}

// The walk backwards stops at the transaction's own start record: nothing
// written before a transaction began can belong to it.
func TestRecoveryManagerRollbackStopsAtItsOwnStartRecord(t *testing.T) {
	const txNum = 1

	fm, lm, bm := newTestRecoveryManagerDeps(t)
	blk, err := fm.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	writeBlockOnDisk(t, fm, blk, func(p *filemanager.Page) error { return p.SetInt(0, 100) })

	// A record carrying the same txNum, appended before the transaction starts.
	if _, err := WriteSetIntRecordToLog(lm, txNum, blk, 0, 7); err != nil {
		t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
	}

	rm, err := NewRecoveryManager(lm, bm, txNum)
	if err != nil {
		t.Fatalf("NewRecoveryManager() error = %v", err)
	}

	if err := rm.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if got := blockOnDisk(t, fm, blk).GetInt(0); got != 100 {
		t.Errorf("GetInt(0) = %d, want 100 (the record before the start record must not be undone)", got)
	}
}

func TestRecoveryManagerRecover(t *testing.T) {
	// Recovery runs under its own transaction number, which no crashed
	// transaction in the log can have used.
	const recoveryTxNum = 99

	tests := []struct {
		name string
		// writeLog replays what the log held when the database came back up.
		writeLog func(*testing.T, *logmanager.LogManager, *filemanager.BlockId)
		want     int32
	}{
		{
			name: "undoes a transaction that never finished",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 42)
			},
			want: 42,
		},
		{
			name: "leaves a committed transaction alone",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 42)
				if _, err := WriteCommitRecordToLog(lm, 1); err != nil {
					t.Fatalf("WriteCommitRecordToLog() error = %v", err)
				}
			},
			want: 100,
		},
		{
			name: "leaves a rolled back transaction alone",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 42)
				if _, err := WriteRollbackRecordToLog(lm, 1); err != nil {
					t.Fatalf("WriteRollbackRecordToLog() error = %v", err)
				}
			},
			want: 100,
		},
		{
			name: "undoes one transaction while leaving a committed one alone",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 42)
				if _, err := WriteCommitRecordToLog(lm, 1); err != nil {
					t.Fatalf("WriteCommitRecordToLog() error = %v", err)
				}
				writeSetIntRecord(t, lm, 2, blk, 7)
			},
			want: 7,
		},
		{
			name: "stops at a checkpoint record",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 42)
				if _, err := WriteCheckpointRecordToLog(lm); err != nil {
					t.Fatalf("WriteCheckpointRecordToLog() error = %v", err)
				}
			},
			want: 100,
		},
		{
			// Reading backwards is what makes this come out right: the oldest
			// pre-image has to be the one applied last.
			name: "restores the oldest pre-image when a value was overwritten twice",
			writeLog: func(t *testing.T, lm *logmanager.LogManager, blk *filemanager.BlockId) {
				writeSetIntRecord(t, lm, 1, blk, 1)
				writeSetIntRecord(t, lm, 1, blk, 2)
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm, bm := newTestRecoveryManagerDeps(t)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			writeBlockOnDisk(t, fm, blk, func(p *filemanager.Page) error { return p.SetInt(0, 100) })

			tt.writeLog(t, lm, blk)

			rm, err := NewRecoveryManager(lm, bm, recoveryTxNum)
			if err != nil {
				t.Fatalf("NewRecoveryManager() error = %v", err)
			}

			if err := rm.Recover(); err != nil {
				t.Fatalf("Recover() error = %v", err)
			}

			if got := blockOnDisk(t, fm, blk).GetInt(0); got != tt.want {
				t.Errorf("GetInt(0) = %d, want %d", got, tt.want)
			}
		})
	}
}

// writeSetIntRecord logs that txNum overwrote the int at offset 0 of blk, whose
// previous value was oldVal.
func writeSetIntRecord(t *testing.T, lm *logmanager.LogManager, txNum int, blk *filemanager.BlockId, oldVal int32) {
	t.Helper()
	if _, err := WriteSetIntRecordToLog(lm, txNum, blk, 0, oldVal); err != nil {
		t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
	}
}

// Recover ends by writing a checkpoint, which is what lets a later recovery
// stop there instead of walking the whole log again.
func TestRecoveryManagerRecoverWritesCheckpoint(t *testing.T) {
	fm, lm, bm := newTestRecoveryManagerDeps(t)
	rm, err := NewRecoveryManager(lm, bm, 99)
	if err != nil {
		t.Fatalf("NewRecoveryManager() error = %v", err)
	}

	if err := rm.Recover(); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}

	rec := lastLogRecordOnDisk(t, fm)
	if got := rec.Op(); got != Checkpoint {
		t.Errorf("Op() = %d, want %d (Checkpoint)", got, Checkpoint)
	}
}
