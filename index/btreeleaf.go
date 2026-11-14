package index

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// dirEntry is one directory entry: a key, and the child block that covers the
// keys from that key upward.
//
// A block that has just split returns a dirEntry for the new block. The
// directory level above inserts it, keyed by the first key in the new block.
type dirEntry struct {
	value            query.Constant
	childBlockNumber int
}

// btreeLeaf is a leaf block of a B-tree. It is opened to read or change the
// entries for one key.
//
// The leaf is opened at the slot before the first entry for that key, as a
// scan is opened before its first record. It moves forward one entry at a
// time. A page keeps its records sorted, so the entries for one key are
// adjacent. Reading them means moving forward until the key changes.
//
// The flag of a leaf is the number of its overflow block, or -1 if it has
// none. A leaf overflows when the entries for one key do not fit in one
// block. That key, the overflow key, is always the key of the leaf's first
// entry. Every entry in an overflow block has the overflow key. The leaf
// itself can also hold entries for greater keys after them, and the directory
// maps those keys to the leaf as usual.
type btreeLeaf struct {
	tx        *transaction.Transaction
	layout    *recordmanager.Layout
	searchKey query.Constant
	contents  *btreePage
	// currentSlot is the slot of the current entry. Before the first move it
	// is the slot before the first entry for searchKey.
	currentSlot int
	// previousBlock is the number of the block whose overflow block is the
	// current block. It is -1 while the leaf is still on the block it was
	// opened on. It is used to unlink an overflow block that becomes empty.
	previousBlock int
}

// newBTreeLeaf opens the leaf in blk to read or change the entries for
// searchKey. The leaf is positioned before the first of these entries.
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
		tx:            tx,
		layout:        layout,
		searchKey:     searchKey,
		contents:      contents,
		currentSlot:   slot,
		previousBlock: -1,
	}, nil
}

// next moves to the next entry for the search key and reports whether one
// exists.
//
// Within one block, the entries for the search key end at the first entry
// with a greater key, or at the end of the block. In either case more entries
// can follow in the overflow block. If they do, the leaf moves to the
// overflow block and continues from its first entry.
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

// moveToOverflow moves the leaf to the overflow block of the current block
// if the entries for the search key continue there. It positions the leaf
// before the first entry of that block, and reports whether it moved.
//
// The entries continue there only if the current block has an overflow block
// and the search key is the overflow key, which is the key of the block's
// first entry.
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
	l.previousBlock = l.contents.blk.Number()
	l.contents.close()
	l.contents = overflow
	l.currentSlot = -1

	return true, nil
}

// dataRecordID returns the record id of the table record that the current
// entry points to.
func (l *btreeLeaf) dataRecordID() (*recordmanager.RecordID, error) {
	return l.contents.dataRecordID(l.currentSlot)
}

// delete removes the entry that maps the search key to rid.
//
// delete expects a leaf that has just been opened. It moves forward through
// the entries for the search key to find the entry. Both the key and the
// record id must match: an entry with the same record id but another key is a
// different entry, and is not removed. If no entry matches, delete returns
// ErrEntryNotFound, as a hash index does.
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
			if err := l.contents.delete(l.currentSlot); err != nil {
				return err
			}

			if l.previousBlock < 0 {
				return l.refillIfChainLost()
			}

			return l.unlinkIfEmpty()
		}
	}
}

