package multibuffer

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ query.Scan = (*MultibufferProductScan)(nil)

// MultibufferProductScan is every record of a scan put beside every record of
// a table, reading the table a chunk of blocks at a time.
//
// For each chunk the left side is read through once, and every record of it
// is put beside every record of the chunk, which is held in buffers the whole
// time. So the left is read once a chunk rather than the table once a record
// of the left, as a plain product would.
//
// The pairs come out chunk by chunk: those of the first chunk against the
// whole left, then those of the next. A product says nothing about its order,
// so that is not the order of a plain product, and nothing should count on it.
type MultibufferProductScan struct {
	tx       *transaction.Transaction
	lhs      query.Scan
	layout   *recordmanager.Layout
	fileName string
	// fileSize is how many blocks the table had when the scan was made.
	fileSize int
	// chunkSize is how many blocks a chunk holds. The last one may hold fewer.
	chunkSize int
	// nextBlock is the first block of the chunk after the one the scan is on.
	nextBlock int
	// chunk is the chunk the scan is on, and prod the product of the left
	// side and it. Both are nil only when the table has no blocks.
	chunk *ChunkScan
	prod  *query.ProductScan
}

// NewMultibufferProductScan returns the product of lhs and the table
// tableName, whose records layout lays out, placed before its first pair.
//
// How many blocks a chunk holds is decided here, from how many buffers are
// free, and kept for the life of the scan. lhs is the scan's own from here on
// and is closed with it; if this fails, closing it is left to the caller, who
// still holds it.
func NewMultibufferProductScan(tx *transaction.Transaction, lhs query.Scan, tableName string, layout *recordmanager.Layout) (*MultibufferProductScan, error) {
	fileName := query.TableFileName(tableName)
	fileSize, err := tx.Size(fileName)
	if err != nil {
		return nil, fmt.Errorf("count the blocks of %s to read it in chunks: %w", fileName, err)
	}

	mps := &MultibufferProductScan{
		tx:        tx,
		lhs:       lhs,
		layout:    layout,
		fileName:  fileName,
		fileSize:  fileSize,
		chunkSize: BestFactor(tx.AvailableBuffers(), fileSize),
	}
	if err := mps.MoveBeforeFirstRecord(); err != nil {
		mps.closeChunk()
		return nil, err
	}

	return mps, nil
}

// MoveBeforeFirstRecord puts the scan back before its first pair, on the
// first chunk of the table.
func (mps *MultibufferProductScan) MoveBeforeFirstRecord() error {
	mps.nextBlock = 0
	_, err := mps.useNextChunk()

	return err
}

// MoveToNextRecord moves on to the next pair and reports whether there was
// one. When the pairs of a chunk run out, the next chunk is pinned in place of
// it and the left side is read again from the start.
func (mps *MultibufferProductScan) MoveToNextRecord() (bool, error) {
	if mps.prod == nil {
		return false, nil
	}

	for {
		ok, err := mps.prod.MoveToNextRecord()
		if err != nil || ok {
			return ok, err
		}

		more, err := mps.useNextChunk()
		if err != nil || !more {
			return false, err
		}
	}
}

// useNextChunk pins the chunk starting at nextBlock in place of the one the
// scan is on, puts the left side beside it from the start, and reports
// whether there was such a chunk.
//
// The old chunk is closed before the new one is pinned, so that the buffers it
// held are free for the new one: the chunk size was chosen to fit what was
// free with no chunk pinned.
//
// The product scan is made again rather than closed, since closing it would
// close the left side as well, which is read again for every chunk.
func (mps *MultibufferProductScan) useNextChunk() (bool, error) {
	if mps.nextBlock >= mps.fileSize {
		return false, nil
	}
	mps.closeChunk()

	endBlock := min(mps.nextBlock+mps.chunkSize-1, mps.fileSize-1)
	chunk, err := NewChunkScan(mps.tx, mps.fileName, mps.layout, mps.nextBlock, endBlock)
	if err != nil {
		return false, err
	}
	prod, err := query.NewProductScan(mps.lhs, chunk)
	if err != nil {
		chunk.Close()
		return false, err
	}

	mps.chunk = chunk
	mps.prod = prod
	mps.nextBlock = endBlock + 1

	return true, nil
}

// closeChunk gives back the chunk the scan is on, if it is on one, leaving the
// left side open.
func (mps *MultibufferProductScan) closeChunk() {
	if mps.chunk == nil {
		return
	}

	mps.chunk.Close()
	mps.chunk = nil
	mps.prod = nil
}

// GetInt returns the int field fieldName of the pair the scan is on, from
// whichever side has it.
func (mps *MultibufferProductScan) GetInt(fieldName string) (int32, error) {
	if mps.prod == nil {
		return 0, query.ErrNoCurrentRecord
	}

	return mps.prod.GetInt(fieldName)
}

// GetString returns the varchar field fieldName of the pair the scan is on,
// from whichever side has it.
func (mps *MultibufferProductScan) GetString(fieldName string) (string, error) {
	if mps.prod == nil {
		return "", query.ErrNoCurrentRecord
	}

	return mps.prod.GetString(fieldName)
}

// GetValue returns the field fieldName of the pair the scan is on, from
// whichever side has it.
func (mps *MultibufferProductScan) GetValue(fieldName string) (query.Constant, error) {
	if mps.prod == nil {
		return query.Constant{}, query.ErrNoCurrentRecord
	}

	return mps.prod.GetValue(fieldName)
}

// HasField reports whether either side has fieldName.
func (mps *MultibufferProductScan) HasField(fieldName string) bool {
	return mps.lhs.HasField(fieldName) || mps.layout.Schema().HasField(fieldName)
}

// Close closes the left side and the chunk the scan is on.
func (mps *MultibufferProductScan) Close() {
	mps.lhs.Close()
	mps.closeChunk()
}
