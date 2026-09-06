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
	t.Run("it holds the buffer manager it was given and starts with no pins recorded", func(t *testing.T) {
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
	})

	// Every transaction gets its own list, and they share one buffer manager.
	// Each has to remember only the buffers it pinned: a shared record would let
	// a transaction unpin a buffer another one is still using.
	t.Run("given two lists over the same buffer manager, when one records a pin, then the other does not see it", func(t *testing.T) {
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
	})
}

func TestBufferListPin(t *testing.T) {
	tests := []struct {
		name     string
		pinCalls int
		wantPins int
	}{
		{
			name:     "given a block this list has not pinned, it records the buffer holding it",
			pinCalls: 1,
			wantPins: 1,
		},
		{
			// Each pin has to be matched by an unpin, so a repeat is counted
			// rather than ignored.
			name:     "given a block this list already pinned, a second pin is counted rather than ignored",
			pinCalls: 2,
			wantPins: 2,
		},
		{
			name:     "when a block is pinned three times, then three holds are counted",
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

	t.Run("when two blocks are pinned, then each is recorded on its own buffer", func(t *testing.T) {
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
	})

	// A pin that never happened must not be recorded, or the transaction would
	// later unpin a buffer it does not hold.
	t.Run("given a pool with no buffer left, it reports ErrBufferAbort and records nothing", func(t *testing.T) {
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
	})
}

func TestBufferListBuffer(t *testing.T) {
	t.Run("given a block this list has pinned, it returns the buffer holding it", func(t *testing.T) {
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
	})

	// Asking for a block this transaction never pinned is a bug in the caller,
	// and nil is what makes it show up. Handing back some other buffer would let
	// the caller write into a block it does not hold.
	t.Run("given a block this list has not pinned, it returns none rather than some other buffer", func(t *testing.T) {
		bl := NewBufferList(newTestBufferManager(t))
		blk := filemanager.NewBlockId(testDataFile, 0)

		if got := bl.Buffer(blk); got != nil {
			t.Errorf("Buffer(%v) = %v, want nil", blk, got)
		}
	})
}

func TestBufferListUnpin(t *testing.T) {
	tests := []struct {
		name     string
		pinCalls int
		wantPins int
		// wantAvailable is the pool's free count afterwards. The pool only
		// counts a buffer as free once its last pin is gone.
		wantAvailable int
	}{
		{
			name:          "given a block pinned once, releasing it drops the record and frees the buffer",
			pinCalls:      1,
			wantPins:      0,
			wantAvailable: testBufferListPoolSize,
		},
		{
			name:          "given a block pinned twice, releasing one leaves the block held and the buffer taken",
			pinCalls:      2,
			wantPins:      1,
			wantAvailable: testBufferListPoolSize - 1,
		},
		{
			name:          "given a block pinned three times, releasing one leaves two holds on it",
			pinCalls:      3,
			wantPins:      2,
			wantAvailable: testBufferListPoolSize - 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bm := newTestBufferManager(t)
			bl := NewBufferList(bm)
			blk := filemanager.NewBlockId(testDataFile, 0)
			for i := 0; i < tt.pinCalls; i++ {
				if err := bl.Pin(blk); err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}

			bl.Unpin(blk)

			// A block with no pins left must have no record at all, so that a
			// count of zero never reads as a buffer this transaction holds.
			held, present := bl.pinned[*blk]
			if tt.wantPins == 0 {
				if present {
					t.Errorf("pinned[%v] = %+v, want no entry", blk, held)
				}
			} else if held.pins != tt.wantPins {
				t.Errorf("pinned[%v].pins = %d, want %d", blk, held.pins, tt.wantPins)
			}

			if bm.numAvailable != tt.wantAvailable {
				t.Errorf("numAvailable = %d, want %d", bm.numAvailable, tt.wantAvailable)
			}
		})
	}

	t.Run("given a block this list never pinned, nothing happens rather than a buffer being freed", func(t *testing.T) {
		bm := newTestBufferManager(t)
		bl := NewBufferList(bm)

		bl.Unpin(filemanager.NewBlockId(testDataFile, 0))

		if got := len(bl.pinned); got != 0 {
			t.Errorf("len(pinned) = %d, want 0", got)
		}
		if bm.numAvailable != testBufferListPoolSize {
			t.Errorf("numAvailable = %d, want %d", bm.numAvailable, testBufferListPoolSize)
		}
	})
}

func TestBufferListUnpinAll(t *testing.T) {
	// UnpinAll has to release a block as many times as it was pinned. Releasing
	// each block once would leave a repeatedly pinned buffer stuck in the pool
	// for the rest of the run.
	t.Run("given a block pinned once and another pinned twice, every hold is given up and the pool is free again", func(t *testing.T) {
		bm := newTestBufferManager(t)
		bl := NewBufferList(bm)
		once := filemanager.NewBlockId(testDataFile, 0)
		twice := filemanager.NewBlockId(testDataFile, 1)

		if err := bl.Pin(once); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		for i := 0; i < 2; i++ {
			if err := bl.Pin(twice); err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
		}

		bl.UnpinAll()

		if got := len(bl.pinned); got != 0 {
			t.Errorf("len(pinned) = %d, want 0", got)
		}
		if bm.numAvailable != testBufferListPoolSize {
			t.Errorf("numAvailable = %d, want %d", bm.numAvailable, testBufferListPoolSize)
		}
	})

	t.Run("given a list that pinned nothing, it does nothing rather than failing", func(t *testing.T) {
		bm := newTestBufferManager(t)
		bl := NewBufferList(bm)

		bl.UnpinAll()

		if got := len(bl.pinned); got != 0 {
			t.Errorf("len(pinned) = %d, want 0", got)
		}
		if bm.numAvailable != testBufferListPoolSize {
			t.Errorf("numAvailable = %d, want %d", bm.numAvailable, testBufferListPoolSize)
		}
	})
}
