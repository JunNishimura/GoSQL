package logmanager

import (
	"testing"

	"github.com/JunNishimura/GoSQL/file_manager"
)

func TestNewLogIterator(t *testing.T) {
	tests := []struct {
		name   string
		blkNum int
	}{
		{
			name:   "creates iterator for block 0",
			blkNum: 0,
		},
		{
			name:   "creates iterator for a later block",
			blkNum: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)
			blk := filemanager.NewBlockId(testLogFile, tt.blkNum)

			it := NewLogIterator(fm, blk)

			if it.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", it.fileManager, fm)
			}
			if it.blockId != blk {
				t.Errorf("blockId = %v, want %v", it.blockId, blk)
			}
			if it.page == nil {
				t.Fatal("page is nil, want non-nil")
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
