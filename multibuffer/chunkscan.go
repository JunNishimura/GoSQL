package multibuffer

import (
	"errors"
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// beforeFirstSlot is the slot position of a scan that has moved to a block
// but has not read any slot yet. It is one less than slot 0, because a record
// page searches after the slot it is given.
const beforeFirstSlot = -1

var _ query.Scan = (*ChunkScan)(nil)

// ChunkScan reads the records of a range of blocks of one file, from
// startBlock to endBlock. It keeps all these blocks pinned while it is open.
//
// A multibuffer product uses it to read its right side one chunk at a time.
// The chunk is scanned once for every left record. With the whole chunk in
// buffers, none of these scans reads from disk. A table scan would pin only
// its current block, and would read every block again in each scan.
//
// A chunk scan is read-only. Its chunks are copies of the records of a plan
// in a temp table, and nothing writes to that table after it is created.
type ChunkScan struct {
	tx     *transaction.Transaction
	layout *recordmanager.Layout
	// pages are the blocks of the chunk in order, or nil after Close.
	pages []*recordmanager.RecordPage
	// current is the index in pages of the current block.
	current int
	// currentSlot is the current slot in that block, or beforeFirstSlot.
	currentSlot int
	// onRecord reports whether the scan is on a record. It is false before
	// the first move and after the last record.
	onRecord bool
}

// NewChunkScan pins the blocks startBlock to endBlock of fileName, inclusive.
// It returns a scan of their records, which use layout, positioned before the
// first record.
//
// An endBlock less than startBlock is rejected, because the chunk would have
// no blocks. If a block cannot be pinned, the blocks already pinned are
// unpinned, because no scan is returned that the caller could close.
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

// MoveBeforeFirstRecord positions the scan before the first record of the
// first block. It reads nothing from disk, because every block is still
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

// MoveToNextRecord moves to the next record of the chunk and reports whether
// one exists. Blocks with no remaining records are skipped. The end of the
// last block is the end of the chunk, even if the file has more blocks.
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

		// On the last block the slot is not reset. Another move then finds
		// no more records there, and does not restart the block.
		if cs.current == len(cs.pages)-1 {
			cs.onRecord = false
			return false, nil
		}
		cs.current++
		cs.currentSlot = beforeFirstSlot
	}
}

// GetInt returns the int field fieldName of the current record.
func (cs *ChunkScan) GetInt(fieldName string) (int32, error) {
	if err := cs.requireRecord(); err != nil {
		return 0, err
	}

	return cs.pages[cs.current].GetInt(cs.currentSlot, fieldName)
}

// GetString returns the varchar field fieldName of the current record.
func (cs *ChunkScan) GetString(fieldName string) (string, error) {
	if err := cs.requireRecord(); err != nil {
		return "", err
	}

	return cs.pages[cs.current].GetString(cs.currentSlot, fieldName)
}

// GetValue returns the field fieldName of the current record as a constant of
// the type that the layout declares for the field.
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

// Close unpins every block of the chunk. A second Close does nothing, and a
// read after Close returns ErrNoCurrentRecord.
func (cs *ChunkScan) Close() {
	for _, rp := range cs.pages {
		cs.tx.Unpin(rp.BlockID())
	}
	cs.pages = nil
	cs.onRecord = false
}

// requireOpen returns an error if the scan has been closed. A closed scan has
// no blocks to move through.
func (cs *ChunkScan) requireOpen() error {
	if cs.pages == nil {
		return fmt.Errorf("move a closed chunk scan: %w", query.ErrNoCurrentRecord)
	}

	return nil
}

// requireRecord returns an error if the scan is on no record.
func (cs *ChunkScan) requireRecord() error {
	if !cs.onRecord {
		return query.ErrNoCurrentRecord
	}

	return nil
}
