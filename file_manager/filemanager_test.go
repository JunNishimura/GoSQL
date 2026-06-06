package filemanager

import (
	"bytes"
	"os"
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
	t.Run("creates directory when it does not exist", func(t *testing.T) {
		dir := t.TempDir() + "/newdir"
		_, err := NewFileManager(dir, testBlockSize)
		if err != nil {
			t.Fatalf("NewFileManager() error = %v", err)
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Error("directory was not created")
		}
	})

	t.Run("succeeds when directory already exists", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewFileManager(dir, testBlockSize)
		if err != nil {
			t.Fatalf("NewFileManager() error = %v", err)
		}
	})
}

func TestWriteAndRead(t *testing.T) {
	t.Run("reads back the same bytes that were written", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		blk := NewBlockId(testFileName, 0)

		writePage := NewPageByBlockSize(testBlockSize)
		writePage.SetString(0, "hello")
		writePage.SetInt(writePage.MaxLength(len("hello")), 42)
		if err := fm.Write(blk, writePage); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		readPage := NewPageByBlockSize(testBlockSize)
		if err := fm.Read(blk, readPage); err != nil {
			t.Fatalf("Read() error = %v", err)
		}

		if !bytes.Equal(writePage.contents(), readPage.contents()) {
			t.Error("Read() returned different bytes from what was written")
		}
	})

	t.Run("reads correct block when multiple blocks exist", func(t *testing.T) {
		fm, _ := newTestFileManager(t)

		blk0 := NewBlockId(testFileName, 0)
		blk1 := NewBlockId(testFileName, 1)

		page0 := NewPageByBlockSize(testBlockSize)
		page0.SetString(0, "block zero")
		if err := fm.Write(blk0, page0); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		page1 := NewPageByBlockSize(testBlockSize)
		page1.SetString(0, "block one")
		if err := fm.Write(blk1, page1); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		readPage := NewPageByBlockSize(testBlockSize)
		if err := fm.Read(blk1, readPage); err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		if got := readPage.GetString(0); got != "block one" {
			t.Errorf("Read() = %q, want %q", got, "block one")
		}
	})
}

func TestAppend(t *testing.T) {
	t.Run("returns BlockId with blkNum 0 when file is empty", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		blk, err := fm.Append(testFileName)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if blk.Number() != 0 {
			t.Errorf("Append() blkNum = %d, want 0", blk.Number())
		}
	})

	t.Run("increments blkNum on each append", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		for i := 0; i < 3; i++ {
			blk, err := fm.Append(testFileName)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			if blk.Number() != i {
				t.Errorf("Append() blkNum = %d, want %d", blk.Number(), i)
			}
		}
	})

	t.Run("returns BlockId with the given filename", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		blk, err := fm.Append(testFileName)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		if blk.FileName() != testFileName {
			t.Errorf("Append() filename = %q, want %q", blk.FileName(), testFileName)
		}
	})
}

func TestLength(t *testing.T) {
	t.Run("returns 0 for empty file", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		// getFile でファイルを作成してから Length を確認
		if _, err := fm.getFile(testFileName); err != nil {
			t.Fatalf("getFile() error = %v", err)
		}
		length, err := fm.Length(testFileName)
		if err != nil {
			t.Fatalf("Length() error = %v", err)
		}
		if length != 0 {
			t.Errorf("Length() = %d, want 0", length)
		}
	})

	t.Run("returns block count equal to number of appended blocks", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		for i := 0; i < 3; i++ {
			if _, err := fm.Append(testFileName); err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}
		length, err := fm.Length(testFileName)
		if err != nil {
			t.Fatalf("Length() error = %v", err)
		}
		if length != 3 {
			t.Errorf("Length() = %d, want 3", length)
		}
	})
}
