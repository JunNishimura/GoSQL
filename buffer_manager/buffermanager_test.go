package buffermanager

import (
	"errors"
	"reflect"
	"testing"
	"time"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// unassigned marks a pool slot that holds no block in the findExistingBuffer tests.
const unassigned = -1

// holdBlock puts a buffer in the state it reaches after loading a block, without
// touching the disk, keeping the manager's block index in sync.
func holdBlock(bm *BufferManager, index, blkNum int) {
	blk := filemanager.NewBlockId(testDataFile, blkNum)
	bm.bufferPool[index].blk = blk
	bm.bufferByBlock[*blk] = bm.bufferPool[index]
}

func TestNewBufferManager(t *testing.T) {
	tests := []struct {
		name       string
		numBuffers int
		wantErr    bool
	}{
		{
			name:       "given a pool of one buffer, it builds that buffer and counts it free",
			numBuffers: 1,
			wantErr:    false,
		},
		{
			name:       "given a pool of several buffers, each is a distinct buffer rather than one shared",
			numBuffers: 3,
			wantErr:    false,
		},
		{
			name:       "given a pool of no buffers, it refuses to build a manager that could never pin anything",
			numBuffers: 0,
			wantErr:    true,
		},
		{
			name:       "given a negative pool size, it refuses rather than treating it as empty",
			numBuffers: -1,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)

			bm, err := NewBufferManager(fm, lm, tt.numBuffers)

			if tt.wantErr {
				if err == nil {
					t.Fatal("NewBufferManager() error = nil, want error")
				}
				if bm != nil {
					t.Errorf("NewBufferManager() = %v, want nil on error", bm)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}

			if len(bm.bufferPool) != tt.numBuffers {
				t.Fatalf("len(bufferPool) = %d, want %d", len(bm.bufferPool), tt.numBuffers)
			}
			if bm.numAvailable != tt.numBuffers {
				t.Errorf("numAvailable = %d, want %d", bm.numAvailable, tt.numBuffers)
			}
			if bm.cond == nil {
				t.Fatal("cond is nil, want non-nil")
			}

			seen := make(map[*Buffer]bool, tt.numBuffers)
			for i, buf := range bm.bufferPool {
				if buf == nil {
					t.Fatalf("bufferPool[%d] is nil, want non-nil", i)
				}
				if seen[buf] {
					t.Errorf("bufferPool[%d] reuses a buffer already in the pool, want a distinct instance", i)
				}
				seen[buf] = true

				if buf.fileManager != fm {
					t.Errorf("bufferPool[%d].fileManager = %v, want %v", i, buf.fileManager, fm)
				}
				if buf.logManager != lm {
					t.Errorf("bufferPool[%d].logManager = %v, want %v", i, buf.logManager, lm)
				}
				if buf.contents == nil {
					t.Errorf("bufferPool[%d].contents is nil, want non-nil", i)
				}
			}
		})
	}
}

