package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// IndexSelectPlan is the plan that reads the records of a table that an index
// holds for one value. It reads them through the index and does not scan the
// table.
//
// It replaces a select over the table with a predicate that equates the
// indexed field with that value. It returns the same records and reads only
// those.
type IndexSelectPlan struct {
	tp    *TablePlan
	info  *metadatamanager.IndexInfo
	value query.Constant
}

var _ Plan = (*IndexSelectPlan)(nil)

// NewIndexSelectPlan returns the plan that reads the records of the table of
// tp that the index of info holds for value.
func NewIndexSelectPlan(tp *TablePlan, info *metadatamanager.IndexInfo, value query.Constant) *IndexSelectPlan {
	return &IndexSelectPlan{
		tp:    tp,
		info:  info,
		value: value,
	}
}

// Open opens the table and the index, and returns a scan of the records that
// the index holds for the value.
//
// If a step fails, the scans already opened are closed, because no scan is
// returned that the caller could close.
func (p *IndexSelectPlan) Open() (query.Scan, error) {
	s, err := p.tp.Open()
	if err != nil {
		return nil, err
	}
	ts, ok := s.(query.UpdateScan)
	if !ok {
		s.Close()

		return nil, fmt.Errorf("read table %q through an index: its scan is a %T, which cannot be moved to a record id", p.tp.tableName, s)
	}

	idx, err := p.info.Open()
	if err != nil {
		ts.Close()

		return nil, err
	}

	return index.NewSelectScan(ts, idx, p.value)
}

// BlocksAccessed returns an estimate of the number of blocks that a read of
// the plan accesses: the blocks of one index search, plus one table block for
// each returned record.
//
// Each record is assumed to be in a different block. Records with the same
// value are stored wherever they were written and are not adjacent, so each
// record is likely to need another block.
func (p *IndexSelectPlan) BlocksAccessed() int {
	return p.info.BlocksAccessed() + p.RecordsOutput()
}

// RecordsOutput returns an estimate of the number of records that the plan
// returns, which is the number that one index search returns.
func (p *IndexSelectPlan) RecordsOutput() int {
	return p.info.RecordsOutput()
}

// DistinctValues returns an estimate of the number of distinct values of
// fieldName in the returned records. The index info provides it, as for an
// index search: one for the indexed field, and the table's count for any
// other field.
func (p *IndexSelectPlan) DistinctValues(fieldName string) int {
	return p.info.DistinctValues(fieldName)
}

// Schema returns the schema of the table, because the plan returns records of
// the table.
func (p *IndexSelectPlan) Schema() *recordmanager.Schema {
	return p.tp.Schema()
}
