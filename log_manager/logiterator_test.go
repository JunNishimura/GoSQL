package logmanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

func TestNewLogIterator(t *testing.T) {
	tests := []struct {
		name         string
		wantBoundary int32
	}{
		{
			name:         "given a block with nothing written to it, it starts at the end of the page",
			wantBoundary: 0,
		},
		{
			name:         "given a block that already holds records, it starts at the boundary written in it",
			wantBoundary: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			blk, err := fm.Append(testLogFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}

			if tt.wantBoundary != 0 {
				writePage := filemanager.NewPageByBlockSize(testBlockSize)
				if err := writePage.SetInt(0, tt.wantBoundary); err != nil {
					t.Fatalf("SetInt() error = %v", err)
				}
				if err := fm.Write(blk, writePage); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			}

			it, err := NewLogIterator(fm, blk)
			if err != nil {
				t.Fatalf("NewLogIterator() error = %v", err)
			}

			if it.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", it.fileManager, fm)
			}
			if it.blockId != blk {
				t.Errorf("blockId = %v, want %v", it.blockId, blk)
			}
			if it.boundary != int(tt.wantBoundary) {
				t.Errorf("boundary = %d, want %d", it.boundary, tt.wantBoundary)
			}
			if it.currentPos != int(tt.wantBoundary) {
				t.Errorf("currentPos = %d, want %d", it.currentPos, tt.wantBoundary)
			}

			if err := it.page.SetInt(testBlockSize-4, 1); err != nil {
				t.Errorf("SetInt() at last valid offset error = %v, want nil", err)
			}
			if err := it.page.SetInt(testBlockSize-3, 1); err == nil {
				t.Error("SetInt() beyond block size = nil error, want error")
			}
		})
	}
}

func TestMoveToBlock(t *testing.T) {
	t.Run("given a block that holds records, when the iterator is moved onto it, then it reads from the boundary written in it", func(t *testing.T) {
		fm := newTestFileManager(t)
		blk, err := fm.Append(testLogFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		const wantBoundary = 42
		writePage := filemanager.NewPageByBlockSize(testBlockSize)
		if err := writePage.SetInt(0, wantBoundary); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := fm.Write(blk, writePage); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		it, err := NewLogIterator(fm, blk)
		if err != nil {
			t.Fatalf("NewLogIterator() error = %v", err)
		}

		if err := it.moveToBlock(blk); err != nil {
			t.Fatalf("moveToBlock() error = %v", err)
		}

		if it.blockId != blk {
			t.Errorf("blockId = %v, want %v", it.blockId, blk)
		}
		if it.boundary != wantBoundary {
			t.Errorf("boundary = %d, want %d", it.boundary, wantBoundary)
		}
		if it.currentPos != wantBoundary {
			t.Errorf("currentPos = %d, want %d", it.currentPos, wantBoundary)
		}
		if got := it.page.GetInt(0); got != wantBoundary {
			t.Errorf("page.GetInt(0) = %d, want %d", got, wantBoundary)
		}
	})
}

func TestHasNext(t *testing.T) {
	tests := []struct {
		name       string
		currentPos int
		blkNum     int
		want       bool
	}{
		{
			name:       "given records left in the current block, it reports there is more to read",
			currentPos: 10,
			blkNum:     0,
			want:       true,
		},
		{
			name:       "given the first block read to its end, it reports there is nothing more",
			currentPos: testBlockSize,
			blkNum:     0,
			want:       false,
		},
		{
			name:       "given a later block read to its end, it reports there is more, since earlier blocks remain",
			currentPos: testBlockSize,
			blkNum:     2,
			want:       true,
		},
		{
			name:       "given a later block with records still in it, it reports there is more to read",
			currentPos: 10,
			blkNum:     2,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			blk, err := fm.Append(testLogFile)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			it, err := NewLogIterator(fm, blk)
			if err != nil {
				t.Fatalf("NewLogIterator() error = %v", err)
			}

			it.currentPos = tt.currentPos
			it.blockId = filemanager.NewBlockId(testLogFile, tt.blkNum)

			if got := it.HasNext(); got != tt.want {
				t.Errorf("HasNext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNext(t *testing.T) {
	t.Run("given a log of three records, it returns them newest first and then reports there is no more", func(t *testing.T) {
		dir := t.TempDir()
		fm, err := filemanager.NewFileManager(dir, smallTestBlockSize)
		if err != nil {
			t.Fatalf("NewFileManager() error = %v", err)
		}
		lm, err := NewLogManager(fm, testLogFile)
		if err != nil {
			t.Fatalf("NewLogManager() error = %v", err)
		}

		records := [][]byte{[]byte("AB"), []byte("CD"), []byte("EF")}
		var lsn int
		for _, rec := range records {
			lsn, err = lm.Append(rec)
			if err != nil {
				t.Fatalf("Append() error = %v", err)
			}
		}
		if err := lm.Flush(lsn); err != nil {
			t.Fatalf("Flush() error = %v", err)
		}

		it, err := NewLogIterator(fm, lm.currentBlock)
		if err != nil {
			t.Fatalf("NewLogIterator() error = %v", err)
		}

		wantOrder := [][]byte{[]byte("EF"), []byte("CD"), []byte("AB")}
		for i, want := range wantOrder {
			if !it.HasNext() {
				t.Fatalf("HasNext() = false before reading record %d, want true", i)
			}
			got, err := it.Next()
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("Next()[%d] = %q, want %q", i, got, want)
			}
		}

		if it.HasNext() {
			t.Error("HasNext() = true after reading all records, want false")
		}
	})
}
