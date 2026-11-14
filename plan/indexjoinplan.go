package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// IndexJoinPlan is the plan that joins the records of one plan with the
// records of a table, on a field of the plan and an indexed field of the
// table. For each record of the plan it searches the index, and does not scan
// the whole table.
//
// It replaces a select over the product of the two with a predicate that
// equates the two fields. It returns the same pairs, and reads only the table
// records that are part of a pair.
type IndexJoinPlan struct {
	p1        Plan
	p2        *TablePlan
	info      *metadatamanager.IndexInfo
	joinField string
	schema    *recordmanager.Schema
}

var _ Plan = (*IndexJoinPlan)(nil)

// NewIndexJoinPlan returns the plan that joins the records of p1 with the
// records of the table of p2. A pair matches when the value of joinField in
// the p1 record equals the key of the p2 record in the index of info.
//
// It fails, like a product, if the two sides share a field name, because the
// pairs would then have two fields with the same name.
func NewIndexJoinPlan(p1 Plan, p2 *TablePlan, info *metadatamanager.IndexInfo, joinField string) (*IndexJoinPlan, error) {
	schema := recordmanager.NewSchema()
	if err := schema.AddAll(p1.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the left side of an index join: %w", err)
	}
	if err := schema.AddAll(p2.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the right side of an index join: %w", err)
	}

	return &IndexJoinPlan{
		p1:        p1,
		p2:        p2,
		info:      info,
		joinField: joinField,
		schema:    schema,
	}, nil
}

// Open opens the left plan, the index and the table, and returns a scan of
// the joined pairs.
//
// If a step fails, the scans already opened are closed, because no scan is
// returned that the caller could close.
func (p *IndexJoinPlan) Open() (query.Scan, error) {
	lhs, err := p.p1.Open()
	if err != nil {
		return nil, err
	}

	s, err := p.p2.Open()
	if err != nil {
		lhs.Close()

		return nil, err
	}
	rhs, ok := s.(query.UpdateScan)
	if !ok {
		s.Close()
		lhs.Close()

		return nil, fmt.Errorf("join table %q through an index: its scan is a %T, which cannot be moved to a record id", p.p2.tableName, s)
	}

	idx, err := p.info.Open()
	if err != nil {
		rhs.Close()
		lhs.Close()

		return nil, err
	}

	return index.NewJoinScan(lhs, idx, p.joinField, rhs)
}

// BlocksAccessed returns an estimate of the number of blocks that a read of
// the plan accesses: one read of the left side, one index search for each
// left record, and one table block for each pair, as in an index select.
func (p *IndexJoinPlan) BlocksAccessed() int {
	return p.p1.BlocksAccessed() + p.p1.RecordsOutput()*p.info.BlocksAccessed() + p.RecordsOutput()
}

// RecordsOutput returns an estimate of the number of pairs: the number of
// left records times the number of records that one index search returns.
func (p *IndexJoinPlan) RecordsOutput() int {
	return p.p1.RecordsOutput() * p.info.RecordsOutput()
}

// DistinctValues returns an estimate of the number of distinct values of
// fieldName in the pairs. It is assumed to equal the count on the field's own
// side: the left plan's count for a field of the left side, and the table's
// count for any other field.
func (p *IndexJoinPlan) DistinctValues(fieldName string) int {
	if p.p1.Schema().HasField(fieldName) {
		return p.p1.DistinctValues(fieldName)
	}

	return p.p2.DistinctValues(fieldName)
}

// Schema returns the fields of both sides, with those of the left plan first.
func (p *IndexJoinPlan) Schema() *recordmanager.Schema {
	return p.schema
}
