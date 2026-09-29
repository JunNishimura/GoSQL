package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

var _ Plan = (*ProductPlan)(nil)

// ProductPlan is the plan of putting every record of one plan beside every
// record of another: what two tables are joined out of, before a select says
// which pairs to keep.
//
// The two sides are not alike in cost. The right side is read once for every
// record of the left, so which plan goes on which side is a choice a planner
// makes by these numbers.
//
// The products are not guarded against overflow. It would take sides of about
// a hundred million records each to reach it, which is far past what this
// database is built for.
type ProductPlan struct {
	p1     Plan
	p2     Plan
	schema *recordmanager.Schema
}

// NewProductPlan returns the plan of the product of p1 and p2, with p1 on the
// left.
//
// A field both sides have is refused here. A record of the product could not
// say which of the two it meant, and settling that before anything is opened
// lets a planner hear of it while it is still weighing plans.
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
// A scan already opened is closed if a later step fails, since nothing else
// would hold it to close.
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

// BlocksAccessed is one read of the left side plus a read of the right side
// for every record of the left.
func (pp *ProductPlan) BlocksAccessed() int {
	return pp.p1.BlocksAccessed() + pp.p1.RecordsOutput()*pp.p2.BlocksAccessed()
}

// RecordsOutput is every record of the left beside every record of the right.
func (pp *ProductPlan) RecordsOutput() int {
	return pp.p1.RecordsOutput() * pp.p2.RecordsOutput()
}

// DistinctValues is the distinct values of fieldName on whichever side has it.
// Every value of a side turns up in the product beside the records of the
// other, so the product changes no field's count.
func (pp *ProductPlan) DistinctValues(fieldName string) int {
	if pp.p1.Schema().HasField(fieldName) {
		return pp.p1.DistinctValues(fieldName)
	}

	return pp.p2.DistinctValues(fieldName)
}

// Schema is the fields of the left followed by those of the right.
func (pp *ProductPlan) Schema() *recordmanager.Schema {
	return pp.schema
}