func TestFindExistingBuffer(t *testing.T) {
	tests := []struct {
		name         string
		assigned     []int
		targetFile   string
		targetBlkNum int
		wantIndex    int
	}{
		{
			name:         "given a pool where no buffer holds a block yet, it finds none",
			assigned:     []int{unassigned, unassigned, unassigned},
			targetFile:   testDataFile,
			targetBlkNum: 0,
			wantIndex:    -1,
		},
		{
			name:         "given a pool holding other blocks, it finds none for the one asked about",
			assigned:     []int{0, 1, 2},
			targetFile:   testDataFile,
			targetBlkNum: 3,
			wantIndex:    -1,
		},
		{
			name:         "given a buffer holding the block asked about, that buffer is found",
			assigned:     []int{0, 1, 2},
			targetFile:   testDataFile,
			targetBlkNum: 1,
			wantIndex:    1,
		},
		{
			name:         "given buffers holding no block either side of it, the one that holds it is still found",
			assigned:     []int{unassigned, 1, unassigned},
			targetFile:   testDataFile,
			targetBlkNum: 1,
			wantIndex:    1,
		},
		{
			name:         "given the same block number in another file, it finds none, so the file name counts too",
			assigned:     []int{0, 1, 2},
			targetFile:   testLogFile,
			targetBlkNum: 1,
			wantIndex:    -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			bm, err := NewBufferManager(fm, lm, len(tt.assigned))
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			for i, blkNum := range tt.assigned {
				if blkNum == unassigned {
					continue
				}
				holdBlock(bm, i, blkNum)
			}

			// A fresh BlockId is passed in so that a match cannot rely on
			// pointer identity with the one stored in the pool.
			got := bm.findExistingBuffer(filemanager.NewBlockId(tt.targetFile, tt.targetBlkNum))

			if tt.wantIndex == -1 {
				if got != nil {
					t.Errorf("findExistingBuffer() = %v, want nil", got)
				}
				return
			}
			if got != bm.bufferPool[tt.wantIndex] {
				t.Errorf("findExistingBuffer() = %v, want bufferPool[%d]", got, tt.wantIndex)
			}
		})
	}

	t.Run("given a buffer that was reassigned to another block, the block it used to hold is no longer found", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)
		prepareDataFile(t, fm, []int32{100, 101})

		bm, err := NewBufferManager(fm, lm, 1)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}

		buf, err := bm.Pin(filemanager.NewBlockId(testDataFile, 0))
		if err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		bm.Unpin(buf)
		if _, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1)); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		if got := bm.findExistingBuffer(filemanager.NewBlockId(testDataFile, 0)); got != nil {
			t.Errorf("findExistingBuffer(block 0) = %v after its buffer was reassigned, want nil", got)
		}
		if got := bm.findExistingBuffer(filemanager.NewBlockId(testDataFile, 1)); got != buf {
			t.Errorf("findExistingBuffer(block 1) = %v, want the reassigned buffer", got)
		}
	})

	// A failed assignment can leave a buffer naming a block it does not hold.
	// Replacing that buffer must not disturb the buffer that really holds it.
	t.Run("given another buffer that names a block it does not hold, replacing it leaves the real holder findable", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)
		prepareDataFile(t, fm, []int32{100, 101})

		bm, err := NewBufferManager(fm, lm, 2)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}

		block0 := filemanager.NewBlockId(testDataFile, 0)
		held, err := bm.Pin(block0)
		if err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		bm.bufferPool[1].blk = block0

		if _, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1)); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		if got := bm.findExistingBuffer(block0); got != held {
			t.Errorf("findExistingBuffer(block 0) = %v, want the buffer that holds it", got)
		}
	})
}

