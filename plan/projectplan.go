package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*ProjectPlan)(nil)

// ProjectPlan is the plan that keeps only some fields of another plan: the
// column list of a query.
//
// It drops fields and not records, so every estimate it returns is the
// estimate of the underlying plan. Only its schema is its own.
type ProjectPlan struct {
	p          Plan
	fieldNames []string
	schema     *recordmanager.Schema
}

// NewProjectPlan returns the plan for p with only the named fields, in the
// order given.
//
// The schema is built here. A field that p does not have, or a field that is
// named twice, is therefore rejected here. A planner then learns of an
// invalid column list while it still compares plans, and not from the scan of
// the plan it chose.
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

// Open returns a project scan over a scan of the underlying plan.
//
// If the projection cannot be created, the underlying scan is closed, because
// nothing else holds it. This should not happen, because the fields were
// already checked against the schema of the plan. Without the close, a scan
// whose fields differ from its plan would leave its buffers pinned.
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

// BlocksAccessed returns the blocks of the underlying plan.
func (pp *ProjectPlan) BlocksAccessed() int {
	return pp.p.BlocksAccessed()
}

// RecordsOutput returns the records of the underlying plan.
func (pp *ProjectPlan) RecordsOutput() int {
	return pp.p.RecordsOutput()
}

// DistinctValues returns the distinct values of fieldName in the underlying
// plan.
func (pp *ProjectPlan) DistinctValues(fieldName string) int {
	return pp.p.DistinctValues(fieldName)
}

// Schema returns the kept fields, in the order in which they were named.
func (pp *ProjectPlan) Schema() *recordmanager.Schema {
	return pp.schema
}
