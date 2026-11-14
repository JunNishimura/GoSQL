package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*ProductPlan)(nil)

// ProductPlan is the plan that pairs every record of one plan with every
// record of another. A join is built from it: a select then chooses which
// pairs to keep.
//
// The two sides differ in cost. The right side is read once for every record
// of the left side, so a planner uses these estimates to decide which plan
// goes on which side.
//
// The multiplications are not protected against overflow. An overflow would
// need about a hundred million records on each side, which is far beyond the
// intended scale of this database.
type ProductPlan struct {
	p1     Plan
	p2     Plan
	schema *recordmanager.Schema
}

// NewProductPlan returns the plan for the product of p1 and p2, with p1 on
// the left.
//
// A field that both sides have is rejected here, because a record of the
// product could not tell the two apart. A planner then learns of the conflict
// while it still compares plans, before any scan is opened.
func NewProductPlan(p1, p2 Plan) (*ProductPlan, error) {
	schema := recordmanager.NewSchema()
	if err := schema.AddAll(p1.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the left side of a product: %w", err)
	}
	if err := schema.AddAll(p2.Schema()); err != nil {
		return nil, fmt.Errorf("take the fields of the right side of a product: %w", err)
	}

	return &ProductPlan{
		p1:     p1,
		p2:     p2,
		schema: schema,
	}, nil
}

// Open returns a product scan over scans of the two plans.
//
// If a later step fails, a scan that is already open is closed, because
// nothing else holds it.
func (pp *ProductPlan) Open() (query.Scan, error) {
	s1, err := pp.p1.Open()
	if err != nil {
		return nil, err
	}

	s2, err := pp.p2.Open()
	if err != nil {
		s1.Close()
		return nil, err
	}

	ps, err := query.NewProductScan(s1, s2)
	if err != nil {
		s1.Close()
		s2.Close()
		return nil, err
	}

	return ps, nil
}

// BlocksAccessed returns the blocks of one read of the left side, plus one
// read of the right side for every record of the left side.
func (pp *ProductPlan) BlocksAccessed() int {
	return pp.p1.BlocksAccessed() + pp.p1.RecordsOutput()*pp.p2.BlocksAccessed()
}

// RecordsOutput returns the number of pairs: the records of the left side
// times the records of the right side.
func (pp *ProductPlan) RecordsOutput() int {
	return pp.p1.RecordsOutput() * pp.p2.RecordsOutput()
}

// DistinctValues returns the distinct values of fieldName on the side that
// has the field. Every value of a side appears in the product, paired with
// the records of the other side, so the product changes no field's count.
func (pp *ProductPlan) DistinctValues(fieldName string) int {
	if pp.p1.Schema().HasField(fieldName) {
		return pp.p1.DistinctValues(fieldName)
	}

	return pp.p2.DistinctValues(fieldName)
}

// Schema returns the fields of the left side followed by those of the right
// side.
func (pp *ProductPlan) Schema() *recordmanager.Schema {
	return pp.schema
}
