package plan

import (
	"github.com/JunNishimura/GoSQL/materialize"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

var _ Plan = (*MaterializePlan)(nil)

// MaterializePlan is the plan of writing the records of another plan into a
// temp table and reading them from there.
//
// It gives back the same records as the plan underneath, so every number but
// the blocks is that plan's. What it changes is what reading them again costs:
// the records are worked out once, when the plan is opened, and a scan that
// starts over reads the temp table rather than the plan underneath.
type MaterializePlan struct {
	tx  *transaction.Transaction
	src Plan
}

// NewMaterializePlan returns the plan of materializing src.
func NewMaterializePlan(tx *transaction.Transaction, src Plan) *MaterializePlan {
	return &MaterializePlan{
		tx:  tx,
		src: src,
	}
}

// Open writes every record of src into a new temp table and returns a scan of
// the table, before its first record.
//
// src is read to the end and closed here, so the scan handed back holds only
// the temp table. Whatever was opened before a failure is closed again, since
// no scan comes back for the caller to close.
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

// copyRecords writes a record into dest for every record src has left, with
// the values src holds in fieldNames.
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

// copyRecord writes a new record into dest with the values the record src is
// on holds in fieldNames.
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

// BlocksAccessed is the blocks the temp table takes up: the records of src,
// as many to a block as fit in one.
//
// Writing the table is left out. It is done once, when the plan is opened,
// while this is the cost of reading the plan, which an operator above may do
// many times over.
func (mp *MaterializePlan) BlocksAccessed() int {
	recordsPerBlock := mp.tx.BlockSize() / recordmanager.NewLayout(mp.src.Schema()).SlotSize()

	return (mp.src.RecordsOutput() + recordsPerBlock - 1) / recordsPerBlock
}

// RecordsOutput is the records of src.
func (mp *MaterializePlan) RecordsOutput() int {
	return mp.src.RecordsOutput()
}

// DistinctValues is the distinct values of fieldName in src.
func (mp *MaterializePlan) DistinctValues(fieldName string) int {
	return mp.src.DistinctValues(fieldName)
}

// Schema is the schema of src.
func (mp *MaterializePlan) Schema() *recordmanager.Schema {
	return mp.src.Schema()
}
