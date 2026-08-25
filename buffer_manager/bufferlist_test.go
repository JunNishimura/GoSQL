package buffermanager

import (
	"errors"
	"testing"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

const (
	testBufferListPoolSize   = 3
	testBufferListDataBlocks = testBufferListPoolSize + 1
)

// newTestBufferManager builds a manager whose pool is one buffer smaller than
// the data file, so that a test can both pin blocks and run the pool out.
func newTestBufferManager(t *testing.T) *BufferManager {
	t.Helper()
	fm, lm := newTestManagers(t, testBlockSize)
	values := make([]int32, testBufferListDataBlocks)
	for i := range values {
		values[i] = int32(100 + i)
	}
	prepareDataFile(t, fm, values)

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

func TestBufferListPin(t *testing.T) {
	tests := []struct {
		name     string
		pinCalls int
		wantPins int
	}{
		{
			name:     "records the buffer the first time a block is pinned",
			pinCalls: 1,
			wantPins: 1,
		},
		{
			// Each pin has to be matched by an unpin, so a repeat is counted
			// rather than ignored.
			name:     "counts a second pin of the same block",
			pinCalls: 2,
			wantPins: 2,
		},
		{
			name:     "counts every pin of the same block",
			pinCalls: 3,
			wantPins: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bl := NewBufferList(newTestBufferManager(t))
			blk := filemanager.NewBlockId(testDataFile, 0)

			for i := 0; i < tt.pinCalls; i++ {
				if err := bl.Pin(blk); err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}

			held := bl.pinned[*blk]
			if held.pins != tt.wantPins {
				t.Errorf("pinned[%v].pins = %d, want %d", blk, held.pins, tt.wantPins)
			}
			if held.buffer == nil {
				t.Fatal("pinned buffer is nil, want the buffer holding the block")
			}
			if got := held.buffer.Block(); !got.Equals(blk) {
				t.Errorf("pinned buffer holds %v, want %v", got, blk)
			}
		})
	}
}

func TestBufferListBuffer(t *testing.T) {
	bl := NewBufferList(newTestBufferManager(t))
	blk := filemanager.NewBlockId(testDataFile, 0)
	if err := bl.Pin(blk); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}

	got := bl.Buffer(blk)

	if got == nil {
		t.Fatalf("Buffer(%v) = nil, want the buffer holding the block", blk)
	}
	if held := got.Block(); !held.Equals(blk) {
		t.Errorf("Buffer(%v) holds %v", blk, held)
	}
}

// Asking for a block this transaction never pinned is a bug in the caller, and
// nil is what makes it show up. Handing back some other buffer would let the
// caller write into a block it does not hold.
func TestBufferListBufferForABlockItHasNotPinned(t *testing.T) {
	bl := NewBufferList(newTestBufferManager(t))
	blk := filemanager.NewBlockId(testDataFile, 0)

	if got := bl.Buffer(blk); got != nil {
		t.Errorf("Buffer(%v) = %v, want nil", blk, got)
	}
}

func TestBufferListPinIsPerBlock(t *testing.T) {
	bl := NewBufferList(newTestBufferManager(t))
	first := filemanager.NewBlockId(testDataFile, 0)
	second := filemanager.NewBlockId(testDataFile, 1)

	if err := bl.Pin(first); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}
	if err := bl.Pin(second); err != nil {
		t.Fatalf("Pin() error = %v", err)
	}

	if got := bl.pinned[*first].pins; got != 1 {
		t.Errorf("pinned[%v].pins = %d, want 1", first, got)
	}
	if got := bl.pinned[*second].pins; got != 1 {
		t.Errorf("pinned[%v].pins = %d, want 1", second, got)
	}
	if bl.Buffer(first) == bl.Buffer(second) {
		t.Error("both blocks map to the same buffer, want one each")
	}
}

// A pin that never happened must not be recorded, or the transaction would
// later unpin a buffer it does not hold.
func TestBufferListPinRecordsNothingWhenNoBufferIsAvailable(t *testing.T) {
	bm := newTestBufferManager(t)
	bm.maxWaitTime = 50 * time.Millisecond
	bl := NewBufferList(bm)
	for i := 0; i < testBufferListPoolSize; i++ {
		if err := bl.Pin(filemanager.NewBlockId(testDataFile, i)); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
	}

	blk := filemanager.NewBlockId(testDataFile, testBufferListPoolSize)
	err := bl.Pin(blk)

	if !errors.Is(err, ErrBufferAbort) {
		t.Errorf("Pin() error = %v, want %v", err, ErrBufferAbort)
	}
	if _, present := bl.pinned[*blk]; present {
		t.Errorf("pinned[%v] has an entry, want none", blk)
	}
}
