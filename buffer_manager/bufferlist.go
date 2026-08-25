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

// Buffer returns the buffer this transaction has pinned for blk, or nil if it
// has not pinned it.
//
// Asking for a block the transaction never pinned is a bug in the caller, and
// nil is what makes it show up. Handing back some other buffer would let the
// caller write into a block it does not hold.
func (bl *BufferList) Buffer(blk *filemanager.BlockId) *Buffer {
	return bl.pinned[*blk].buffer
}

// Pin pins blk for this transaction and keeps the buffer, so that Buffer can
// hand it back and the pin can be released at the end.
//
// Pinning a block that is already pinned raises the count rather than replacing
// the record. The pool counts the pins too, so releasing one of them must not
// release the others.
func (bl *BufferList) Pin(blk *filemanager.BlockId) error {
	buf, err := bl.bufferManager.Pin(blk)
	if err != nil {
		return err
	}

	held := bl.pinned[*blk]
	held.buffer = buf
	held.pins++
	bl.pinned[*blk] = held

	return nil
}

// Unpin gives up one pin on blk. The record is dropped once the last one is
// gone rather than left at a count of zero, so that a block with no record is
// the only way to say this transaction does not hold it.
//
// Unpinning a block this transaction never pinned does nothing. The record is
// what names the buffer to release, so without one there is nothing to do.
func (bl *BufferList) Unpin(blk *filemanager.BlockId) {
	held, ok := bl.pinned[*blk]
	if !ok {
		return
	}

	bl.bufferManager.Unpin(held.buffer)

	held.pins--
	if held.pins == 0 {
		delete(bl.pinned, *blk)
		return
	}
	bl.pinned[*blk] = held
}

// UnpinAll releases every pin this transaction holds, which is what it does
// when it ends.
//
// Each block is released as many times as it was pinned. Releasing it once
// would leave a repeatedly pinned buffer stuck in the pool for the rest of the
// run, since the pool only frees a buffer when its last pin is gone.
func (bl *BufferList) UnpinAll() {
	for _, held := range bl.pinned {
		for range held.pins {
			bl.bufferManager.Unpin(held.buffer)
		}
	}

	clear(bl.pinned)
}
