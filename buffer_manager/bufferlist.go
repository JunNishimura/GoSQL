package buffermanager

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// pinnedBuffer is a buffer that one transaction has pinned, with the number of
// pins. The count matters because every pin must be matched by an unpin. A
// block pinned twice and unpinned once would stay in the pool permanently.
type pinnedBuffer struct {
	buffer *Buffer
	pins   int
}

// BufferList serves one transaction. It records the buffers that the
// transaction has pinned, so that it can return them by block and unpin them
// all when the transaction ends.
//
// The buffer manager is shared by all transactions and owns the pool. The
// records here are private to one transaction. If they were shared, a
// transaction could unpin a buffer that another one is still using.
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

// Buffer returns the buffer that this transaction has pinned for blk, or nil
// if it has not pinned blk.
//
// A request for a block that the transaction never pinned is a bug in the
// caller, and the nil result exposes it. Returning another buffer would let
// the caller write to a block that it does not hold.
func (bl *BufferList) Buffer(blk *filemanager.BlockId) *Buffer {
	return bl.pinned[*blk].buffer
}

// Pin pins blk for this transaction and records the buffer, so that Buffer
// can return it and the pin can be released at the end.
//
// Pinning a block that is already pinned increments its count and keeps the
// record. The pool counts the pins too, so releasing one pin must not release
// the others.
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

// Unpin releases one pin on blk. When the last pin is released, the record is
// removed, not kept with a count of zero. A missing record is then the only
// state that means the transaction does not hold the block.
//
// Unpinning a block that this transaction never pinned does nothing. The
// record identifies the buffer to release, so without a record there is
// nothing to release.
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

// UnpinAll releases every pin that this transaction holds. A transaction
// calls it when it ends.
//
// Each block is unpinned as many times as it was pinned. The pool frees a
// buffer only when its last pin is released, so a single unpin would leave a
// buffer that was pinned several times in the pool permanently.
func (bl *BufferList) UnpinAll() {
	for _, held := range bl.pinned {
		for range held.pins {
			bl.bufferManager.Unpin(held.buffer)
		}
	}

	clear(bl.pinned)
}
