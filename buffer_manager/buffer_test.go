package buffermanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const (
	testLogFile   = "test.log"
	testDataFile  = "test.tbl"
	testBlockSize = 400
)

func newTestManagers(t *testing.T, blockSize int) (*filemanager.FileManager, *logmanager.LogManager) {
	t.Helper()

	fm, err := filemanager.NewFileManager(t.TempDir(), blockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	return fm, lm
}

func TestNewBuffer(t *testing.T) {
	tests := []struct {
		name      string
		blockSize int
	}{
		{
			name:      "it starts unpinned, unmodified, on no block, with a page of the block size",
			blockSize: 400,
		},
		{
			name:      "given a smaller block size, the page it allocates is that size rather than a fixed one",
			blockSize: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, tt.blockSize)

			buf := NewBuffer(fm, lm)

			if buf.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", buf.fileManager, fm)
			}
			if buf.logManager != lm {
				t.Errorf("logManager = %v, want %v", buf.logManager, lm)
			}
			if buf.contents == nil {
				t.Fatal("contents is nil, want non-nil")
			}
			if buf.pins != 0 {
				t.Errorf("pins = %d, want 0", buf.pins)
			}
			if buf.txNum != -1 {
				t.Errorf("txNum = %d, want -1", buf.txNum)
			}
			if buf.lsn != -1 {
				t.Errorf("lsn = %d, want -1", buf.lsn)
			}
			if buf.blk != nil {
				t.Errorf("blk = %v, want nil", buf.blk)
			}

			// Page keeps its buffer unexported, so the size is verified through
			// the bounds check of SetInt at the last writable offset and one past it.
			if err := buf.contents.SetInt(tt.blockSize-4, 1); err != nil {
				t.Errorf("SetInt(%d) error = %v, want nil", tt.blockSize-4, err)
			}
			if err := buf.contents.SetInt(tt.blockSize-3, 1); err == nil {
				t.Errorf("SetInt(%d) error = nil, want out of bounds error", tt.blockSize-3)
			}
		})
	}
}

func TestBlock(t *testing.T) {
	tests := []struct {
		name string
		// blkNum names which of the appended blocks the buffer is assigned to.
		blkNum int
	}{
		{
			name:   "given a buffer assigned to a block, it reports that block",
			blkNum: 0,
		},
		{
			name:   "given a buffer assigned twice, it reports the block it was last given",
			blkNum: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			blks := make([]*filemanager.BlockId, 2)
			for i := range blks {
				blk, err := fm.Append(testDataFile)
				if err != nil {
					t.Fatalf("Append() error = %v", err)
				}
				blks[i] = blk
			}

			buf := NewBuffer(fm, lm)
			for i := 0; i <= tt.blkNum; i++ {
				if err := buf.assignToBlock(blks[i]); err != nil {
					t.Fatalf("assignToBlock() error = %v", err)
				}
			}

			got := buf.Block()
			if got == nil {
				t.Fatal("Block() = nil, want non-nil")
			}
			if !got.Equals(blks[tt.blkNum]) {
				t.Errorf("Block() = %v, want %v", got, blks[tt.blkNum])
			}
		})
	}

	// A buffer that has never been assigned holds no block, and callers such as
	// the recovery manager have to be able to tell that apart from a real one.
	t.Run("given a buffer that has never been assigned a block, it reports none rather than a block of its own", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)

		buf := NewBuffer(fm, lm)

		if got := buf.Block(); got != nil {
			t.Errorf("Block() = %v, want nil", got)
		}
	})
}

func TestContents(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  int32
	}{
		{
			name:   "given a block holding a value at its start, the page it returns reads that value",
			offset: 0,
			value:  777,
		},
		{
			name:   "given a value further into the block, the page it returns reads it from there",
			offset: 64,
			value:  -12345,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			page := filemanager.NewPageByBlockSize(testBlockSize)
			if err := page.SetInt(tt.offset, tt.value); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
			if err := fm.Write(blk, page); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			buf := NewBuffer(fm, lm)
			if err := buf.assignToBlock(blk); err != nil {
				t.Fatalf("assignToBlock() error = %v", err)
			}

			if got := buf.Contents().GetInt(tt.offset); got != tt.value {
				t.Errorf("Contents().GetInt(%d) = %d, want %d", tt.offset, got, tt.value)
			}
		})
	}

	// Contents must hand out the buffer's own page rather than a copy. Callers
	// such as an undo restoring a pre-image write through it and expect flush
	// to put that write on disk; a copy would silently discard the change.
	t.Run("when the page it returned is written to and the buffer is flushed, then the write reaches the disk", func(t *testing.T) {
		const (
			txNum         = 1
			restoredValue = 555
		)

		fm, lm := newTestManagers(t, testBlockSize)
		blk, err := fm.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		buf := NewBuffer(fm, lm)
		if err := buf.assignToBlock(blk); err != nil {
			t.Fatalf("assignToBlock() error = %v", err)
		}

		if err := buf.Contents().SetInt(0, restoredValue); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		buf.SetModified(txNum, appendLogRecord(t, lm))

		if err := buf.flush(); err != nil {
			t.Fatalf("flush() error = %v", err)
		}

		readPage := filemanager.NewPageByBlockSize(testBlockSize)
		if err := fm.Read(blk, readPage); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got := readPage.GetInt(0); got != restoredValue {
			t.Errorf("value on disk = %d, want %d (Contents returned a copy)", got, restoredValue)
		}
	})
}

