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
func (d *btreeDir) search(searchKey query.Constant) (int, error) {
	return d.findChildBlock(searchKey)
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

// close gives back the pin on whichever block the directory is reading.
func (d *btreeDir) close() {
	d.contents.close()
}
