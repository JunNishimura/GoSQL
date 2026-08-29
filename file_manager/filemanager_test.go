package filemanager

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testBlockSize = 400
	testFileName  = "test.db"
)

func newTestFileManager(t *testing.T) (*FileManager, string) {
	t.Helper()
	dir := t.TempDir()
	fm, err := NewFileManager(dir, testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	return fm, dir
}

func TestNewFileManager(t *testing.T) {
	tests := []struct {
		name    string
		dirFunc func(t *testing.T) string
	}{
		{
			name:    "creates directory when it does not exist",
			dirFunc: func(t *testing.T) string { return t.TempDir() + "/newdir" },
		},
		{
			name:    "succeeds when directory already exists",
			dirFunc: func(t *testing.T) string { return t.TempDir() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.dirFunc(t)
			_, err := NewFileManager(dir, testBlockSize)
			if err != nil {
				t.Fatalf("NewFileManager() error = %v", err)
			}
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Error("directory does not exist after NewFileManager()")
			}
		})
	}
}

func TestWriteAndRead(t *testing.T) {
	tests := []struct {
		name              string
		blk               *BlockId
		writeData         string
		wantBlocksWritten int
		wantBlocksRead    int
	}{
		{
			name:              "reads back data written to block 0",
			blk:               NewBlockId(testFileName, 0),
			writeData:         "hello",
			wantBlocksWritten: 1,
			wantBlocksRead:    1,
		},
		{
			name:              "reads back data written to block 1 at correct offset",
			blk:               NewBlockId(testFileName, 1),
			writeData:         "block one",
			wantBlocksWritten: 1,
			wantBlocksRead:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _ := newTestFileManager(t)

			writePage := NewPageByBlockSize(testBlockSize)
			if err := writePage.SetString(0, tt.writeData); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}
			if err := fm.Write(tt.blk, writePage); err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			readPage := NewPageByBlockSize(testBlockSize)
			if err := fm.Read(tt.blk, readPage); err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if got := readPage.GetString(0); got != tt.writeData {
				t.Errorf("GetString() = %q, want %q", got, tt.writeData)
			}

			stats := fm.GetStats()
			if stats.BlocksWritten() != tt.wantBlocksWritten {
				t.Errorf("BlocksWritten() = %d, want %d", stats.BlocksWritten(), tt.wantBlocksWritten)
			}
			if stats.BlocksRead() != tt.wantBlocksRead {
				t.Errorf("BlocksRead() = %d, want %d", stats.BlocksRead(), tt.wantBlocksRead)
			}
		})
	}
}

func TestAppend(t *testing.T) {
	tests := []struct {
		name        string
		appendCount int
		wantBlkNum  int
	}{
		{
			name:        "returns blkNum 0 and correct filename on first append to empty file",
			appendCount: 1,
			wantBlkNum:  0,
		},
		{
			name:        "returns blkNum 1 on second append",
			appendCount: 2,
			wantBlkNum:  1,
		},
		{
			name:        "returns blkNum 2 on third append",
			appendCount: 3,
			wantBlkNum:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _ := newTestFileManager(t)
			var blk *BlockId
			for i := 0; i < tt.appendCount; i++ {
				var err error
				blk, err = fm.Append(testFileName)
				if err != nil {
					t.Fatalf("Append() error = %v", err)
				}
			}
			if blk.Number() != tt.wantBlkNum {
				t.Errorf("Number() = %d, want %d", blk.Number(), tt.wantBlkNum)
			}
			if blk.FileName() != testFileName {
				t.Errorf("FileName() = %q, want %q", blk.FileName(), testFileName)
			}
		})
	}
}

func TestLength(t *testing.T) {
	tests := []struct {
		name        string
		appendCount int
		want        int
	}{
		{
			name:        "returns 0 for empty file",
			appendCount: 0,
			want:        0,
		},
		{
			name:        "returns block count equal to number of appended blocks",
			appendCount: 3,
			want:        3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _ := newTestFileManager(t)
			for i := 0; i < tt.appendCount; i++ {
				if _, err := fm.Append(testFileName); err != nil {
					t.Fatalf("Append() error = %v", err)
				}
			}
			length, err := fm.Length(testFileName)
			if err != nil {
				t.Fatalf("Length() error = %v", err)
			}
			if length != tt.want {
				t.Errorf("Length() = %d, want %d", length, tt.want)
			}
		})
	}
}

// writeFirstBlock puts a value into block 0 of fileName, so that a test can tell
// one file from another by its contents.
func writeFirstBlock(t *testing.T, fm *FileManager, fileName string, value int32) {
	t.Helper()
	blk, err := fm.Append(fileName)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	page := NewPageByBlockSize(testBlockSize)
	if err := page.SetInt(0, value); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	if err := fm.Write(blk, page); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func readFirstBlock(t *testing.T, fm *FileManager, fileName string) int32 {
	t.Helper()
	page := NewPageByBlockSize(testBlockSize)
	if err := fm.Read(NewBlockId(fileName, 0), page); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	return page.GetInt(0)
}

func TestArchive(t *testing.T) {
	fm, dir := newTestFileManager(t)
	writeFirstBlock(t, fm, testFileName, 100)
	destPath := filepath.Join(dir, "archive", "old.db")

	if err := fm.Archive(testFileName, destPath); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// The destination directory is created along the way, so the caller does
	// not have to prepare it.
	archived, err := NewFileManager(filepath.Dir(destPath), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	if got := readFirstBlock(t, archived, filepath.Base(destPath)); got != 100 {
		t.Errorf("the archived file holds %d, want 100", got)
	}
}

// The manager keeps every file it has opened, so archiving has to close and
// forget the handle. Otherwise the next read of that name would follow the
// handle to the file that was moved away, and the archive would keep growing
// instead of a new file being started.
func TestArchiveLeavesTheNameFree(t *testing.T) {
	fm, dir := newTestFileManager(t)
	writeFirstBlock(t, fm, testFileName, 100)

	if err := fm.Archive(testFileName, filepath.Join(dir, "archive", "old.db")); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	got, err := fm.Length(testFileName)
	if err != nil {
		t.Fatalf("Length() error = %v", err)
	}
	if got != 0 {
		t.Errorf("Length(%q) = %d, want 0 (the name should refer to a new, empty file)", testFileName, got)
	}
}

// Archiving exists to keep the old file, so it must not quietly replace one
// that is already there.
func TestArchiveDoesNotReplaceAnExistingArchive(t *testing.T) {
	fm, dir := newTestFileManager(t)
	destPath := filepath.Join(dir, "archive", "old.db")
	writeFirstBlock(t, fm, testFileName, 100)
	if err := fm.Archive(testFileName, destPath); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	writeFirstBlock(t, fm, testFileName, 200)

	err := fm.Archive(testFileName, destPath)

	if err == nil {
		t.Fatal("Archive() error = nil, want an error")
	}
	archived, err := NewFileManager(filepath.Dir(destPath), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	if got := readFirstBlock(t, archived, filepath.Base(destPath)); got != 100 {
		t.Errorf("the archived file holds %d, want 100 (the first archive must survive)", got)
	}
	if got := readFirstBlock(t, fm, testFileName); got != 200 {
		t.Errorf("the file holds %d, want 200 (a refused archive must leave it alone)", got)
	}
}
