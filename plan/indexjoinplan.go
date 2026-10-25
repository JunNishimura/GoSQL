package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// IndexJoinPlan is the plan of joining the records of one plan with those of a
// table on a field of the first and an indexed field of the table, by
// searching the index for each record of the first rather than reading the
// whole table for it.
//
// It stands in for a select over the product of the two on a predicate that
// equates the two fields: it gives back the same pairs, having read only the
// records of the table that are in one.
type IndexJoinPlan struct {
	p1        Plan
	p2        *TablePlan
	info      *metadatamanager.IndexInfo
	joinField string
	schema    *recordmanager.Schema
}

var _ Plan = (*IndexJoinPlan)(nil)

// NewIndexJoinPlan returns the plan of joining the records of p1 with those of
// the table p2 reads, where what a record of p1 holds in joinField is what the
// index info describes files the records of p2 under.
//
// It fails as a product would if the two sides share a field name, since the
// pairs would then have two fields of one name.
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

// Open opens the left plan, the index and the table, and returns a scan of the
// pairs the join keeps.
//
// Whatever was opened before a failure is closed again, since no scan comes
// back for the caller to close.
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

// BlocksAccessed is a guess at how many blocks reading the plan touches: the
// left side read once, a search through the index for each of its records,
// and a block of the table for each pair, as for an index select.
func (p *IndexJoinPlan) BlocksAccessed() int {
	return p.p1.BlocksAccessed() + p.p1.RecordsOutput()*p.info.BlocksAccessed() + p.RecordsOutput()
}

// RecordsOutput is a guess at how many pairs the plan gives back: as many for
// each record on the left as a search through the index gives back.
func (p *IndexJoinPlan) RecordsOutput() int {
	return p.p1.RecordsOutput() * p.info.RecordsOutput()
}

// DistinctValues is a guess at how many different values fieldName holds in
// the pairs the plan gives back, which is taken to be what it holds on its own
// side: the left plan's for a field of the left, and the table's for any other.
func (p *IndexJoinPlan) DistinctValues(fieldName string) int {
	if p.p1.Schema().HasField(fieldName) {
		return p.p1.DistinctValues(fieldName)
	}

	return p.p2.DistinctValues(fieldName)
}

// Schema is the fields of both sides, the left plan's first.
func (p *IndexJoinPlan) Schema() *recordmanager.Schema {
	return p.schema
}
