package index

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrNoChildBlock reports searching a B-tree directory for a value less than
// every value it files a child under, so that no child is the one to go down
// to.
//
// A directory built as a B-tree index builds one never has such a value: its
// root starts out with an entry for the least value there is, and every entry
// added later is for a greater one. So this means the directory is not what
// the index made it, and is reported rather than read past.
var ErrNoChildBlock = errors.New("no child block for the search key")

// btreeDir is a directory block of a B-tree, opened to find which leaf a value
// is filed in, or to file a block a split has just made.
//
// Each of its records files a child block under a value, saying that the
// entries from that value on, up to the value of the next record, are reached
// through that child. The flag is the directory's level: at 0 its children are
// leaves, and above that they are directory blocks one level down.
type btreeDir struct {
	tx       *transaction.Transaction
	layout   *recordmanager.Layout
	contents *btreePage
}

// newBTreeDir opens the directory block blk, whose records are laid out as
// layout says. The block stays pinned until close.
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

// search goes down from this directory block to the leaf searchKey is filed
// in, and returns the number of that leaf's block.
//
// Each block down is opened in place of the one above it, so that the
// directory reads one block at a time however deep it goes. It ends up
// holding the block at level 0, and close gives that one back.
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

// moveTo opens the directory block numbered blkNum in place of the one being
// read.
//
// The new block is opened before the old one is given back, so that a failure
// to open it leaves the directory holding the block it had, for close to give
// back.
func (d *btreeDir) moveTo(blkNum int) error {
	next, err := newBTreePage(d.tx, filemanager.NewBlockId(d.contents.blk.FileName(), blkNum), d.layout)
	if err != nil {
		return err
	}

	d.contents.close()
	d.contents = next

	return nil
}

// findChildBlock is the number of the child block searchKey is reached
// through: the one filed under the greatest value no greater than searchKey.
//
// findSlotBefore stops short of a record filed under searchKey itself, since
// it is looking for where such records start. Here such a record is the one
// wanted, since its child holds the entries from searchKey on, so the slot
// after is taken if it is one.
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

// insert files e, the record for a block a split below has just made, in the
// directory from this block down.
//
// If that fills a directory block, it is split in turn, and the record for the
// new directory block comes back for the level above to file. Otherwise
// nothing does, and nil comes back.
//
// At level 0, e is filed in this block, since its children are the leaves e
// is about. Above that, e goes down to the child whose entries it is among,
// and this block only files what comes back from the child splitting.
//
// This block stays pinned while the child is, so going down holds one block
// per level at once.
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

// insertEntry files e in this block, at the place that keeps its records in
// order of value.
//
// If that fills the block, the second half of its records moves into a new
// block at the same level, and the record for that block comes back: it is
// filed under the first value it holds, since that is where the entries
// reached through it start. The split is made in the middle as it is, without
// the moving a leaf's split does, since no two records of a directory are
// filed under one value: a leaf never parts the entries for one value, so no
// two blocks a leaf splits into start with the same one.
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

// close gives back the pin on whichever block the directory is reading.
func (d *btreeDir) close() {
	d.contents.close()
}
