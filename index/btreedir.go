package index

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNoChildBlock is returned when a B-tree directory is searched for a key
// that is less than every key in it, so no child block covers the search key.
//
// A directory built by BTreeIndex never has this gap. Its root starts with an
// entry for the minimum key, and every entry added later has a greater key.
// The error therefore means that the directory is corrupt. The search reports
// it instead of continuing.
var ErrNoChildBlock = errors.New("no child block for the search key")

// btreeDir is a directory block of a B-tree. It is opened to find the leaf
// that a key belongs to, or to insert the entry for a block created by a
// split.
//
// Each directory entry pairs a key with a child block. The child covers the
// keys from the entry's key up to, but not including, the key of the next
// entry. The flag of the block is its level. At level 0 the children are
// leaves. At a higher level they are directory blocks one level below.
type btreeDir struct {
	tx       *transaction.Transaction
	layout   *recordmanager.Layout
	contents *btreePage
}

// newBTreeDir opens the directory block blk, whose entries use layout. The
// block stays pinned until close.
func newBTreeDir(
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
) (*btreeDir, error) {
	contents, err := newBTreePage(tx, blk, layout)
	if err != nil {
		return nil, err
	}

	return &btreeDir{
		tx:       tx,
		layout:   layout,
		contents: contents,
	}, nil
}

// search descends from this directory block to the leaf that searchKey
// belongs to, and returns the block number of that leaf.
//
// At each level the child block replaces the current block, so the directory
// pins one block at a time whatever the depth. After the search the directory
// holds the level-0 block, and close unpins it.
func (d *btreeDir) search(searchKey query.Constant) (int, error) {
	for {
		child, err := d.findChildBlock(searchKey)
		if err != nil {
			return 0, err
		}

		level, err := d.contents.flag()
		if err != nil {
			return 0, err
		}
		if level == 0 {
			return child, nil
		}

		if err := d.moveTo(child); err != nil {
			return 0, err
		}
	}
}

// moveTo replaces the current block with the directory block numbered blkNum.
//
// The new block is pinned before the old one is unpinned. If pinning fails,
// the directory still holds the old block, and close can unpin it.
func (d *btreeDir) moveTo(blkNum int) error {
	next, err := newBTreePage(d.tx, filemanager.NewBlockId(d.contents.blk.FileName(), blkNum), d.layout)
	if err != nil {
		return err
	}

	d.contents.close()
	d.contents = next

	return nil
}

// findChildBlock returns the number of the child block that covers
// searchKey. This is the child of the entry with the greatest key that is
// less than or equal to searchKey.
//
// findSlotBefore returns the slot before the first record whose key equals
// searchKey. In a directory, an entry whose key equals searchKey is the one
// wanted, because its child covers the keys from searchKey upward. So when
// the next slot holds such an entry, findChildBlock uses that slot.
func (d *btreeDir) findChildBlock(searchKey query.Constant) (int, error) {
	slot, err := d.contents.findSlotBefore(searchKey)
	if err != nil {
		return 0, err
	}

	n, err := d.contents.numRecords()
	if err != nil {
		return 0, err
	}
	if slot+1 < n {
		v, err := d.contents.dataValue(slot + 1)
		if err != nil {
			return 0, err
		}
		if v == searchKey {
			slot++
		}
	}

	if slot < 0 {
		return 0, fmt.Errorf("search B-tree directory %s for %s: %w", d.contents.blk, searchKey, ErrNoChildBlock)
	}

	return d.contents.childBlockNumber(slot)
}

// insert adds e to the directory subtree rooted at this block. e is the
// entry for a block that a split below has just created.
//
// If the insert fills a directory block, that block is split too. insert
// then returns the entry for the new directory block, and the caller inserts
// it one level above. Otherwise insert returns nil.
//
// At level 0, e is inserted into this block, because the children of this
// block are leaves and e refers to a leaf. At a higher level, insert recurses
// into the child that covers e's key. This block then receives only the entry
// that the child returns after a split.
//
// This block stays pinned during the recursion, so an insert pins one block
// per level at the same time.
func (d *btreeDir) insert(e dirEntry) (*dirEntry, error) {
	level, err := d.contents.flag()
	if err != nil {
		return nil, err
	}
	if level == 0 {
		return d.insertEntry(e)
	}

	childBlkNum, err := d.findChildBlock(e.value)
	if err != nil {
		return nil, err
	}
	child, err := newBTreeDir(d.tx, filemanager.NewBlockId(d.contents.blk.FileName(), childBlkNum), d.layout)
	if err != nil {
		return nil, err
	}
	fromChild, err := child.insert(e)
	child.close()
	if err != nil || fromChild == nil {
		return nil, err
	}

	return d.insertEntry(*fromChild)
}

// makeNewRoot raises this block, the root, by one level after it has split.
// e is the entry for the block that the split created.
//
// The root block stays in place, so the index always finds the root in the
// same block however deep the tree grows. The records of the root move to a
// new block at the old level. The root then holds two entries: one that maps
// the root's old first key to that new block, and e.
//
// The two entries are inserted in that order without a search. e covers the
// second half of the old root, so its key is the greater one.
func (d *btreeDir) makeNewRoot(e dirEntry) error {
	firstVal, err := d.contents.dataValue(0)
	if err != nil {
		return err
	}
	level, err := d.contents.flag()
	if err != nil {
		return err
	}

	newBlk, err := d.contents.split(0, level)
	if err != nil {
		return err
	}

	if err := d.contents.insertDir(0, firstVal, newBlk.Number()); err != nil {
		return err
	}
	if err := d.contents.insertDir(1, e.value, e.childBlockNumber); err != nil {
		return err
	}

	return d.contents.setFlag(level + 1)
}

// insertEntry inserts e into this block at the slot that keeps the entries
// sorted by key.
//
// If the insert fills the block, the second half of the entries moves to a
// new block at the same level. insertEntry then returns the entry for the new
// block. Its key is the first key in the new block, because the keys that the
// new block covers start there.
//
// The split is made exactly in the middle. A leaf split has to adjust the
// split position to keep equal keys together, but a directory block never
// holds two entries with the same key. A leaf never splits the entries for
// one key, so no two blocks produced by leaf splits start with the same key.
func (d *btreeDir) insertEntry(e dirEntry) (*dirEntry, error) {
	slot, err := d.contents.findSlotBefore(e.value)
	if err != nil {
		return nil, err
	}
	if err := d.contents.insertDir(slot+1, e.value, e.childBlockNumber); err != nil {
		return nil, err
	}

	full, err := d.contents.isFull()
	if err != nil || !full {
		return nil, err
	}

	level, err := d.contents.flag()
	if err != nil {
		return nil, err
	}
	n, err := d.contents.numRecords()
	if err != nil {
		return nil, err
	}

	splitPos := n / 2
	splitVal, err := d.contents.dataValue(splitPos)
	if err != nil {
		return nil, err
	}
	newBlk, err := d.contents.split(splitPos, level)
	if err != nil {
		return nil, err
	}

	return &dirEntry{value: splitVal, childBlockNumber: newBlk.Number()}, nil
}

// close unpins the block that the directory currently holds.
func (d *btreeDir) close() {
	d.contents.close()
}
