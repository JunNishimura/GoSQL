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
