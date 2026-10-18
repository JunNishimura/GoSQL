package index

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// dirEntry is one directory record: a value, and the child block the entries
// from that value on are reached through.
//
// It is what a block that has just been split hands up to the directory above
// it, which files the new block under the first value it holds.
type dirEntry struct {
	value            query.Constant
	childBlockNumber int
}

// btreeLeaf is a leaf block of a B-tree, opened to read or change the entries
// filed under one value.
//
// It is opened at the slot before the first entry for that value, as a scan is
// opened before its first record, and moves forward from there one entry at a
// time. The entries for one value are next to each other, since a page keeps
// its records in order, so reading them is moving forward until the value
// changes.
//
// A leaf's flag is the number of the block its entries overflow into, or -1
// if they do not. Entries overflow when so many share one value that they do
// not fit in a block, and that value is the one the leaf's first entry is filed
// under. Every entry of an overflow block is filed under it, but the leaf the
// chain starts from may go on past them to entries for greater values, which
// the directory sends there as it would to any leaf.
type btreeLeaf struct {
	tx        *transaction.Transaction
	layout    *recordmanager.Layout
	searchKey query.Constant
	contents  *btreePage
	// currentSlot is the slot of the entry being read, or the slot before the
	// first entry for searchKey until the first move.
	currentSlot int
}

// newBTreeLeaf opens the leaf in blk to read or change the entries filed under
// searchKey, before the first of them.
//
// The block stays pinned until close.
func newBTreeLeaf(
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
	searchKey query.Constant,
) (*btreeLeaf, error) {
	contents, err := newBTreePage(tx, blk, layout)
	if err != nil {
		return nil, err
	}

	slot, err := contents.findSlotBefore(searchKey)
	if err != nil {
		contents.close()

		return nil, err
	}

	return &btreeLeaf{
		tx:          tx,
		layout:      layout,
		searchKey:   searchKey,
		contents:    contents,
		currentSlot: slot,
	}, nil
}

// next moves on to the next entry filed under the search key and reports
// whether there was one.
//
// The entries for the search key in one block end at the first entry filed
// under a greater value, or at the end of the block. Either may not be the end
// of them, since they may go on in the block this one overflows into, and then
// the leaf moves there and goes on from its first entry.
func (l *btreeLeaf) next() (bool, error) {
	for {
		l.currentSlot++

		n, err := l.contents.numRecords()
		if err != nil {
			return false, err
		}

		if l.currentSlot < n {
			v, err := l.contents.dataValue(l.currentSlot)
			if err != nil {
				return false, err
			}
			if v == l.searchKey {
				return true, nil
			}
		}

		moved, err := l.moveToOverflow()
		if err != nil || !moved {
			return false, err
		}
	}
}

// moveToOverflow moves the leaf to the block the current one overflows into,
// before its first entry, if the entries for the search key go on there, and
// reports whether it did.
//
// They go on there only if the block overflows at all and the search key is
// the value its entries overflow under, which is the value of its first entry.
func (l *btreeLeaf) moveToOverflow() (bool, error) {
	flag, err := l.contents.flag()
	if err != nil {
		return false, err
	}
	if flag < 0 {
		return false, nil
	}

	firstKey, err := l.contents.dataValue(0)
	if err != nil {
		return false, err
	}
	if firstKey != l.searchKey {
		return false, nil
	}

	overflow, err := newBTreePage(l.tx, filemanager.NewBlockId(l.contents.blk.FileName(), int(flag)), l.layout)
	if err != nil {
		return false, err
	}
	l.contents.close()
	l.contents = overflow
	l.currentSlot = -1

	return true, nil
}

// dataRecordID is the record id of the indexed table the current entry points
// to.
func (l *btreeLeaf) dataRecordID() (*recordmanager.RecordID, error) {
	return l.contents.dataRecordID(l.currentSlot)
}

// delete takes out the entry filing rid under the search key.
//
// It is meant for a leaf just opened, and moves forward through the entries
// for the search key to find it. Both halves have to match: the same record id
// filed under another value is a different entry, and is left alone. One that
// is not there at all is ErrEntryNotFound, as it is for a hash index.
func (l *btreeLeaf) delete(rid *recordmanager.RecordID) error {
	for {
		ok, err := l.next()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("delete %s filed under %s from B-tree leaf %s: %w", rid, l.searchKey, l.contents.blk, ErrEntryNotFound)
		}

		current, err := l.dataRecordID()
		if err != nil {
			return err
		}
		if current.Equals(rid) {
			return l.contents.delete(l.currentSlot)
		}
	}
}

