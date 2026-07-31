package buffermanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// unassigned marks a pool slot that holds no block in the findExistingBuffer tests.
const unassigned = -1

func TestNewBufferManager(t *testing.T) {
	tests := []struct {
		name       string
		numBuffers int
		wantErr    bool
	}{
		{
			name:       "creates a pool of 1 buffer when numBuffers is 1",
			numBuffers: 1,
			wantErr:    false,
		},
		{
			name:       "creates a pool of 3 buffers when numBuffers is 3",
			numBuffers: 3,
			wantErr:    false,
		},
		{
			name:       "returns an error when numBuffers is 0",
			numBuffers: 0,
			wantErr:    true,
		},
		{
			name:       "returns an error when numBuffers is negative",
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
			name:         "returns nil when no buffer has been assigned a block yet",
			assigned:     []int{unassigned, unassigned, unassigned},
			targetFile:   testDataFile,
			targetBlkNum: 0,
			wantIndex:    -1,
		},
		{
			name:         "returns nil when no buffer holds the requested block",
			assigned:     []int{0, 1, 2},
			targetFile:   testDataFile,
			targetBlkNum: 3,
			wantIndex:    -1,
		},
		{
			name:         "returns the buffer holding the requested block",
			assigned:     []int{0, 1, 2},
			targetFile:   testDataFile,
			targetBlkNum: 1,
			wantIndex:    1,
		},
		{
			name:         "skips unassigned buffers and returns the one holding the requested block",
			assigned:     []int{unassigned, 1, unassigned},
			targetFile:   testDataFile,
			targetBlkNum: 1,
			wantIndex:    1,
		},
		{
			name:         "returns nil when the block number matches but the file name differs",
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
				bm.bufferPool[i].blk = filemanager.NewBlockId(testDataFile, blkNum)
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
}

func TestChooseUnpinnedBuffer(t *testing.T) {
	tests := []struct {
		name      string
		pins      []int
		wantIndex int
	}{
		{
			name:      "returns the first buffer when none of them is pinned",
			pins:      []int{0, 0, 0},
			wantIndex: 0,
		},
		{
			name:      "skips the pinned head and returns the first unpinned buffer",
			pins:      []int{2, 0, 0},
			wantIndex: 1,
		},
		{
			name:      "returns the last buffer when it is the only unpinned one",
			pins:      []int{1, 1, 0},
			wantIndex: 2,
		},
		{
			name:      "returns nil when every buffer is pinned",
			pins:      []int{1, 1, 1},
			wantIndex: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, lm := newTestManagers(t, testBlockSize)
			bm, err := NewBufferManager(fm, lm, len(tt.pins))
			if err != nil {
				t.Fatalf("NewBufferManager() error = %v", err)
			}
			for i, pins := range tt.pins {
				for j := 0; j < pins; j++ {
					bm.bufferPool[i].pin()
				}
			}

			got := bm.chooseUnpinnedBuffer()

			if tt.wantIndex == -1 {
				if got != nil {
					t.Errorf("chooseUnpinnedBuffer() = %v, want nil", got)
				}
				return
			}
			if got != bm.bufferPool[tt.wantIndex] {
				t.Errorf("chooseUnpinnedBuffer() = %v, want bufferPool[%d]", got, tt.wantIndex)
			}
		})
	}
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
			name:             "assigns the block to an unpinned buffer when no buffer holds it",
			pins:             []int{0, 0, 0},
			assigned:         []int{unassigned, unassigned, unassigned},
			targetBlkNum:     1,
			wantIndex:        0,
			wantPins:         1,
			wantContents:     101,
			wantNumAvailable: 2,
		},
		{
			name:             "reuses the buffer already holding the block without reloading it from disk",
			pins:             []int{0, 0, 0},
			assigned:         []int{unassigned, 1, unassigned},
			targetBlkNum:     1,
			wantIndex:        1,
			wantPins:         1,
			wantContents:     0,
			wantNumAvailable: 2,
		},
		{
			name:             "keeps numAvailable unchanged when the buffer holding the block is already pinned",
			pins:             []int{0, 1, 0},
			assigned:         []int{unassigned, 1, unassigned},
			targetBlkNum:     1,
			wantIndex:        1,
			wantPins:         2,
			wantContents:     0,
			wantNumAvailable: 2,
		},
		{
			name:             "returns nil when every buffer is pinned and none holds the block",
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
					bm.bufferPool[i].blk = filemanager.NewBlockId(testDataFile, tt.assigned[i])
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
			name:        "flushes only the buffers modified by the given transaction",
			txNums:      []int{1, 2, 1},
			flushTxNum:  1,
			wantFlushed: []bool{true, false, true},
		},
		{
			name:        "flushes nothing when no buffer belongs to the given transaction",
			txNums:      []int{1, 2, 3},
			flushTxNum:  4,
			wantFlushed: []bool{false, false, false},
		},
		{
			name:        "flushes nothing when every buffer is unmodified",
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
				buf := bm.bufferPool[i]
				buf.blk = filemanager.NewBlockId(testDataFile, i)
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