// prepareDataFile appends one block to the data file per given value and writes
// that value at offset 0, so that a page loaded from disk can be told apart from
// one that was never read.
func prepareDataFile(t *testing.T, fm *filemanager.FileManager, values []int32) {
	t.Helper()

	for _, value := range values {
		blk, err := fm.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		page := filemanager.NewPageByBlockSize(testBlockSize)
		if err := page.SetInt(0, value); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := fm.Write(blk, page); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
}

func TestTryToPin(t *testing.T) {
	tests := []struct {
		name             string
		pins             []int
		assigned         []int
		targetBlkNum     int
		wantIndex        int
		wantPins         int
		wantContents     int32
		wantNumAvailable int
	}{
		{
			name:             "given no buffer holds the block, a free one is given it and the block read in",
			pins:             []int{0, 0, 0},
			assigned:         []int{unassigned, unassigned, unassigned},
			targetBlkNum:     1,
			wantIndex:        0,
			wantPins:         1,
			wantContents:     101,
			wantNumAvailable: 2,
		},
		{
			name:             "given a buffer already holding the block, it is reused rather than the block read again",
			pins:             []int{0, 0, 0},
			assigned:         []int{unassigned, 1, unassigned},
			targetBlkNum:     1,
			wantIndex:        1,
			wantPins:         1,
			wantContents:     0,
			wantNumAvailable: 2,
		},
		{
			name:             "given the buffer holding the block is already pinned, the free count does not fall again",
			pins:             []int{0, 1, 0},
			assigned:         []int{unassigned, 1, unassigned},
			targetBlkNum:     1,
			wantIndex:        1,
			wantPins:         2,
			wantContents:     0,
			wantNumAvailable: 2,
		},
		{
			name:             "given every buffer pinned and none holding the block, it takes none",
			pins:             []int{1, 1, 1},
			assigned:         []int{0, 2, 3},
			targetBlkNum:     1,
			wantIndex:        -1,
			wantNumAvailable: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101, 102, 103})

			bm, err := NewBufferManager(fm, lm, len(tt.pins))
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			numAvailable := 0
			for i := range tt.pins {
				if tt.assigned[i] != unassigned {
					holdBlock(bm, i, tt.assigned[i])
				}
				for j := 0; j < tt.pins[i]; j++ {
					bm.bufferPool[i].pin()
				}
				if tt.pins[i] == 0 {
					numAvailable++
				}
			}
			bm.numAvailable = numAvailable

			targetBlk := filemanager.NewBlockId(testDataFile, tt.targetBlkNum)
			got, err := bm.tryToPin(targetBlk)
			if err != nil {
				t.Fatalf("tryToPin() error = %v", err)
			}

			if bm.numAvailable != tt.wantNumAvailable {
				t.Errorf("numAvailable = %d, want %d", bm.numAvailable, tt.wantNumAvailable)
			}

			if tt.wantIndex == -1 {
				if got != nil {
					t.Errorf("tryToPin() = %v, want nil", got)
				}
				return
			}
			if got != bm.bufferPool[tt.wantIndex] {
				t.Fatalf("tryToPin() = %v, want bufferPool[%d]", got, tt.wantIndex)
			}
			if !got.blk.Equals(targetBlk) {
				t.Errorf("blk = %v, want %v", got.blk, targetBlk)
			}
			if got.pins != tt.wantPins {
				t.Errorf("pins = %d, want %d", got.pins, tt.wantPins)
			}
			if contents := got.contents.GetInt(0); contents != tt.wantContents {
				t.Errorf("contents.GetInt(0) = %d, want %d", contents, tt.wantContents)
			}
		})
	}

	readTimeTests := []struct {
		name          string
		assigned      []int
		pinBlkNums    []int
		wantReadTimes []int
	}{
		{
			name:          "when blocks are read in one after another, then each buffer gets a later read time",
			assigned:      []int{unassigned, unassigned, unassigned},
			pinBlkNums:    []int{1, 2, 3},
			wantReadTimes: []int{1, 2, 3},
		},
		{
			name:          "given a buffer already holding the block, its read time is left as it was",
			assigned:      []int{unassigned, 1, unassigned},
			pinBlkNums:    []int{1},
			wantReadTimes: []int{0, 0, 0},
		},
		{
			name:          "when one buffer is given a block, then the read times of the others are left alone",
			assigned:      []int{unassigned, 1, unassigned},
			pinBlkNums:    []int{1, 2},
			wantReadTimes: []int{1, 0, 0},
		},
	}

	for _, tt := range readTimeTests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101, 102, 103})

			bm, err := NewBufferManager(fm, lm, len(tt.assigned))
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			for i, blkNum := range tt.assigned {
				if blkNum != unassigned {
					holdBlock(bm, i, blkNum)
				}
			}

			for _, blkNum := range tt.pinBlkNums {
				if _, err := bm.tryToPin(filemanager.NewBlockId(testDataFile, blkNum)); err != nil {
					t.Fatalf("tryToPin() error = %v", err)
				}
			}

			for i, want := range tt.wantReadTimes {
				if got := bm.bufferPool[i].readTime; got != want {
					t.Errorf("bufferPool[%d].readTime = %d, want %d", i, got, want)
				}
			}
		})
	}
}

