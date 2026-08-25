package buffermanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const testBufferListPoolSize = 3

func newTestBufferManager(t *testing.T) *BufferManager {
	t.Helper()
	fm, lm := newTestManagers(t, testBlockSize)
	bm, err := NewBufferManager(fm, lm, testBufferListPoolSize)
	if err != nil {
		t.Fatalf("NewBufferManager() error = %v", err)
	}
	return bm
}

func TestNewBufferList(t *testing.T) {
	bm := newTestBufferManager(t)

	bl := NewBufferList(bm)

	if bl == nil {
		t.Fatal("NewBufferList() = nil, want non-nil")
	}
	if bl.bufferManager != bm {
		t.Errorf("bufferManager = %v, want %v", bl.bufferManager, bm)
	}
	if bl.pinned == nil {
		t.Fatal("pinned is nil, want an initialized map")
	}
	if got := len(bl.pinned); got != 0 {
		t.Errorf("len(pinned) = %d, want 0", got)
	}
}

// Every transaction gets its own list, and they share one buffer manager. Each
// has to remember only the buffers it pinned: a shared record would let a
// transaction unpin a buffer another one is still using.
func TestNewBufferListKeepsItsOwnPins(t *testing.T) {
	bm := newTestBufferManager(t)

	first := NewBufferList(bm)
	second := NewBufferList(bm)

	if first.bufferManager != second.bufferManager {
		t.Error("the two lists point at different buffer managers, want the same one")
	}

	first.pinned[*filemanager.NewBlockId(testDataFile, 0)] = pinnedBuffer{pins: 1}

	if got := len(second.pinned); got != 0 {
		t.Errorf("len(pinned) of the second list = %d, want 0", got)
	}
}
