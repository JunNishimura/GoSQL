package filemanager

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
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
			name:    "given a database directory that does not exist, it creates it",
			dirFunc: func(t *testing.T) string { return t.TempDir() + "/newdir" },
		},
		{
			name:    "given a database directory that already exists, it uses it rather than failing",
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

	t.Run("given a database directory that holds a temp table's file and another file, it removes the temp table's file and keeps the other", func(t *testing.T) {
		dir := t.TempDir()
		tempFile := filepath.Join(dir, TempFilePrefix+"1.tbl")
		otherFile := filepath.Join(dir, testFileName)
		for _, path := range []string{tempFile, otherFile} {
			if err := os.WriteFile(path, nil, 0666); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
		}

		if _, err := NewFileManager(dir, testBlockSize); err != nil {
			t.Fatalf("NewFileManager() error = %v", err)
		}

		if _, err := os.Stat(tempFile); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Stat(%q) error = %v, want %v", tempFile, err, os.ErrNotExist)
		}
		if _, err := os.Stat(otherFile); err != nil {
			t.Errorf("Stat(%q) error = %v, want the file kept", otherFile, err)
		}
	})
}

func TestIsNew(t *testing.T) {
	tests := []struct {
		name    string
		dirFunc func(t *testing.T) string
		want    bool
	}{
		{
			name:    "given a database directory that does not exist, when a file manager is made on it, then it reports the database as new",
			dirFunc: func(t *testing.T) string { return t.TempDir() + "/newdir" },
			want:    true,
		},
		{
			// A directory made ahead of time, by hand or by a test, holds no
			// database yet, so it has to be set up like one that did not exist.
			name:    "given a database directory that exists but is empty, when a file manager is made on it, then it reports the database as new",
			dirFunc: func(t *testing.T) string { return t.TempDir() },
			want:    true,
		},
		{
			name: "given a database directory that holds a file, when a file manager is made on it, then it reports the database as not new",
			dirFunc: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, testFileName), nil, 0666); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				return dir
			},
			want: false,
		},
		{
			// The temp table's file is removed as the file manager is made, so
			// the directory is left as empty as one that never held anything.
			name: "given a database directory that holds only a temp table's file, when a file manager is made on it, then it reports the database as new",
			dirFunc: func(t *testing.T) string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, TempFilePrefix+"1.tbl"), nil, 0666); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				return dir
			},
			want: true,
		},
		{
			name: "given a database directory an earlier file manager created and wrote nothing to, when a second is made on it, then it reports the database as new",
			dirFunc: func(t *testing.T) string {
				dir := t.TempDir() + "/newdir"
				if _, err := NewFileManager(dir, testBlockSize); err != nil {
					t.Fatalf("NewFileManager() error = %v", err)
				}
				return dir
			},
			want: true,
		},
		{
			name: "given a database directory an earlier file manager wrote a block to, when a second is made on it, then it reports the database as not new",
			dirFunc: func(t *testing.T) string {
				dir := t.TempDir() + "/newdir"
				fm, err := NewFileManager(dir, testBlockSize)
				if err != nil {
					t.Fatalf("NewFileManager() error = %v", err)
				}
				writeFirstBlock(t, fm, testFileName, 100)
				return dir
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := NewFileManager(tt.dirFunc(t), testBlockSize)
			if err != nil {
				t.Fatalf("NewFileManager() error = %v", err)
			}
			if got := fm.IsNew(); got != tt.want {
				t.Errorf("IsNew() = %v, want %v", got, tt.want)
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
			name:              "when block 0 is written and read back, then it holds what was written",
			blk:               NewBlockId(testFileName, 0),
			writeData:         "hello",
			wantBlocksWritten: 1,
			wantBlocksRead:    1,
		},
		{
			name:              "when a block past the first is written and read back, then its number picks the offset",
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

	// The buffer manager and the log manager each guard their own state with
	// their own lock, so nothing outside the file manager keeps two goroutines
	// from reaching it at once.
	t.Run("given goroutines that each write and read back their own block of the same file at the same time, then each reads what it wrote", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		const goroutines = 8
		const rounds = 100

		var wg sync.WaitGroup
		for i := range goroutines {
			wg.Go(func() {
				blk := NewBlockId(testFileName, i)
				writePage := NewPageByBlockSize(testBlockSize)
				readPage := NewPageByBlockSize(testBlockSize)
				for round := range rounds {
					want := int32(i*rounds + round)
					if err := writePage.SetInt(0, want); err != nil {
						t.Errorf("SetInt() error = %v", err)
						return
					}
					if err := fm.Write(blk, writePage); err != nil {
						t.Errorf("Write(%s) error = %v", blk, err)
						return
					}
					if err := fm.Read(blk, readPage); err != nil {
						t.Errorf("Read(%s) error = %v", blk, err)
						return
					}
					if got := readPage.GetInt(0); got != want {
						t.Errorf("%s holds %d, want %d", blk, got, want)
						return
					}
				}
			})
		}
		wg.Wait()
	})
}

func TestAppend(t *testing.T) {
	tests := []struct {
		name        string
		appendCount int
		wantBlkNum  int
	}{
		{
			name:        "given a file with no blocks, the first append returns block 0 of that file",
			appendCount: 1,
			wantBlkNum:  0,
		},
		{
			name:        "given a file of one block, the next append returns block 1",
			appendCount: 2,
			wantBlkNum:  1,
		},
		{
			name:        "given a file of two blocks, the next append returns block 2",
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
			name:        "given a file that has never been appended to, it reports no blocks",
			appendCount: 0,
			want:        0,
		},
		{
			name:        "given a file appended to three times, it reports three blocks",
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
	t.Run("it moves the file to the destination, creating the directories leading up to it", func(t *testing.T) {
		fm, dir := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)
		destPath := filepath.Join(dir, "archive", "old.db")

		if err := fm.Archive(testFileName, destPath); err != nil {
			t.Fatalf("Archive() error = %v", err)
		}

		archived, err := NewFileManager(filepath.Dir(destPath), testBlockSize)
		if err != nil {
			t.Fatalf("NewFileManager() error = %v", err)
		}
		if got := readFirstBlock(t, archived, filepath.Base(destPath)); got != 100 {
			t.Errorf("the archived file holds %d, want 100", got)
		}
	})

	// The manager keeps every file it has opened, so archiving has to close and
	// forget the handle. Otherwise the next read of that name would follow the
	// handle to the file that was moved away, and the archive would keep growing
	// instead of a new file being started.
	t.Run("given a file that has been archived, when its name is used again, then it starts an empty file", func(t *testing.T) {
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
	})

	// Archiving exists to keep the old file, so it must not quietly replace one
	// that is already there.
	t.Run("given an archive already at the destination, it refuses to replace it and leaves both files alone", func(t *testing.T) {
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
	})

	// The archived file is synced before it is moved, so that it is whole on
	// disk. Its name then refers to no file at all, so nothing under that name
	// is left to sync.
	t.Run("given a file that has been written, when it is archived, then its name is reported as synced", func(t *testing.T) {
		fm, dir := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)

		if err := fm.Archive(testFileName, filepath.Join(dir, "archive", "old.db")); err != nil {
			t.Fatalf("Archive() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{testFileName: true})
	})

	// Moving a file changes the database directory, which no longer names it.
	t.Run("given a file created by a write, when it is archived, then the database directory is reported as synced", func(t *testing.T) {
		fm, dir := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)

		if err := fm.Archive(testFileName, filepath.Join(dir, "archive", "old.db")); err != nil {
			t.Fatalf("Archive() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{".": true})
	})
}

// assertSynced checks what IsSynced reports for each file in want.
func assertSynced(t *testing.T, fm *FileManager, want map[string]bool) {
	t.Helper()
	for fileName, wantSynced := range want {
		if got := fm.IsSynced(fileName); got != wantSynced {
			t.Errorf("IsSynced(%q) = %v, want %v", fileName, got, wantSynced)
		}
	}
}

func TestIsSynced(t *testing.T) {
	tests := []struct {
		name string
		act  func(t *testing.T, fm *FileManager)
		want map[string]bool
	}{
		{
			name: "given a file manager that has written nothing, then it reports a file as synced",
			act:  func(t *testing.T, fm *FileManager) {},
			want: map[string]bool{"a.db": true},
		},
		{
			name: "given a file manager, when a block of one file is written, then that file is reported as not synced and another file as synced",
			act: func(t *testing.T, fm *FileManager) {
				if err := fm.Write(NewBlockId("a.db", 0), NewPageByBlockSize(testBlockSize)); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			},
			want: map[string]bool{"a.db": false, "b.db": true},
		},
		{
			name: "given a file manager, when a block is appended to one file, then that file is reported as not synced and another file as synced",
			act: func(t *testing.T, fm *FileManager) {
				if _, err := fm.Append("a.db"); err != nil {
					t.Fatalf("Append() error = %v", err)
				}
			},
			want: map[string]bool{"a.db": false, "b.db": true},
		},
		{
			// A new file is an entry in the directory, and syncing the file
			// does not put that entry on disk.
			name: "given a file manager, when a block is written to a file that did not exist, then the database directory is reported as not synced",
			act: func(t *testing.T, fm *FileManager) {
				if err := fm.Write(NewBlockId("a.db", 0), NewPageByBlockSize(testBlockSize)); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			},
			want: map[string]bool{".": false},
		},
		{
			name: "given a file manager, when the length of a file that did not exist is asked for, then the database directory is reported as not synced",
			act: func(t *testing.T, fm *FileManager) {
				if _, err := fm.Length("a.db"); err != nil {
					t.Fatalf("Length() error = %v", err)
				}
			},
			want: map[string]bool{".": false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _ := newTestFileManager(t)

			tt.act(t, fm)

			assertSynced(t, fm, tt.want)
		})
	}
}

func TestSync(t *testing.T) {
	tests := []struct {
		name     string
		written  []string
		fileName string
		want     map[string]bool
	}{
		{
			name:     "given two files that have been written, when one is synced, then only that one is reported as synced",
			written:  []string{"a.db", "b.db"},
			fileName: "a.db",
			want:     map[string]bool{"a.db": true, "b.db": false},
		},
		{
			// A file synced to make a write durable has to be found after a
			// crash too, which for a new file takes its directory entry.
			name:     "given a file created by a write, when it is synced, then the database directory is reported as synced too",
			written:  []string{"a.db"},
			fileName: "a.db",
			want:     map[string]bool{"a.db": true, ".": true},
		},
		{
			name:     "given a file that has never been written, when it is synced, then it is reported as synced",
			written:  nil,
			fileName: "a.db",
			want:     map[string]bool{"a.db": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _ := newTestFileManager(t)
			for _, fileName := range tt.written {
				writeFirstBlock(t, fm, fileName, 100)
			}

			if err := fm.Sync(tt.fileName); err != nil {
				t.Fatalf("Sync(%q) error = %v", tt.fileName, err)
			}

			assertSynced(t, fm, tt.want)
		})
	}
}

func TestSyncAll(t *testing.T) {
	t.Run("given two files that have been written, when every file is synced, then both are reported as synced", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, "a.db", 100)
		writeFirstBlock(t, fm, "b.db", 200)

		if err := fm.SyncAll(); err != nil {
			t.Fatalf("SyncAll() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{"a.db": true, "b.db": true})
	})

	t.Run("given two files that have been written and synced, when one is written again, then only that one is reported as not synced", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, "a.db", 100)
		writeFirstBlock(t, fm, "b.db", 200)
		if err := fm.SyncAll(); err != nil {
			t.Fatalf("SyncAll() error = %v", err)
		}

		if err := fm.Write(NewBlockId("a.db", 0), NewPageByBlockSize(testBlockSize)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{"a.db": false, "b.db": true})
	})

	t.Run("given a file created by a write, when every file is synced, then the database directory is reported as synced", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, "a.db", 100)

		if err := fm.SyncAll(); err != nil {
			t.Fatalf("SyncAll() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{".": true})
	})
}

func TestClose(t *testing.T) {
	// Close forgets the handles, so a file it left unsynced could not be synced
	// through the manager afterwards.
	t.Run("given a file manager that has written to a file, when it is closed, then the file is reported as synced", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)

		if err := fm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{testFileName: true})
	})

	t.Run("given a file manager that has created a file, when it is closed, then the database directory is reported as synced", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)

		if err := fm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		assertSynced(t, fm, map[string]bool{".": true})
	})

	t.Run("given a file manager that has written to two files, when it is closed, then every handle it held is closed", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, "a.db", 100)
		writeFirstBlock(t, fm, "b.db", 200)
		var handles []*os.File
		for _, file := range fm.openFiles {
			handles = append(handles, file)
		}

		if err := fm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		for _, file := range handles {
			if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Errorf("Stat() on %s error = %v, want %v", file.Name(), err, os.ErrClosed)
			}
		}
	})

	// Close forgets the handles as well as closing them, so a closed handle is
	// never used again: a later read opens the file afresh.
	t.Run("given a file manager that has been closed, when a file it wrote is read, then it reads what was written", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)
		if err := fm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if got := readFirstBlock(t, fm, testFileName); got != 100 {
			t.Errorf("the file holds %d, want 100", got)
		}
	})

	t.Run("given a file manager that has been closed, when it is closed again, then it reports no error", func(t *testing.T) {
		fm, _ := newTestFileManager(t)
		writeFirstBlock(t, fm, testFileName, 100)
		if err := fm.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		if err := fm.Close(); err != nil {
			t.Errorf("Close() error = %v, want nil", err)
		}
	})
}