func TestPin(t *testing.T) {
	tests := []struct {
		name     string
		pinCalls int
		wantPins int
	}{
		{
			name:     "given an unpinned buffer, one pin makes it held once",
			pinCalls: 1,
			wantPins: 1,
		},
		{
			name:     "when a buffer is pinned three times, then it counts three holds rather than one",
			pinCalls: 3,
			wantPins: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			buf := NewBuffer(fm, lm)

			for i := 0; i < tt.pinCalls; i++ {
				buf.pin()
			}

			if buf.pins != tt.wantPins {
				t.Errorf("pins = %d, want %d", buf.pins, tt.wantPins)
			}
		})
	}
}

func TestSetModified(t *testing.T) {
	type modification struct {
		txNum int
		lsn   int
	}

	tests := []struct {
		name          string
		modifications []modification
		wantTxNum     int
		wantLSN       int
	}{
		{
			name:          "given a logged change, it records both the transaction and the lsn",
			modifications: []modification{{txNum: 1, lsn: 5}},
			wantTxNum:     1,
			wantLSN:       5,
		},
		{
			name:          "given an lsn of zero, it is recorded rather than read as no lsn at all",
			modifications: []modification{{txNum: 1, lsn: 0}},
			wantTxNum:     1,
			wantLSN:       0,
		},
		{
			name:          "leaves lsn at its initial value when lsn is negative",
			modifications: []modification{{txNum: 1, lsn: -1}},
			wantTxNum:     1,
			wantLSN:       -1,
		},
		{
			name:          "keeps the previously recorded lsn when a later call passes a negative lsn",
			modifications: []modification{{txNum: 1, lsn: 5}, {txNum: 2, lsn: -1}},
			wantTxNum:     2,
			wantLSN:       5,
		},
		{
			name:          "overwrites the previously recorded lsn when a later call passes a larger lsn",
			modifications: []modification{{txNum: 1, lsn: 5}, {txNum: 2, lsn: 9}},
			wantTxNum:     2,
			wantLSN:       9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			buf := NewBuffer(fm, lm)

			for _, m := range tt.modifications {
				buf.SetModified(m.txNum, m.lsn)
			}

			if buf.txNum != tt.wantTxNum {
				t.Errorf("txNum = %d, want %d", buf.txNum, tt.wantTxNum)
			}
			if buf.lsn != tt.wantLSN {
				t.Errorf("lsn = %d, want %d", buf.lsn, tt.wantLSN)
			}
		})
	}
}

