package query

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ProductScan is every record of one scan put beside every record of another.
//
// It is what two tables are joined out of, but it is not the join. A join is a
// select over a product: the product says which pairs there are, and the
// predicate says which of them to keep. Keeping the two apart is what lets a
// query be joined on any condition that can be written as a predicate, rather
// than only the ones a join operator was taught.
//
// The right side is read from start to end once for every record of the left,
// which is what a product costs with nothing to go on. Only the record of the
// left and wherever the right has reached are held at a time, so the two scans
// between them hold the blocks and the product holds none of its own.
type ProductScan struct {
	s1 Scan
	s2 Scan
	// onLeftRecord is whether s1 is on a record. A left side that has run out,
	// or that had no records to begin with, leaves the product with nothing to
	// read the right side against.
	onLeftRecord bool
}

var _ Scan = (*ProductScan)(nil)

// NewProductScan returns the product of s1 and s2, placed before its first
// record.
//
// It reads from s1 before returning, because the product's first record is the
// first of the left beside the first of the right, so the left has to be on one
// already for the right to be read against it. That is a change to a scan the
// caller handed over, which is fair: a product takes its two sides as its own
// and closes them.
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

// MoveBeforeFirstRecord puts the product back before its first pair.
//
// The left side is left on its first record rather than before it, which is the
// one place the two sides are not treated alike. Only the right side is walked
// by the next move, so the left has to be somewhere for that walk to mean
// anything.
//
// A left side with no records at all is remembered here. There is then nothing
// for the right to be read against, and the product is empty however many
// records the right side holds.
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

// MoveToNextRecord moves on to the next pair and reports whether there was one.
//
// The next pair is the next record of the right side against the record of the
// left. When the right side runs out, the left moves on by one and the right is
// read again from the start.
//
// That is a loop rather than a single step because a right side with no records
// would otherwise end the walk at the first record of the left. Read again for
// the next record of the left it is empty again, and so on until the left runs
// out, which is the answer wanted — a product with an empty side is empty —
// reached by the same path every other product takes.
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

// GetInt returns the int field of the pair the product is on, from whichever
// side has it.
func (ps *ProductScan) GetInt(fieldName string) (int32, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return 0, err
	}

	return side.GetInt(fieldName)
}

// GetString returns the varchar field of the pair the product is on, from
// whichever side has it.
func (ps *ProductScan) GetString(fieldName string) (string, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return "", err
	}

	return side.GetString(fieldName)
}

// GetValue returns the field of the pair the product is on as a constant, from
// whichever side has it.
func (ps *ProductScan) GetValue(fieldName string) (Constant, error) {
	side, err := ps.sideFor(fieldName)
	if err != nil {
		return Constant{}, err
	}

	return side.GetValue(fieldName)
}

// HasField reports whether either side has the field. A product has the fields
// of both, which is what makes two tables readable as one run of records.
func (ps *ProductScan) HasField(fieldName string) bool {
	return ps.s1.HasField(fieldName) || ps.s2.HasField(fieldName)
}

// Close closes both sides. A product holding one of them open would leave that
// side's buffer taken for the rest of the transaction, however long ago the
// query was finished with.
func (ps *ProductScan) Close() {
	ps.s1.Close()
	ps.s2.Close()
}

// sideFor is the side a field is read from, and the error when it is on
// neither.
//
// The left is asked first, so a name both sides have is read from the left. A
// name two tables share is ambiguous in the query that used it, and nothing
// here can say which was meant, since a field name carries no table with it.
// Answering from the left is a rule rather than a resolution: it makes the
// ambiguity come out the same way every time instead of following whichever
// side the code happened to reach first.
func (ps *ProductScan) sideFor(fieldName string) (Scan, error) {
	if ps.s1.HasField(fieldName) {
		return ps.s1, nil
	}
	if ps.s2.HasField(fieldName) {
		return ps.s2, nil
	}

	return nil, fmt.Errorf("read field %q, which neither side of the product has: %w", fieldName, recordmanager.ErrFieldNotFound)
}
