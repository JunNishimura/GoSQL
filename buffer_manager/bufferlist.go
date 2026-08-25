package buffermanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// pinnedBuffer is a buffer one transaction has pinned, and how many times it did
// so. The count matters because every pin has to be matched by an unpin: a
// block pinned twice and released once would stay in the pool for good.
type pinnedBuffer struct {
	buffer *Buffer
	pins   int
}

// BufferList serves one transaction. It remembers the buffers that transaction
// pinned, so that it can hand them back by block and release them all when the
// transaction ends.
//
// The buffer manager is shared by every transaction and is what actually owns
// the pool; the records here are private to this transaction. Sharing them
// would let a transaction unpin a buffer another one is still using.
type BufferList struct {
	bufferManager *BufferManager
	pinned        map[filemanager.BlockId]pinnedBuffer
}

func NewBufferList(bm *BufferManager) *BufferList {
	return &BufferList{
		bufferManager: bm,
		pinned:        make(map[filemanager.BlockId]pinnedBuffer),
	}
}
