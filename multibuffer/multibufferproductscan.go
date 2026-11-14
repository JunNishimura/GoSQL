package multibuffer

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ query.Scan = (*MultibufferProductScan)(nil)

// MultibufferProductScan pairs every record of a scan with every record of a
// table. It reads the table in chunks of several blocks.
//
// For each chunk, the left side is scanned once, and each left record is
// paired with every record of the chunk. The chunk stays in buffers for that
// whole scan. The left side is therefore scanned once per chunk. A plain
// product scans the table once per left record.
//
// The pairs are returned chunk by chunk: first the pairs of the first chunk
// with the whole left side, then those of the next chunk. This differs from
// the order of a plain product. A product does not define its order, and no
// caller should depend on it.
type MultibufferProductScan struct {
	tx       *transaction.Transaction
	lhs      query.Scan
	layout   *recordmanager.Layout
	fileName string
	// fileSize is the number of blocks of the table when the scan was created.
	fileSize int
	// chunkSize is the number of blocks in a chunk. The last chunk can have
	// fewer.
	chunkSize int
	// nextBlock is the first block of the chunk after the current one.
	nextBlock int
	// chunk is the current chunk, and prod is the product of the left side
	// and that chunk. Both are nil only if the table has no blocks.
	chunk *ChunkScan
	prod  *query.ProductScan
}

// NewMultibufferProductScan returns the product of lhs and the table
// tableName, whose records use layout. The scan is positioned before its
// first pair.
//
// The chunk size is computed here from the number of free buffers, and stays
// fixed for the lifetime of the scan. The scan takes ownership of lhs and
// closes it in Close. If this function fails, the caller still owns lhs and
// must close it.
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

// MoveBeforeFirstRecord positions the scan before its first pair, on the
// first chunk of the table.
func (mps *MultibufferProductScan) MoveBeforeFirstRecord() error {
	mps.nextBlock = 0
	_, err := mps.useNextChunk()

	return err
}

// MoveToNextRecord moves to the next pair and reports whether one exists.
// When the pairs of a chunk are exhausted, the next chunk replaces it and the
// left side restarts.
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

// useNextChunk replaces the current chunk with the chunk that starts at
// nextBlock, and restarts the left side for it. It reports whether such a
// chunk exists.
//
// The old chunk is closed before the new one is pinned, so that its buffers
// are free for the new chunk. The chunk size was chosen to fit the buffers
// that were free while no chunk was pinned.
//
// The product scan is replaced, not closed. Closing it would also close the
// left side, which is scanned again for every chunk.
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

// closeChunk closes the current chunk, if there is one, and leaves the left
// side open.
func (mps *MultibufferProductScan) closeChunk() {
	if mps.chunk == nil {
		return
	}

	mps.chunk.Close()
	mps.chunk = nil
	mps.prod = nil
}

// GetInt returns the int field fieldName of the current pair, from the side
// that has the field.
func (mps *MultibufferProductScan) GetInt(fieldName string) (int32, error) {
	if mps.prod == nil {
		return 0, query.ErrNoCurrentRecord
	}

	return mps.prod.GetInt(fieldName)
}

// GetString returns the varchar field fieldName of the current pair, from the
// side that has the field.
func (mps *MultibufferProductScan) GetString(fieldName string) (string, error) {
	if mps.prod == nil {
		return "", query.ErrNoCurrentRecord
	}

	return mps.prod.GetString(fieldName)
}

// GetValue returns the field fieldName of the current pair as a constant,
// from the side that has the field.
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

// Close closes the left side and the current chunk.
func (mps *MultibufferProductScan) Close() {
	mps.lhs.Close()
	mps.closeChunk()
}
