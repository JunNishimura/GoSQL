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
			if buf.pins != 0 {
				t.Errorf("pins = %d, want 0", buf.pins)
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

func TestPin(t *testing.T) {
	tests := []struct {
		name     string
		pinCalls int
		wantPins int
	}{
		{
			name:     "increments pins from 0 to 1 on the first call",
			pinCalls: 1,
			wantPins: 1,
		},
		{
			name:     "increments pins to 3 after three calls",
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
