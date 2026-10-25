package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// IndexSelectPlan is the plan of reading the records of a table that an index
// files under one value, through the index rather than by reading the table.
//
// It stands in for a select over the table on a predicate that equates the
// indexed field with that value: it gives back the same records, having read
// only those.
type IndexSelectPlan struct {
	tp    *TablePlan
	info  *metadatamanager.IndexInfo
	value query.Constant
}

var _ Plan = (*IndexSelectPlan)(nil)

// NewIndexSelectPlan returns the plan of reading the records of the table tp
// reads that the index info describes files under value.
func NewIndexSelectPlan(tp *TablePlan, info *metadatamanager.IndexInfo, value query.Constant) *IndexSelectPlan {
	return &IndexSelectPlan{
		tp:    tp,
		info:  info,
		value: value,
	}
}

// Open opens the table and the index, and returns a scan of the records the
// index files under the value.
//
// Whatever was opened before a failure is closed again, since no scan comes
// back for the caller to close.
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

// BlocksAccessed is a guess at how many blocks reading the plan touches: those
// a search through the index reads, and then one of the table for each record
// it gives back.
//
// Each record is taken to be in a block of its own. Records filed under one
// value are wherever they were written, not next to each other, so going from
// one to the next is as good as going to another block.
func (p *IndexSelectPlan) BlocksAccessed() int {
	return p.info.BlocksAccessed() + p.RecordsOutput()
}

// RecordsOutput is a guess at how many records the plan gives back, which is
// how many a search through the index does.
func (p *IndexSelectPlan) RecordsOutput() int {
	return p.info.RecordsOutput()
}

// DistinctValues is a guess at how many different values fieldName holds in
// the records the plan gives back, which the index answers as it would for a
// search through it: one for the indexed field, and the table's for any other.
func (p *IndexSelectPlan) DistinctValues(fieldName string) int {
	return p.info.DistinctValues(fieldName)
}

// Schema is the table's, since the plan gives back the table's own records.
func (p *IndexSelectPlan) Schema() *recordmanager.Schema {
	return p.tp.Schema()
}