func TestIsPinned(t *testing.T) {
	tests := []struct {
		name       string
		pinCalls   int
		unpinCalls int
		want       bool
	}{
		{
			name:       "returns false when the buffer has never been pinned",
			pinCalls:   0,
			unpinCalls: 0,
			want:       false,
		},
		{
			name:       "returns true when the buffer is pinned once",
			pinCalls:   1,
			unpinCalls: 0,
			want:       true,
		},
		{
			name:       "returns true when one of two pins is still held",
			pinCalls:   2,
			unpinCalls: 1,
			want:       true,
		},
		{
			name:       "returns false when every pin is released",
			pinCalls:   2,
			unpinCalls: 2,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			buf := NewBuffer(fm, lm)

			for i := 0; i < tt.pinCalls; i++ {
				buf.pin()
			}
			for i := 0; i < tt.unpinCalls; i++ {
				buf.unpin()
			}

			if got := buf.isPinned(); got != tt.want {
				t.Errorf("isPinned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsModified(t *testing.T) {
	tests := []struct {
		name       string
		txNum      int
		flushAfter bool
		want       bool
	}{
		{
			name:  "returns false when the buffer has never been modified",
			txNum: -1,
			want:  false,
		},
		{
			name:  "returns true when the buffer is modified by transaction 0",
			txNum: 0,
			want:  true,
		},
		{
			name:  "returns true when the buffer is modified by a positive transaction",
			txNum: 3,
			want:  true,
		},
		{
			name:       "returns false once the modified buffer has been flushed",
			txNum:      3,
			flushAfter: true,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			buf := NewBuffer(fm, lm)
			buf.blk = blk
			buf.lsn = appendLogRecord(t, lm)
			buf.txNum = tt.txNum

			if tt.flushAfter {
				if err := buf.flush(); err != nil {
					t.Fatalf("flush() error = %v", err)
				}
			}

			if got := buf.isModified(); got != tt.want {
				t.Errorf("isModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnpin(t *testing.T) {
	tests := []struct {
		name       string
		pinCalls   int
		unpinCalls int
		wantPins   int
	}{
		{
			name:       "decrements pins back to 0 when the only pin is released",
			pinCalls:   1,
			unpinCalls: 1,
			wantPins:   0,
		},
		{
			name:       "decrements pins to 1 when two of three pins are released",
			pinCalls:   3,
			unpinCalls: 2,
			wantPins:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			buf := NewBuffer(fm, lm)

			for i := 0; i < tt.pinCalls; i++ {
				buf.pin()
			}
			for i := 0; i < tt.unpinCalls; i++ {
				buf.unpin()
			}

			if buf.pins != tt.wantPins {
				t.Errorf("pins = %d, want %d", buf.pins, tt.wantPins)
			}
		})
	}
}

// appendLogRecord writes a record into the log page kept in memory and returns
// its LSN. The record only reaches disk once the log manager is flushed.
func appendLogRecord(t *testing.T, lm *logmanager.LogManager) int {
	t.Helper()

	lsn, err := lm.Append([]byte("record"))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	return lsn
}

// logFlushed reports whether the log manager has written its page to disk, which
// shows up as a boundary smaller than the block size in block 0 of the log file.
func logFlushed(t *testing.T, fm *filemanager.FileManager) bool {
	t.Helper()

	page := filemanager.NewPageByBlockSize(testBlockSize)
	if err := fm.Read(filemanager.NewBlockId(testLogFile, 0), page); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return int(page.GetInt(0)) < testBlockSize
}

func TestFlush(t *testing.T) {
	const modifiedValue = 999

	tests := []struct {
		name        string
		txNum       int
		wantWritten bool
	}{
		{
			name:        "keeps the block on disk untouched when txNum is negative",
			txNum:       -1,
			wantWritten: false,
		},
		{
			name:        "writes the page to disk when txNum is 0",
			txNum:       0,
			wantWritten: true,
		},
		{
			name:        "writes the page to disk when txNum is positive",
			txNum:       3,
			wantWritten: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			blk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			buf := NewBuffer(fm, lm)
			buf.blk = blk
			buf.lsn = appendLogRecord(t, lm)
			buf.txNum = tt.txNum
			if err := buf.contents.SetInt(0, modifiedValue); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			if err := buf.flush(); err != nil {
				t.Fatalf("flush() error = %v", err)
			}

			readPage := filemanager.NewPageByBlockSize(testBlockSize)
			if err := fm.Read(blk, readPage); err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if gotWritten := readPage.GetInt(0) == modifiedValue; gotWritten != tt.wantWritten {
				t.Errorf("block written to disk = %v, want %v", gotWritten, tt.wantWritten)
			}

			// The log record must reach disk before the modified page does.
			if gotLogFlushed := logFlushed(t, fm); gotLogFlushed != tt.wantWritten {
				t.Errorf("log flushed = %v, want %v", gotLogFlushed, tt.wantWritten)
			}

			if buf.txNum != -1 {
				t.Errorf("txNum = %d, want -1", buf.txNum)
			}
		})
	}
}

func TestAssignToBlock(t *testing.T) {
	const (
		modifiedValue = 999
		newBlockValue = 777
	)

	tests := []struct {
		name            string
		txNum           int
		pins            int
		wantOldBlkFlush bool
	}{
		{
			name:            "discards the unmodified page and loads the new block",
			txNum:           -1,
			pins:            3,
			wantOldBlkFlush: false,
		},
		{
			name:            "flushes the modified page before loading the new block",
			txNum:           1,
			pins:            3,
			wantOldBlkFlush: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			oldBlk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			newBlk, err := fm.Append(testDataFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			newBlkPage := filemanager.NewPageByBlockSize(testBlockSize)
			if err := newBlkPage.SetInt(0, newBlockValue); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
			if err := fm.Write(newBlk, newBlkPage); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			buf := NewBuffer(fm, lm)
			buf.blk = oldBlk
			buf.lsn = appendLogRecord(t, lm)
			buf.txNum = tt.txNum
			buf.pins = tt.pins
			if err := buf.contents.SetInt(0, modifiedValue); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			if err := buf.assignToBlock(newBlk); err != nil {
				t.Fatalf("assignToBlock() error = %v", err)
			}

			if buf.blk != newBlk {
				t.Errorf("blk = %v, want %v", buf.blk, newBlk)
			}
			if got := buf.contents.GetInt(0); got != newBlockValue {
				t.Errorf("contents.GetInt(0) = %d, want %d", got, newBlockValue)
			}
			if buf.pins != 0 {
				t.Errorf("pins = %d, want 0", buf.pins)
			}
			if buf.txNum != -1 {
				t.Errorf("txNum = %d, want -1", buf.txNum)
			}

			oldBlkPage := filemanager.NewPageByBlockSize(testBlockSize)
			if err := fm.Read(oldBlk, oldBlkPage); err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if gotFlushed := oldBlkPage.GetInt(0) == modifiedValue; gotFlushed != tt.wantOldBlkFlush {
				t.Errorf("old block written to disk = %v, want %v", gotFlushed, tt.wantOldBlkFlush)
			}
		})
	}
}
