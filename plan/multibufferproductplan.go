package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/multibuffer"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*MultibufferProductPlan)(nil)

// MultibufferProductPlan is the plan of putting every record of one plan
// beside every record of another, reading the right side a chunk of blocks at
// a time.
//
// The right side is copied into a temp table, so that it can be read by block
// number a chunk at a time, and the left side is materialized, since it is
// read through again for every chunk and reading a temp table again is cheaper
// than working the plan out again.
type MultibufferProductPlan struct {
	tx *transaction.Transaction
	// lhs is the left side, materialized.
	lhs    *MaterializePlan
	rhs    Plan
	schema *recordmanager.Schema
}

// NewMultibufferProductPlan returns the plan of the product of lhs and rhs,
// with lhs on the left.
//
// A field both sides have is refused here, as a product plan refuses it.
func NewMultibufferProductPlan(tx *transaction.Transaction, lhs, rhs Plan) (*MultibufferProductPlan, error) {
	schema := recordmanager.NewSchema()
	if err := schema.AddAll(lhs.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the left side of a multibuffer product: %w", err)
	}
	if err := schema.AddAll(rhs.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the right side of a multibuffer product: %w", err)
	}

	return &MultibufferProductPlan{
		tx:     tx,
		lhs:    NewMaterializePlan(tx, lhs),
		rhs:    rhs,
		schema: schema,
	}, nil
}

// Open copies the records of the right side into a temp table, materializes
// the left side, and returns a multibuffer product scan of the two.
//
// The right side is copied after the left is materialized and before the
// product scan is made, so that while the scan chooses its chunk size the
// only buffer held is the one of the left side it reads.
func (mp *MultibufferProductPlan) Open() (query.Scan, error) {
	lhs, err := mp.lhs.Open()
	if err != nil {
		return nil, err
	}

	tt, err := mp.copyRight()
	if err != nil {
		lhs.Close()
		return nil, err
	}

	mps, err := multibuffer.NewMultibufferProductScan(mp.tx, lhs, tt.TableName(), tt.Layout())
	if err != nil {
		lhs.Close()
		return nil, err
	}

	return mps, nil
}

// copyRight writes every record of the right side into a new temp table and
// returns it, with no scan of it or of the right side left open.
func (mp *MultibufferProductPlan) copyRight() (*materialize.TempTable, error) {
	tt := materialize.NewTempTable(mp.tx, mp.rhs.Schema())

	src, err := mp.rhs.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dest, err := tt.Open()
	if err != nil {
		return nil, err
	}
	defer dest.Close()

	if err := copyRecords(src, dest, mp.rhs.Schema().Fields()); err != nil {
		return nil, err
	}

	return tt, nil
}

// BlocksAccessed is one read of the right side, to copy it, plus a read of the
// materialized left side for every chunk the copy is read in.
//
// The chunks are counted with the chunk size the scan would choose if it were
// opened now, from the buffers free at this moment.
func (mp *MultibufferProductPlan) BlocksAccessed() int {
	size := NewMaterializePlan(mp.tx, mp.rhs).BlocksAccessed()
	chunkSize := multibuffer.BestFactor(mp.tx.AvailableBuffers(), size)
	numChunks := (size + chunkSize - 1) / chunkSize

	return mp.rhs.BlocksAccessed() + mp.lhs.BlocksAccessed()*numChunks
}

// RecordsOutput is every record of the left beside every record of the right.
func (mp *MultibufferProductPlan) RecordsOutput() int {
	return mp.lhs.RecordsOutput() * mp.rhs.RecordsOutput()
}

// DistinctValues is the distinct values of fieldName on whichever side has it.
func (mp *MultibufferProductPlan) DistinctValues(fieldName string) int {
	if mp.lhs.Schema().HasField(fieldName) {
		return mp.lhs.DistinctValues(fieldName)
	}

	return mp.rhs.DistinctValues(fieldName)
}

// Schema is the fields of the left followed by those of the right.
func (mp *MultibufferProductPlan) Schema() *recordmanager.Schema {
	return mp.schema
}
