package index

import (
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

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
// not fit in a block, and every entry of an overflow block is filed under that
// value.
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
// The first entry past the ones for the search key is filed under a greater
// value, or there is none, so either is where they end.
func (l *btreeLeaf) next() (bool, error) {
	l.currentSlot++

	n, err := l.contents.numRecords()
	if err != nil {
		return false, err
	}
	if l.currentSlot >= n {
		return false, nil
	}

	v, err := l.contents.dataValue(l.currentSlot)
	if err != nil {
		return false, err
	}

	return v == l.searchKey, nil
}

// dataRecordID is the record id of the indexed table the current entry points
// to.
func (l *btreeLeaf) dataRecordID() (*recordmanager.RecordID, error) {
	return l.contents.dataRecordID(l.currentSlot)
}

// close gives back the pin on whichever block the leaf is reading.
func (l *btreeLeaf) close() {
	l.contents.close()
}