// refillIfChainLost keeps the overflow chain reachable after a delete in the
// leaf. If the delete removed the leaf's last entry for the search key, it
// moves one entry from the overflow block back into the leaf.
//
// An overflow chain is followed only for the key of the leaf's first entry.
// Once the leaf holds no entry for that key, or no entry at all, the entries
// in the chain still exist but no search can reach them. Moving one of them
// to the first slot of the leaf makes the chain reachable again. The entry
// moved is the last one in the overflow block, so no other entry in that
// block has to shift.
//
// The leaf has room for the entry, because the delete has just freed a slot.
// If the move empties the overflow block, the block is unlinked from the
// chain, as unlinkIfEmpty does.
func (l *btreeLeaf) refillIfChainLost() error {
	if l.currentSlot != 0 {
		return nil
	}

	flag, err := l.contents.flag()
	if err != nil || flag < 0 {
		return err
	}

	n, err := l.contents.numRecords()
	if err != nil {
		return err
	}
	if n > 0 {
		firstKey, err := l.contents.dataValue(0)
		if err != nil || firstKey == l.searchKey {
			return err
		}
	}

	overflow, err := newBTreePage(l.tx, filemanager.NewBlockId(l.contents.blk.FileName(), int(flag)), l.layout)
	if err != nil {
		return err
	}
	defer overflow.close()

	m, err := overflow.numRecords()
	if err != nil {
		return err
	}
	rid, err := overflow.dataRecordID(m - 1)
	if err != nil {
		return err
	}

	if err := l.contents.insertLeaf(0, l.searchKey, rid); err != nil {
		return err
	}
	if err := overflow.delete(m - 1); err != nil {
		return err
	}
	if m > 1 {
		return nil
	}

	next, err := overflow.flag()
	if err != nil {
		return err
	}

	return l.contents.setFlag(next)
}

// unlinkIfEmpty removes the current block from its overflow chain if it is an
// overflow block with no entries left. It sets the flag of the previous block
// to the current block's own overflow block.
//
// An empty block cannot stay in a chain. A search reads the first entry of a
// block to decide whether the chain continues past it. An empty block has no
// first entry: slot 0 holds only the stale bytes of a deleted entry.
func (l *btreeLeaf) unlinkIfEmpty() error {
	if l.previousBlock < 0 {
		return nil
	}

	n, err := l.contents.numRecords()
	if err != nil || n > 0 {
		return err
	}

	flag, err := l.contents.flag()
	if err != nil {
		return err
	}

	previous, err := newBTreePage(l.tx, filemanager.NewBlockId(l.contents.blk.FileName(), l.previousBlock), l.layout)
	if err != nil {
		return err
	}
	defer previous.close()

	return previous.setFlag(flag)
}

// insert adds an entry that maps the search key to rid. The new entry goes
// before any existing entries for the same key.
//
// insert expects a leaf that has just been opened. Such a leaf is positioned
// before the entries for the search key, and inserting at the next slot keeps
// the entries sorted.
//
// If the insert fills the block, the block is split. insert then returns the
// directory entry for the new block, and the caller inserts it into the
// directory. Otherwise insert returns nil.
//
// A leaf with an overflow block must start with the entries for its overflow
// key, so an entry with a smaller key cannot be inserted before them. In that
// case the existing entries move to a new block together with the overflow
// chain. insert returns the directory entry for that block, and the leaf
// keeps only the new entry.
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

// moveOverflowingOut handles an insert into a leaf whose overflow key is
// greater than the search key. It moves all entries of the leaf to a new
// block, and inserts the entry that maps the search key to rid as the only
// entry of the leaf. It returns the directory entry for the new block.
//
// It returns nil if the leaf has no overflow block, or if its overflow key is
// not greater than the search key. The caller then does a normal insert.
//
// The new block takes over the leaf's flag. The overflow chain therefore
// continues from the new block, and the leaf no longer has an overflow block.
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

// overflow makes room in a full leaf whose entries all have the same key. It
// moves every entry except the first to a new overflow block.
//
// Such a leaf cannot be split, because any split position would separate two
// entries for that key. The new block becomes the head of the leaf's overflow
// chain, and the leaf's old overflow block becomes the overflow block of the
// new one. The chain therefore stays a single list of blocks for that key.
// The directory does not change: it still maps that key to the leaf.
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

// split moves the second half of a full leaf to a new block and returns the
// directory entry for that block. n is the number of entries in the leaf, and
// firstKey is the key of its first entry.
//
// The split position starts in the middle and is adjusted so that it does not
// separate two entries with the same key. Otherwise a search for that key
// would be sent to one block and miss the entries in the other.
//
// Normally the position moves left, to the first entry for the middle key, so
// that all entries for that key go to the new block. If the middle key is
// also the first key of the block, moving left would leave no entries in the
// old block. The position then moves right, past the last entry for that key,
// so that all of them stay.
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

// close unpins the block that the leaf currently holds.
func (l *btreeLeaf) close() {
	l.contents.close()
}
