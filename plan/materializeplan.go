package plan

import (
	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*MaterializePlan)(nil)

// MaterializePlan is the plan that writes the records of another plan to a
// temp table and reads them from that table.
//
// It returns the same records as the underlying plan, so every estimate
// except the blocks is the estimate of that plan. It changes the cost of
// repeated reads. The records are computed once, when the plan is opened, and
// a scan that restarts reads the temp table and not the underlying plan.
type MaterializePlan struct {
	tx  *transaction.Transaction
	src Plan
}

// NewMaterializePlan returns the plan that materializes src.
func NewMaterializePlan(tx *transaction.Transaction, src Plan) *MaterializePlan {
	return &MaterializePlan{
		tx:  tx,
		src: src,
	}
}

// Open writes every record of src to a new temp table and returns a scan of
// the table, positioned before its first record.
//
// src is read to its end and closed here, so the returned scan holds only the
// temp table. If a step fails, the scans already opened are closed, because
// no scan is returned that the caller could close.
func (mp *MaterializePlan) Open() (query.Scan, error) {
	tt := materialize.NewTempTable(mp.tx, mp.src.Schema())

	src, err := mp.src.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dest, err := tt.Open()
	if err != nil {
		return nil, err
	}

	if err := copyRecords(src, dest, mp.src.Schema().Fields()); err != nil {
		dest.Close()

		return nil, err
	}

	if err := dest.MoveBeforeFirstRecord(); err != nil {
		dest.Close()

		return nil, err
	}

	return dest, nil
}

// copyRecords inserts a record into dest for every remaining record of src,
// with the values of fieldNames from src.
func copyRecords(src query.Scan, dest query.UpdateScan, fieldNames []string) error {
	for {
		ok, err := src.MoveToNextRecord()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}

		if err := copyRecord(src, dest, fieldNames); err != nil {
			return err
		}
	}
}

// copyRecord inserts a new record into dest with the values of fieldNames
// from the current record of src.
func copyRecord(src query.Scan, dest query.UpdateScan, fieldNames []string) error {
	if err := dest.MoveToNewRecord(); err != nil {
		return err
	}
	for _, fieldName := range fieldNames {
		val, err := src.GetValue(fieldName)
		if err != nil {
			return err
		}
		if err := dest.SetValue(fieldName, val); err != nil {
			return err
		}
	}

	return nil
}

// BlocksAccessed returns the number of blocks of the temp table: the records
// of src divided by the number of records per block.
//
// The cost of writing the table is not included. The table is written once,
// when the plan is opened. This estimate is the cost of one read of the plan,
// which an operator above can repeat many times.
func (mp *MaterializePlan) BlocksAccessed() int {
	recordsPerBlock := mp.tx.BlockSize() / recordmanager.NewLayout(mp.src.Schema()).SlotSize()

	return (mp.src.RecordsOutput() + recordsPerBlock - 1) / recordsPerBlock
}

// RecordsOutput returns the records of src.
func (mp *MaterializePlan) RecordsOutput() int {
	return mp.src.RecordsOutput()
}

// DistinctValues returns the distinct values of fieldName in src.
func (mp *MaterializePlan) DistinctValues(fieldName string) int {
	return mp.src.DistinctValues(fieldName)
}

// Schema returns the schema of src.
func (mp *MaterializePlan) Schema() *recordmanager.Schema {
	return mp.src.Schema()
}