func TestBufferManagerPin(t *testing.T) {
	const poolSize = 2

	tests := []struct {
		name             string
		targets          []int
		wantIndex        int
		wantPins         int
		wantContents     int32
		wantNumAvailable int
	}{
		{
			name:             "given a free buffer, the block is read into it and the buffer counted as taken",
			targets:          []int{1},
			wantIndex:        0,
			wantPins:         1,
			wantContents:     101,
			wantNumAvailable: poolSize - 1,
		},
		{
			name:             "given the block is already in the pool, that buffer is reused rather than another taken",
			targets:          []int{1, 1},
			wantIndex:        0,
			wantPins:         2,
			wantContents:     101,
			wantNumAvailable: poolSize - 1,
		},
		{
			name:             "given another block is asked for, a second buffer is taken for it",
			targets:          []int{0, 1},
			wantIndex:        1,
			wantPins:         1,
			wantContents:     101,
			wantNumAvailable: poolSize - 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101})

			bm, err := NewBufferManager(fm, lm, poolSize)
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}

			var got *Buffer
			for _, blkNum := range tt.targets {
				got, err = bm.Pin(filemanager.NewBlockId(testDataFile, blkNum))
				if err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}

			if got != bm.bufferPool[tt.wantIndex] {
				t.Fatalf("Pin() = %v, want bufferPool[%d]", got, tt.wantIndex)
			}
			if got.pins != tt.wantPins {
				t.Errorf("pins = %d, want %d", got.pins, tt.wantPins)
			}
			if contents := got.contents.GetInt(0); contents != tt.wantContents {
				t.Errorf("contents.GetInt(0) = %d, want %d", contents, tt.wantContents)
			}
			if bm.numAvailable != tt.wantNumAvailable {
				t.Errorf("numAvailable = %d, want %d", bm.numAvailable, tt.wantNumAvailable)
			}
		})
	}

	t.Run("given a pool with no buffer free, it waits, and takes one once another caller unpins", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)
		prepareDataFile(t, fm, []int32{100, 101})

		bm, err := NewBufferManager(fm, lm, 1)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}

		held, err := bm.Pin(filemanager.NewBlockId(testDataFile, 0))
		if err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		type pinResult struct {
			buf *Buffer
			err error
		}
		done := make(chan pinResult, 1)
		go func() {
			buf, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1))
			done <- pinResult{buf: buf, err: err}
		}()

		select {
		case got := <-done:
			t.Fatalf("Pin() returned (%v, %v) while the pool was full, want it to keep waiting", got.buf, got.err)
		case <-time.After(50 * time.Millisecond):
			// Still waiting, which is the expected behaviour.
		}

		bm.Unpin(held)

		select {
		case got := <-done:
			if got.err != nil {
				t.Fatalf("Pin() error = %v", got.err)
			}
			if got.buf != bm.bufferPool[0] {
				t.Fatalf("Pin() = %v, want bufferPool[0]", got.buf)
			}
			if contents := got.buf.contents.GetInt(0); contents != 101 {
				t.Errorf("contents.GetInt(0) = %d, want 101", contents)
			}
			if bm.numAvailable != 0 {
				t.Errorf("numAvailable = %d, want 0", bm.numAvailable)
			}
		case <-time.After(time.Second):
			t.Fatal("Pin() did not return after the buffer was unpinned")
		}
	})

	t.Run("given a pool whose buffers are never freed, it waits out the limit and reports ErrBufferAbort", func(t *testing.T) {
		const maxWaitTime = 50 * time.Millisecond

		fm, lm := newTestManagers(t, testBlockSize)
		prepareDataFile(t, fm, []int32{100, 101})

		bm, err := NewBufferManager(fm, lm, 1)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}
		bm.maxWaitTime = maxWaitTime

		if _, err := bm.Pin(filemanager.NewBlockId(testDataFile, 0)); err != nil {
			t.Fatalf("Pin() error = %v", err)
		}

		start := time.Now()
		got, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1))
		elapsed := time.Since(start)

		if !errors.Is(err, ErrBufferAbort) {
			t.Errorf("Pin() error = %v, want %v", err, ErrBufferAbort)
		}
		if got != nil {
			t.Errorf("Pin() = %v, want nil", got)
		}
		if elapsed < maxWaitTime {
			t.Errorf("Pin() gave up after %v, want it to wait at least %v", elapsed, maxWaitTime)
		}
		if bm.numAvailable != 0 {
			t.Errorf("numAvailable = %d, want 0", bm.numAvailable)
		}
	})
}

