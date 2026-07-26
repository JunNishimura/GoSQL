package logmanager

import (
	"testing"

	"github.com/JunNishimura/GoSQL/file_manager"
)

const testBlockSize = 400

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
		name    string
		logFile string
	}{
		{
			name:    "creates log manager with given log file name",
			logFile: "test.log",
		},
		{
			name:    "creates log manager with a different log file name",
			logFile: "other.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm := newTestFileManager(t)

			lm := NewLogManager(fm, tt.logFile)

			if lm.fileManager != fm {
				t.Errorf("fileManager = %v, want %v", lm.fileManager, fm)
			}
			if lm.logFile != tt.logFile {
				t.Errorf("logFile = %q, want %q", lm.logFile, tt.logFile)
			}
			if lm.logPage == nil {
				t.Error("logPage is nil, want non-nil")
			}
		})
	}
}
