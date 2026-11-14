package query

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ProductScan pairs every record of one scan with every record of another.
//
// A join is built from a product, but a product is not a join. A join is a
// select over a product: the product produces all pairs, and the predicate
// chooses which pairs to keep. This separation allows a join on any condition
// that a predicate can express, not only on conditions built into a join
// operator.
//
// The right side is scanned completely once for each record of the left
// side, which is the cost of a product without an index. The product holds
// only the current left record and the current position of the right side.
// The two scans hold the blocks, and the product holds none itself.
type ProductScan struct {
	s1 Scan
	s2 Scan
	// onLeftRecord reports whether s1 is on a record. If the left side is
	// exhausted, or had no records at all, the product has no left record to
	// pair the right side with.
	onLeftRecord bool
}

var _ Scan = (*ProductScan)(nil)

// NewProductScan returns the product of s1 and s2, positioned before its
// first record.
//
// It reads from s1 before it returns. The first record of the product pairs
// the first left record with the first right record, so the left side must
// already be on a record when the right side is read. This changes a scan
// that the caller passed in. That is acceptable, because a product takes
// ownership of both sides and closes them.
func NewProductScan(s1, s2 Scan) (*ProductScan, error) {
	ps := &ProductScan{
		s1: s1,
		s2: s2,
	}

	if err := ps.MoveBeforeFirstRecord(); err != nil {
		return nil, err
	}

	return ps, nil
}

// MoveBeforeFirstRecord positions the product before its first pair.
//
// The left side is positioned on its first record, not before it. This is the
// one place where the two sides are treated differently. The next move
// advances only the right side, so the left side must already be on a record.
//
// A left side with no records is recorded here. The right side then has no
// left record to pair with, and the product is empty whatever the right side
// holds.
func (ps *ProductScan) MoveBeforeFirstRecord() error {
	if err := ps.s1.MoveBeforeFirstRecord(); err != nil {
		return err
	}

	onLeftRecord, err := ps.s1.MoveToNextRecord()
	if err != nil {
		return err
	}
	ps.onLeftRecord = onLeftRecord

	return ps.s2.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves to the next pair and reports whether one exists.
//
// The next pair is the next right record with the current left record. When
// the right side is exhausted, the left side advances by one record and the
// right side restarts.
//
// This is a loop and not a single step. With an empty right side, a single
// step would end the scan at the first left record. In the loop, the right
// side is empty again for each left record, until the left side is exhausted.
// The result is correct, because a product with an empty side is empty, and
// it is reached by the same code path as any other product.
func (ps *ProductScan) MoveToNextRecord() (bool, error) {
	for ps.onLeftRecord {
		onRightRecord, err := ps.s2.MoveToNextRecord()
		if err != nil {
			return false, err
		}
		if onRightRecord {
			return true, nil
		}

		onLeftRecord, err := ps.s1.MoveToNextRecord()
		if err != nil {
			return false, err
		}
		ps.onLeftRecord = onLeftRecord
		if !ps.onLeftRecord {
			return false, nil
		}

		if err := ps.s2.MoveBeforeFirstRecord(); err != nil {
			return false, err
		}
	}

	return false, nil
}

// GetInt returns the int field fieldName of the current pair, from the side
// that has the field.
func (ps *ProductScan) GetInt(fieldName string) (int32, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return 0, err
	}

	return side.GetInt(fieldName)
}

// GetString returns the varchar field fieldName of the current pair, from the
// side that has the field.
func (ps *ProductScan) GetString(fieldName string) (string, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return "", err
	}

	return side.GetString(fieldName)
}

// GetValue returns the field fieldName of the current pair as a constant,
// from the side that has the field.
func (ps *ProductScan) GetValue(fieldName string) (Constant, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return Constant{}, err
	}

	return side.GetValue(fieldName)
}

// HasField reports whether either side has the field. A product has the
// fields of both sides, so two tables can be read as one sequence of records.
func (ps *ProductScan) HasField(fieldName string) bool {
	return ps.s1.HasField(fieldName) || ps.s2.HasField(fieldName)
}

// Close closes both sides. If one side stayed open, its buffer would remain
// pinned until the end of the transaction, long after the query finished.
func (ps *ProductScan) Close() {
	ps.s1.Close()
	ps.s2.Close()
}

// sideFor returns the side that a field is read from, or an error if neither
// side has it.
//
// The left side is checked first, so a name that both sides have is read from
// the left. A name shared by two tables is ambiguous in the query. This code
// cannot resolve it, because a field name carries no table name. Reading from
// the left is a fixed rule, not a resolution. It makes the result
// deterministic instead of dependent on the order of the code.
func (ps *ProductScan) sideFor(fieldName string) (Scan, error) {
	if ps.s1.HasField(fieldName) {
		return ps.s1, nil
	}
	if ps.s2.HasField(fieldName) {
		return ps.s2, nil
	}

	return nil, fmt.Errorf("read field %q, which neither side of the product has: %w", fieldName, recordmanager.ErrFieldNotFound)
}
