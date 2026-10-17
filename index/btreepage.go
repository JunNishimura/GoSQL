package index

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Where the two ints of a B-tree page's header sit within its block.
const (
	flagOffset       = 0
	numRecordsOffset = flagOffset + filemanager.IntBytes
)

// btreePage is one block of a B-tree, seen as a header followed by records kept
// in order of the value they are filed under.
//
// The header is two ints. The first is a flag whose meaning is the caller's: a
// directory block keeps its level there, and a leaf the block its entries
// overflow into. The second is how many records the block holds, which is what
// says where the records end, since a record here has no flag of its own saying
// whether it is in use: the first numRecords slots are, and no others.
//
// Leaves and directory blocks are both this. What sets them apart is the layout
// a page is opened with, which says which fields a record has.
type btreePage struct {
	tx     *transaction.Transaction
	blk    *filemanager.BlockId
	layout *recordmanager.Layout
}

// appendBTreeBlock adds a block to the end of fileName and writes the header of
// an empty page there, flagged with flag.
//
// Only the header is written. The slots past it are never read before a record
// is put in them, since numRecords says none of them hold one.
func appendBTreeBlock(
	tx *transaction.Transaction,
	fileName string,
	layout *recordmanager.Layout,
	flag int32,
) (*filemanager.BlockId, error) {
	blk, err := tx.Append(fileName)
	if err != nil {
		return nil, fmt.Errorf("append a block to B-tree file %s: %w", fileName, err)
	}

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		return nil, err
	}
	defer page.close()

	if err := page.setFlag(flag); err != nil {
		return nil, err
	}
	if err := page.setNumRecords(0); err != nil {
		return nil, err
	}

	return blk, nil
}

// newBTreePage takes hold of blk on behalf of tx and reads it as a B-tree page
// of records laid out as layout says. The block stays pinned until close.
func newBTreePage(
	tx *transaction.Transaction,
	blk *filemanager.BlockId,
	layout *recordmanager.Layout,
) (*btreePage, error) {
	if err := tx.Pin(blk); err != nil {
		return nil, fmt.Errorf("pin %s to read it as a B-tree page: %w", blk, err)
	}

	return &btreePage{
		tx:     tx,
		blk:    blk,
		layout: layout,
	}, nil
}

// flag is the int the header keeps for the caller.
func (p *btreePage) flag() (int32, error) {
	return p.tx.GetInt(p.blk, flagOffset)
}

// setFlag writes flag to the header.
func (p *btreePage) setFlag(flag int32) error {
	return p.tx.SetInt(p.blk, flagOffset, flag)
}

// numRecords is how many records the block holds.
func (p *btreePage) numRecords() (int, error) {
	n, err := p.tx.GetInt(p.blk, numRecordsOffset)
	if err != nil {
		return 0, err
	}

	return int(n), nil
}

// setNumRecords writes n to the header as the number of records the block
// holds.
func (p *btreePage) setNumRecords(n int) error {
	return p.tx.SetInt(p.blk, numRecordsOffset, int32(n))
}

// close gives back the pin taken when the page was opened.
func (p *btreePage) close() {
	p.tx.Unpin(p.blk)
}
