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

// MultibufferProductPlan is the plan that pairs every record of one plan with
// every record of another, and reads the right side in chunks of several
// blocks.
//
// The right side is copied to a temp table, so that it can be read by block
// number, one chunk at a time. The left side is materialized, because it is
// scanned again for every chunk. A second read of a temp table is cheaper
// than a second evaluation of the plan.
type MultibufferProductPlan struct {
	tx *transaction.Transaction
	// lhs is the left side, materialized.
	lhs    *MaterializePlan
	rhs    Plan
	schema *recordmanager.Schema
}

// NewMultibufferProductPlan returns the plan for the product of lhs and rhs,
// with lhs on the left.
//
// A field that both sides have is rejected here, as in a product plan.
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

// Open copies the records of the right side to a temp table, materializes the
// left side, and returns a multibuffer product scan of the two.
//
// The right side is copied after the left side is materialized and before the
// product scan is created. While the scan chooses its chunk size, the only
// pinned buffer is then the one of the left side.
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

// copyRight writes every record of the right side to a new temp table and
// returns the table. It leaves no scan of the table or of the right side
// open.
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

// BlocksAccessed returns the blocks of one read of the right side, for the
// copy, plus one read of the materialized left side for every chunk of the
// copy.
//
// The number of chunks uses the chunk size that the scan would choose if it
// were opened now, from the buffers that are currently free.
func (mp *MultibufferProductPlan) BlocksAccessed() int {
	size := NewMaterializePlan(mp.tx, mp.rhs).BlocksAccessed()
	chunkSize := multibuffer.BestFactor(mp.tx.AvailableBuffers(), size)
	numChunks := (size + chunkSize - 1) / chunkSize

	return mp.rhs.BlocksAccessed() + mp.lhs.BlocksAccessed()*numChunks
}

// RecordsOutput returns the number of pairs: the records of the left side
// times the records of the right side.
func (mp *MultibufferProductPlan) RecordsOutput() int {
	return mp.lhs.RecordsOutput() * mp.rhs.RecordsOutput()
}

// DistinctValues returns the distinct values of fieldName on the side that
// has the field.
func (mp *MultibufferProductPlan) DistinctValues(fieldName string) int {
	if mp.lhs.Schema().HasField(fieldName) {
		return mp.lhs.DistinctValues(fieldName)
	}

	return mp.rhs.DistinctValues(fieldName)
}

// Schema returns the fields of the left side followed by those of the right
// side.
func (mp *MultibufferProductPlan) Schema() *recordmanager.Schema {
	return mp.schema
}
