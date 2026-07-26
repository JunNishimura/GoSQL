package logmanager

import (
	"testing"

	"github.com/JunNishimura/GoSQL/file_manager"
)

const (
	testBlockSize = 400
	testLogFile   = "test.log"
)

func newTestFileManager(t *testing.T) *filemanager.FileManager {
	t.Helper()
	dir := t.TempDir()
	fm, err := filemanager.NewFileManager(dir, testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	return fm
}

func TestNewLogManager(t *testing.T) {
	tests := []struct {
		name           string
		existingBlocks int
		wantBlkNum     int
		wantLength     int
	}{
		{
			name:           "appends a new block when log file is empty",
			existingBlocks: 0,
			wantBlkNum:     0,
			wantLength:     1,
		},
		{
			name:           "reuses the last block when log file already has blocks",
			existingBlocks: 3,
			wantBlkNum:     2,
			wantLength:     3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			for i := 0; i < tt.existingBlocks; i++ {
				if _, err := fm.Append(testLogFile); err != nil {
					t.Fatalf("Append() error = %v", err)
				}
			}

			lm, err := NewLogManager(fm, testLogFile)
			if err != nil {
				t.Fatalf("NewLogManager() error = %v", err)
			}

			if lm.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", lm.fileManager, fm)
			}
			if lm.logFile != testLogFile {
				t.Errorf("logFile = %q, want %q", lm.logFile, testLogFile)
			}
			if lm.currentBlock == nil {
				t.Fatal("currentBlock is nil, want non-nil")
			}
			if lm.currentBlock.Number() != tt.wantBlkNum {
				t.Errorf("currentBlock.Number() = %d, want %d", lm.currentBlock.Number(), tt.wantBlkNum)
			}
			if lm.latestLSN != 0 {
				t.Errorf("latestLSN = %d, want 0", lm.latestLSN)
			}
			if lm.lastSavedLSN != 0 {
				t.Errorf("lastSavedLSN = %d, want 0", lm.lastSavedLSN)
			}

			length, err := fm.Length(testLogFile)
			if err != nil {
				t.Fatalf("Length() error = %v", err)
			}
			if length != tt.wantLength {
				t.Errorf("Length() = %d, want %d", length, tt.wantLength)
			}
		})
	}
}

func TestNewLogManagerLoadsLastBlockContent(t *testing.T) {
	fm := newTestFileManager(t)

	var lastBlk *filemanager.BlockId
	for i := 0; i < 2; i++ {
		blk, err := fm.Append(testLogFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		lastBlk = blk
	}

	wantPage := filemanager.NewPageByBlockSize(testBlockSize)
	if err := wantPage.SetInt(0, 123); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	if err := fm.Write(lastBlk, wantPage); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	lm, err := NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}

	if got := lm.logPage.GetInt(0); got != 123 {
		t.Errorf("logPage.GetInt(0) = %d, want 123", got)
	}
}

func TestAppendNewBlock(t *testing.T) {
	tests := []struct {
		name             string
		additionalAppend int
		wantBlkNum       int
	}{
		{
			name:             "returns blkNum 1 after the initial block created by NewLogManager",
			additionalAppend: 1,
			wantBlkNum:       1,
		},
		{
			name:             "returns blkNum 2 after two additional appends",
			additionalAppend: 2,
			wantBlkNum:       2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			lm, err := NewLogManager(fm, testLogFile)
			if err != nil {
				t.Fatalf("NewLogManager() error = %v", err)
			}

			var blk *filemanager.BlockId
			for i := 0; i < tt.additionalAppend; i++ {
				blk, err = lm.appendNewBlock()
				if err != nil {
					t.Fatalf("appendNewBlock() error = %v", err)
				}
			}

			if blk.Number() != tt.wantBlkNum {
				t.Errorf("Number() = %d, want %d", blk.Number(), tt.wantBlkNum)
			}
			if blk.FileName() != testLogFile {
				t.Errorf("FileName() = %q, want %q", blk.FileName(), testLogFile)
			}

			readPage := filemanager.NewPageByBlockSize(testBlockSize)
			if err := fm.Read(blk, readPage); err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got := readPage.GetInt(0); got != int32(testBlockSize) {
				t.Errorf("GetInt(0) = %d, want %d", got, testBlockSize)
			}
		})
	}
}

func TestFlush(t *testing.T) {
	tests := []struct {
		name             string
		lastSavedLSN     int
		latestLSN        int
		lsn              int
		wantFlushed      bool
		wantLastSavedLSN int
	}{
		{
			name:             "flushes when lsn is greater than lastSavedLSN",
			lastSavedLSN:     0,
			latestLSN:        5,
			lsn:              3,
			wantFlushed:      true,
			wantLastSavedLSN: 5,
		},
		{
			name:             "does not flush when lsn equals lastSavedLSN",
			lastSavedLSN:     3,
			latestLSN:        5,
			lsn:              3,
			wantFlushed:      false,
			wantLastSavedLSN: 3,
		},
		{
			name:             "does not flush when lsn is less than lastSavedLSN",
			lastSavedLSN:     5,
			latestLSN:        7,
			lsn:              3,
			wantFlushed:      false,
			wantLastSavedLSN: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			lm, err := NewLogManager(fm, testLogFile)
			if err != nil {
				t.Fatalf("NewLogManager() error = %v", err)
			}

			if err := lm.logPage.SetInt(4, 999); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
			lm.lastSavedLSN = tt.lastSavedLSN
			lm.latestLSN = tt.latestLSN

			if err := lm.Flush(tt.lsn); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			if lm.lastSavedLSN != tt.wantLastSavedLSN {
				t.Errorf("lastSavedLSN = %d, want %d", lm.lastSavedLSN, tt.wantLastSavedLSN)
			}

			readPage := filemanager.NewPageByBlockSize(testBlockSize)
			if err := fm.Read(lm.currentBlock, readPage); err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			gotFlushed := readPage.GetInt(4) == 999
			if gotFlushed != tt.wantFlushed {
				t.Errorf("flushed to disk = %v, want %v", gotFlushed, tt.wantFlushed)
			}
		})
	}
}
