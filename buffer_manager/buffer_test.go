package buffermanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const (
	testLogFile   = "test.log"
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
			name:      "allocates a page of 400 bytes when block size is 400",
			blockSize: 400,
		},
		{
			name:      "allocates a page of 20 bytes when block size is 20",
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
