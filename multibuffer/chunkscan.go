package multibuffer

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// beforeFirstSlot is where the scan sits in a block it has moved to but not
// yet looked at, one before slot 0, since a record page searches from past the
// slot it is given.
const beforeFirstSlot = -1

var _ query.Scan = (*ChunkScan)(nil)

// ChunkScan reads the records of a run of blocks of one file, from startBlock
// to endBlock, holding every one of them pinned while it is open.
//
// That is what lets a multibuffer product read its right side a chunk at a
// time: the chunk is read through once for every record of the left, and with
// all of it in buffers none of those reads goes to the disk. A table scan would
// hold only the block it is on and read each block again every time round.
//
// It only reads. The chunks it is used for are copies of a plan's records in a
// temp table, which nothing writes to once they are made.
type ChunkScan struct {
	tx     *transaction.Transaction
	layout *recordmanager.Layout
	// pages are the blocks of the chunk in order, or nil once it is closed.
	pages []*recordmanager.RecordPage
	// current is the index in pages of the block the scan is in.
	current int
	// currentSlot is the slot of that block the scan is on, or
	// beforeFirstSlot.
	currentSlot int
	// onRecord is whether the scan is on a record: false before the first
	// move and after the last record has been passed.
	onRecord bool
}

// NewChunkScan pins blocks startBlock to endBlock of fileName, both included,
// and returns a scan of their records as layout lays them out, before the
// first.
//
// An endBlock before startBlock is refused, since the chunk would have no
// blocks for the scan to stand on. If a block cannot be pinned, the ones
// already pinned are given back, since no scan comes back to close.
func NewChunkScan(tx *transaction.Transaction, fileName string, layout *recordmanager.Layout, startBlock, endBlock int) (*ChunkScan, error) {
	if endBlock < startBlock {
		return nil, fmt.Errorf("open a chunk of blocks %d to %d of %s: the last block comes before the first", startBlock, endBlock, fileName)
	}

	cs := &ChunkScan{
		tx:     tx,
		layout: layout,
		pages:  make([]*recordmanager.RecordPage, 0, endBlock-startBlock+1),
	}
	for blkNum := startBlock; blkNum <= endBlock; blkNum++ {
		rp, err := recordmanager.NewRecordPage(tx, filemanager.NewBlockId(fileName, blkNum), layout)
		if err != nil {
			cs.Close()
			return nil, err
		}
		cs.pages = append(cs.pages, rp)
	}

	if err := cs.MoveBeforeFirstRecord(); err != nil {
		cs.Close()
		return nil, err
	}

	return cs, nil
}

// MoveBeforeFirstRecord puts the scan back before the first record of the
// first block. Nothing is read from the disk, since every block is still
// pinned.
func (cs *ChunkScan) MoveBeforeFirstRecord() error {
	if err := cs.requireOpen(); err != nil {
		return err
	}

	cs.current = 0
	cs.currentSlot = beforeFirstSlot
	cs.onRecord = false

	return nil
}

// MoveToNextRecord moves on to the next record of the chunk and reports
// whether there was one. Blocks with no records left are passed over, and the
// end of the last block is the end of the chunk, whatever blocks the file has
// after it.
func (cs *ChunkScan) MoveToNextRecord() (bool, error) {
	if err := cs.requireOpen(); err != nil {
		return false, err
	}

	for {
		slot, err := cs.pages[cs.current].NextUsedSlotAfter(cs.currentSlot)
		if err == nil {
			cs.currentSlot = slot
			cs.onRecord = true
			return true, nil
		}
		if !errors.Is(err, recordmanager.ErrNoSuchSlot) {
			return false, err
		}

		// The slot is kept where it is on the last block, so that moving on
		// again finds no more records there rather than starting it over.
		if cs.current == len(cs.pages)-1 {
			cs.onRecord = false
			return false, nil
		}
		cs.current++
		cs.currentSlot = beforeFirstSlot
	}
}

// GetInt returns the int field fieldName of the record the scan is on.
func (cs *ChunkScan) GetInt(fieldName string) (int32, error) {
	if err := cs.requireRecord(); err != nil {
		return 0, err
	}

	return cs.pages[cs.current].GetInt(cs.currentSlot, fieldName)
}

// GetString returns the varchar field fieldName of the record the scan is on.
func (cs *ChunkScan) GetString(fieldName string) (string, error) {
	if err := cs.requireRecord(); err != nil {
		return "", err
	}

	return cs.pages[cs.current].GetString(cs.currentSlot, fieldName)
}

// GetValue returns the field fieldName of the record the scan is on, of
// whichever kind the layout says it is.
func (cs *ChunkScan) GetValue(fieldName string) (query.Constant, error) {
	fieldType, err := cs.layout.Schema().Type(fieldName)
	if err != nil {
		return query.Constant{}, err
	}

	switch fieldType {
	case recordmanager.FieldTypeInt:
		val, err := cs.GetInt(fieldName)
		if err != nil {
			return query.Constant{}, err
		}

		return query.NewIntConstant(val), nil
	case recordmanager.FieldTypeVarchar:
		val, err := cs.GetString(fieldName)
		if err != nil {
			return query.Constant{}, err
		}

		return query.NewStringConstant(val), nil
	default:
		return query.Constant{}, fmt.Errorf("read field %q of a chunk: a %s has no value to read", fieldName, fieldType)
	}
}

// HasField reports whether the records of the chunk have fieldName.
func (cs *ChunkScan) HasField(fieldName string) bool {
	return cs.layout.Schema().HasField(fieldName)
}

// Close gives back the pin on every block of the chunk. Closing it again does
// nothing, and reading it afterwards is ErrNoCurrentRecord.
func (cs *ChunkScan) Close() {
	for _, rp := range cs.pages {
		cs.tx.Unpin(rp.BlockID())
	}
	cs.pages = nil
	cs.onRecord = false
}

// requireOpen is the error for moving a scan that has been closed, which is
// on no block to move through.
func (cs *ChunkScan) requireOpen() error {
	if cs.pages == nil {
		return fmt.Errorf("move a closed chunk scan: %w", query.ErrNoCurrentRecord)
	}

	return nil
}

// requireRecord is the error for reading a field when the scan is on no
// record.
func (cs *ChunkScan) requireRecord() error {
	if !cs.onRecord {
		return query.ErrNoCurrentRecord
	}

	return nil
}