func TestBufferManagerUnpin(t *testing.T) {
	const poolSize = 3

	tests := []struct {
		name             string
		pinCalls         int
		unpinCalls       int
		wantPins         int
		wantNumAvailable int
	}{
		{
			name:             "given a buffer pinned once, releasing it counts the buffer free again",
			pinCalls:         1,
			unpinCalls:       1,
			wantPins:         0,
			wantNumAvailable: poolSize,
		},
		{
			name:             "given a buffer pinned twice, releasing one leaves it taken",
			pinCalls:         2,
			unpinCalls:       1,
			wantPins:         1,
			wantNumAvailable: poolSize - 1,
		},
		{
			name:             "given a buffer pinned twice, releasing both counts it free again",
			pinCalls:         2,
			unpinCalls:       2,
			wantPins:         0,
			wantNumAvailable: poolSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			bm, err := NewBufferManager(fm, lm, poolSize)
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}

			buf := bm.bufferPool[0]
			for i := 0; i < tt.pinCalls; i++ {
				buf.pin()
			}
			bm.numAvailable = poolSize - 1

			for i := 0; i < tt.unpinCalls; i++ {
				bm.Unpin(buf)
			}

			if buf.pins != tt.wantPins {
				t.Errorf("pins = %d, want %d", buf.pins, tt.wantPins)
			}
			if bm.numAvailable != tt.wantNumAvailable {
				t.Errorf("numAvailable = %d, want %d", bm.numAvailable, tt.wantNumAvailable)
			}
		})
	}
}

func TestFlushAll(t *testing.T) {
	onDisk := []int32{100, 101, 102}
	inMemory := []int32{900, 901, 902}

	tests := []struct {
		name        string
		txNums      []int
		flushTxNum  int
		wantFlushed []bool
	}{
		{
			name:        "given buffers of two transactions, only the one named has its buffers written out",
			txNums:      []int{1, 2, 1},
			flushTxNum:  1,
			wantFlushed: []bool{true, false, true},
		},
		{
			name:        "given no buffer belongs to the transaction named, nothing is written out",
			txNums:      []int{1, 2, 3},
			flushTxNum:  4,
			wantFlushed: []bool{false, false, false},
		},
		{
			name:        "given every buffer unmodified, nothing is written out",
			txNums:      []int{-1, -1, -1},
			flushTxNum:  -1,
			wantFlushed: []bool{false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, onDisk)

			bm, err := NewBufferManager(fm, lm, len(tt.txNums))
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			for i, txNum := range tt.txNums {
				holdBlock(bm, i, i)

				buf := bm.bufferPool[i]
				buf.lsn = appendLogRecord(t, lm)
				buf.txNum = txNum
				if err := buf.contents.SetInt(0, inMemory[i]); err != nil {
					t.Fatalf("SetInt() error = %v", err)
				}
			}

			if err := bm.FlushAll(tt.flushTxNum); err != nil {
				t.Fatalf("FlushAll() error = %v", err)
			}

			for i, wantFlushed := range tt.wantFlushed {
				page := filemanager.NewPageByBlockSize(testBlockSize)
				if err := fm.Read(filemanager.NewBlockId(testDataFile, i), page); err != nil {
					t.Fatalf("Read() error = %v", err)
				}

				want := onDisk[i]
				if wantFlushed {
					want = inMemory[i]
				}
				if got := page.GetInt(0); got != want {
					t.Errorf("block %d on disk = %d, want %d", i, got, want)
				}

				wantTxNum := tt.txNums[i]
				if wantFlushed {
					wantTxNum = -1
				}
				if got := bm.bufferPool[i].txNum; got != wantTxNum {
					t.Errorf("bufferPool[%d].txNum = %d, want %d", i, got, wantTxNum)
				}
			}
		})
	}
}

func TestNewBufferManagerWithPolicy(t *testing.T) {
	tests := []struct {
		name         string
		policy       ReplacementPolicy
		wantStrategy replacementStrategy
		wantErr      bool
	}{
		{
			name:         "given the naive policy, the manager replaces buffers naively",
			policy:       NaivePolicy,
			wantStrategy: &naiveStrategy{},
		},
		{
			name:         "given the FIFO policy, the manager replaces buffers in the order they were read in",
			policy:       FIFOPolicy,
			wantStrategy: &fifoStrategy{},
		},
		{
			name:         "given the LRU policy, the manager replaces the buffer used longest ago",
			policy:       LRUPolicy,
			wantStrategy: &lruStrategy{},
		},
		{
			name:         "given the clock policy, the manager replaces buffers in clock order",
			policy:       ClockPolicy,
			wantStrategy: &clockStrategy{},
		},
		{
			name:         "given the unmodified-first policy, the manager prefers buffers that need no write",
			policy:       UnmodifiedFirstPolicy,
			wantStrategy: &unmodifiedFirstStrategy{},
		},
		{
			name:         "given the least-recently-modified policy, the manager replaces the buffer logged earliest",
			policy:       LeastRecentlyModifiedPolicy,
			wantStrategy: &leastRecentlyModifiedStrategy{},
		},
		{
			name:    "given a policy it does not know, it refuses rather than falling back to one",
			policy:  ReplacementPolicy(99),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)

			bm, err := NewBufferManagerWithPolicy(fm, lm, 3, tt.policy)

			if tt.wantErr {
				if err == nil {
					t.Fatal("NewBufferManagerWithPolicy() error = nil, want error")
				}
				if bm != nil {
					t.Errorf("NewBufferManagerWithPolicy() = %v, want nil on error", bm)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewBufferManagerWithPolicy() error = %v", err)
			}
			if got, want := reflect.TypeOf(bm.strategy), reflect.TypeOf(tt.wantStrategy); got != want {
				t.Errorf("strategy = %v, want %v", got, want)
			}
		})
	}

	t.Run("given a manager built without a policy, it replaces buffers naively", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)

		bm, err := NewBufferManager(fm, lm, 3)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}

		if _, ok := bm.strategy.(*naiveStrategy); !ok {
			t.Errorf("strategy = %T, want *naiveStrategy", bm.strategy)
		}
	})
}