// insert files rid under the search key, ahead of any entries already filed
// under it.
//
// It is meant for a leaf just opened, which is before the entries for the
// search key: that is the place that keeps the entries in order, and the
// insert goes there.
//
// If the block fills up, it is split and the directory record for the new
// block comes back, for the directory to file. Otherwise there is nothing for
// the directory to do, and nil comes back.
//
// A leaf that overflows has to start with the entries for the value it
// overflows under, so an entry for a smaller value cannot go ahead of them.
// Instead they move out, chain and all, into a new block for the directory to
// file under their value, and the leaf is left holding the new entry alone.
func (l *btreeLeaf) insert(rid *recordmanager.RecordID) (*dirEntry, error) {
	if e, err := l.moveOverflowingOut(rid); err != nil || e != nil {
		return e, err
	}

	l.currentSlot++
	if err := l.contents.insertLeaf(l.currentSlot, l.searchKey, rid); err != nil {
		return nil, err
	}

	full, err := l.contents.isFull()
	if err != nil || !full {
		return nil, err
	}

	n, err := l.contents.numRecords()
	if err != nil {
		return nil, err
	}
	firstKey, err := l.contents.dataValue(0)
	if err != nil {
		return nil, err
	}
	lastKey, err := l.contents.dataValue(n - 1)
	if err != nil {
		return nil, err
	}
	if firstKey == lastKey {
		return nil, l.overflow()
	}

	return l.split(n, firstKey)
}

// moveOverflowingOut files rid under the search key in a leaf that
// overflows under a greater value, by moving everything the leaf held into a
// new block and putting the entry in the leaf alone. It returns the directory
// record for the new block, or nil if the leaf does not overflow under a
// greater value and the insert has to go ahead as usual.
//
// The new block takes the leaf's flag, so the chain goes on from it as it did
// from the leaf, and the leaf no longer overflows.
func (l *btreeLeaf) moveOverflowingOut(rid *recordmanager.RecordID) (*dirEntry, error) {
	flag, err := l.contents.flag()
	if err != nil || flag < 0 {
		return nil, err
	}

	firstKey, err := l.contents.dataValue(0)
	if err != nil {
		return nil, err
	}
	c, err := firstKey.Compare(l.searchKey)
	if err != nil || c <= 0 {
		return nil, err
	}

	newBlk, err := l.contents.split(0, flag)
	if err != nil {
		return nil, err
	}
	if err := l.contents.setFlag(-1); err != nil {
		return nil, err
	}

	l.currentSlot = 0
	if err := l.contents.insertLeaf(l.currentSlot, l.searchKey, rid); err != nil {
		return nil, err
	}

	return &dirEntry{value: firstKey, childBlockNumber: newBlk.Number()}, nil
}

// overflow makes room in a full leaf whose entries are all filed under one
// value, by moving all but the first into a new block the leaf overflows into.
//
// Such a leaf cannot be split, since every place to split it would part two
// entries for that value. The new block goes at the head of the leaf's chain,
// overflowing into whichever block the leaf did, so the chain stays one line
// of blocks for that value. Nothing changes for the directory, which still
// sends a search for that value to the leaf.
func (l *btreeLeaf) overflow() error {
	flag, err := l.contents.flag()
	if err != nil {
		return err
	}

	newBlk, err := l.contents.split(1, flag)
	if err != nil {
		return err
	}

	return l.contents.setFlag(int32(newBlk.Number()))
}

// split moves the second half of a full leaf, which holds n entries starting
// with one for firstKey, into a new block and returns the directory record for
// it.
//
// It starts from the middle and moves the split so that it does not part two
// entries filed under the same value: a search for that value would otherwise
// be sent to one block and miss the entries in the other. The split moves
// left, to before the first entry for the middle value, so that they all go to
// the new block; but if that value is the first in the block, there is nothing
// to the left of it to keep, and the split moves right past its last entry
// instead, so that they all stay.
func (l *btreeLeaf) split(n int, firstKey query.Constant) (*dirEntry, error) {
	splitPos := n / 2
	splitKey, err := l.contents.dataValue(splitPos)
	if err != nil {
		return nil, err
	}

	if splitKey == firstKey {
		for splitPos < n {
			v, err := l.contents.dataValue(splitPos)
			if err != nil {
				return nil, err
			}
			if v != splitKey {
				splitKey = v

				break
			}
			splitPos++
		}
	} else {
		for {
			v, err := l.contents.dataValue(splitPos - 1)
			if err != nil {
				return nil, err
			}
			if v != splitKey {
				break
			}
			splitPos--
		}
	}

	newBlk, err := l.contents.split(splitPos, -1)
	if err != nil {
		return nil, err
	}

	return &dirEntry{value: splitKey, childBlockNumber: newBlk.Number()}, nil
}

// close gives back the pin on whichever block the leaf is reading.
func (l *btreeLeaf) close() {
	l.contents.close()
}
