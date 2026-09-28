package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*ProjectPlan)(nil)

// ProjectPlan is the plan of keeping only some fields of another plan: the
// list of columns a query selects.
//
// It drops fields, not records, so every number it reports is the one of the
// plan underneath. Only its schema is its own.
type ProjectPlan struct {
	p          Plan
	fieldNames []string
	schema     *recordmanager.Schema
}

// NewProjectPlan returns the plan of p with only the named fields, in the
// order they are named.
//
// The schema is built here, which is where a field p does not have, or one
// named twice, is refused. Settling that before anything is opened means a
// planner hears of a bad column list while it is still weighing plans, rather
// than from the scan of whichever plan it picked.
func NewProjectPlan(p Plan, fieldNames ...string) (*ProjectPlan, error) {
	schema := recordmanager.NewSchema()
	for _, fieldName := range fieldNames {
		if err := schema.Add(fieldName, p.Schema()); err != nil {
			return nil, fmt.Errorf("project onto %v: %w", fieldNames, err)
		}
	}

	return &ProjectPlan{
		p:          p,
		fieldNames: schema.Fields(),
		schema:     schema,
	}, nil
}

// Open returns a project scan over a scan of the plan underneath.
//
// The scan underneath is closed if the projection cannot be made over it,
// since nothing else would hold it to close. That should not happen, as the
// fields were checked against the plan's schema already, but a scan whose
// fields drifted from its plan's would otherwise leave its buffers pinned.
func (pp *ProjectPlan) Open() (query.Scan, error) {
	s, err := pp.p.Open()
	if err != nil {
		return nil, err
	}

	ps, err := query.NewProjectScan(s, pp.fieldNames...)
	if err != nil {
		s.Close()
		return nil, err
	}

	return ps, nil
}

// BlocksAccessed is the blocks of the plan underneath.
func (pp *ProjectPlan) BlocksAccessed() int {
	return pp.p.BlocksAccessed()
}

// RecordsOutput is the records of the plan underneath.
func (pp *ProjectPlan) RecordsOutput() int {
	return pp.p.RecordsOutput()
}

// DistinctValues is the distinct values of fieldName in the plan underneath.
func (pp *ProjectPlan) DistinctValues(fieldName string) int {
	return pp.p.DistinctValues(fieldName)
}

// Schema is the fields kept, in the order they were named.
func (pp *ProjectPlan) Schema() *recordmanager.Schema {
	return pp.schema
}