// TestBufferManagerPinReplacesTheBufferChosenByPolicy drives the pool through a
// sequence whose outcome is the same under every policy, and then pins one more
// block so that each policy is forced to pick a different victim:
//
//	readTime = {6, 2, 8, 4} -> FIFO picks bufferPool[1]
//	unpinned in the order 2, 3, 1, 0 -> LRU picks bufferPool[2]
//	every buffer unpinned -> naive picks bufferPool[0]
//	clock hand left at 3 -> clock picks bufferPool[3]
func TestBufferManagerPinReplacesTheBufferChosenByPolicy(t *testing.T) {
	const poolSize = 4

	tests := []struct {
		name      string
		policy    ReplacementPolicy
		wantIndex int
	}{
		{
			name:      "given the naive policy, the buffer it replaces is the first of the pool",
			policy:    NaivePolicy,
			wantIndex: 0,
		},
		{
			name:      "given the FIFO policy, the buffer it replaces is the one read in first",
			policy:    FIFOPolicy,
			wantIndex: 1,
		},
		{
			name:      "given the LRU policy, the buffer it replaces is the one released first",
			policy:    LRUPolicy,
			wantIndex: 2,
		},
		{
			name:      "given the clock policy, the buffer it replaces is the one after the last replaced",
			policy:    ClockPolicy,
			wantIndex: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101, 102, 103, 104, 105, 106})

			bm, err := NewBufferManagerWithPolicy(fm, lm, poolSize, tt.policy)
			if err != nil {
				t.Fatalf("NewBufferManagerWithPolicy() error = %v", err)
			}

			pin := func(blkNum int) *Buffer {
				t.Helper()
				buf, err := bm.Pin(filemanager.NewBlockId(testDataFile, blkNum))
				if err != nil {
					t.Fatalf("Pin(%d) error = %v", blkNum, err)
				}
				return buf
			}

			// Fill the pool, then reassign bufferPool[0] and bufferPool[2] so
			// that the read-in order no longer matches the pool order.
			block0 := pin(0)
			block1 := pin(1)
			block2 := pin(2)
			block3 := pin(3)
			bm.Unpin(block0)
			block4 := pin(4)
			bm.Unpin(block2)
			block5 := pin(5)

			// Release every buffer in an order that differs from both the pool
			// order and the read-in order.
			bm.Unpin(block5)
			bm.Unpin(block3)
			bm.Unpin(block1)
			bm.Unpin(block4)

			got := pin(6)

			if got != bm.bufferPool[tt.wantIndex] {
				t.Errorf("Pin(6) = %v, want bufferPool[%d]", got, tt.wantIndex)
			}
		})
	}
}

func TestBufferManagerStats(t *testing.T) {
	const poolSize = 3

	tests := []struct {
		name       string
		pinBlkNums []int
		wantPins   int
		wantHits   int
	}{
		{
			name:       "given a block that has to be read from disk, the pin is counted but no hit",
			pinBlkNums: []int{0},
			wantPins:   1,
			wantHits:   0,
		},
		{
			name:       "given a block the pool already holds, a hit is counted",
			pinBlkNums: []int{0, 0},
			wantPins:   2,
			wantHits:   1,
		},
		{
			name:       "given every pin asks for a different block, no hit is counted",
			pinBlkNums: []int{0, 1, 2},
			wantPins:   3,
			wantHits:   0,
		},
		{
			name:       "given a block asked for again and again, a hit is counted each time",
			pinBlkNums: []int{0, 1, 0, 1},
			wantPins:   4,
			wantHits:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101, 102})

			bm, err := NewBufferManager(fm, lm, poolSize)
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			for _, blkNum := range tt.pinBlkNums {
				if _, err := bm.Pin(filemanager.NewBlockId(testDataFile, blkNum)); err != nil {
					t.Fatalf("Pin(%d) error = %v", blkNum, err)
				}
			}

			stats := bm.GetStats()

			if got := stats.Pins(); got != tt.wantPins {
				t.Errorf("Pins() = %d, want %d", got, tt.wantPins)
			}
			if got := stats.Hits(); got != tt.wantHits {
				t.Errorf("Hits() = %d, want %d", got, tt.wantHits)
			}
		})
	}

	const unmodified = -1

	flushTests := []struct {
		name string
		// modifyTxNum marks the pinned buffer as modified by that transaction,
		// where unmodified leaves it alone.
		modifyTxNum int
		// flushAll asks the manager to write out flushAllTxNum's buffers instead
		// of forcing a replacement.
		flushAll      bool
		flushAllTxNum int
		wantFlushes   int
	}{
		{
			name:        "given the replaced buffer was unmodified, no write is counted",
			modifyTxNum: unmodified,
			flushAll:    false,
			wantFlushes: 0,
		},
		{
			name:        "given the replaced buffer was modified, the write it needed is counted",
			modifyTxNum: 1,
			flushAll:    false,
			wantFlushes: 1,
		},
		{
			name:          "given a modified buffer of the transaction flushed, the write is counted",
			modifyTxNum:   1,
			flushAll:      true,
			flushAllTxNum: 1,
			wantFlushes:   1,
		},
		{
			name:          "given the flush was for another transaction, no write is counted",
			modifyTxNum:   1,
			flushAll:      true,
			flushAllTxNum: 2,
			wantFlushes:   0,
		},
	}

	for _, tt := range flushTests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			prepareDataFile(t, fm, []int32{100, 101})

			bm, err := NewBufferManager(fm, lm, 1)
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			buf, err := bm.Pin(filemanager.NewBlockId(testDataFile, 0))
			if err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
			if tt.modifyTxNum != unmodified {
				buf.SetModified(tt.modifyTxNum, -1)
			}

			if tt.flushAll {
				if err := bm.FlushAll(tt.flushAllTxNum); err != nil {
					t.Fatalf("FlushAll() error = %v", err)
				}
			} else {
				bm.Unpin(buf)
				if _, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1)); err != nil {
					t.Fatalf("Pin() error = %v", err)
				}
			}

			if got := bm.GetStats().Flushes(); got != tt.wantFlushes {
				t.Errorf("Flushes() = %d, want %d", got, tt.wantFlushes)
			}
		})
	}

	t.Run("given a pin that had to wait for a buffer, the wait is counted", func(t *testing.T) {
		fm, lm := newTestManagers(t, testBlockSize)
		prepareDataFile(t, fm, []int32{100, 101})

		bm, err := NewBufferManager(fm, lm, 1)
		if err != nil {
			t.Fatalf("NewBufferManager() error = %v", err)
		}

		held, err := bm.Pin(filemanager.NewBlockId(testDataFile, 0))
		if err != nil {
			t.Fatalf("Pin() error = %v", err)
		}
		if got := bm.GetStats().Waits(); got != 0 {
			t.Fatalf("Waits() = %d for a pin served right away, want 0", got)
		}

		done := make(chan error, 1)
		go func() {
			_, err := bm.Pin(filemanager.NewBlockId(testDataFile, 1))
			done <- err
		}()

		// Give the second pin time to find the pool full and start waiting.
		time.Sleep(50 * time.Millisecond)
		bm.Unpin(held)

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Pin() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Pin() did not return after the buffer was unpinned")
		}

		if got := bm.GetStats().Waits(); got != 1 {
			t.Errorf("Waits() = %d, want 1", got)
		}
	})
}
