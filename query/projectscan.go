package query

import (
	"fmt"
	"slices"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ProjectScan is another scan with only some of its fields: the list of columns
// a query selects.
//
// It keeps every record and drops fields, which is the other half of what a
// select does. The two together are how a query says which rows and which
// columns, and neither has to know about the other.
//
// It is the one scan whose fields are not the fields of what it reads through.
// Every other one here answers HasField by passing the question down; this one
// answers from its own list, and refuses a field it does not keep even though
// the record underneath is holding it.
//
// Nothing is copied. A projection does not build a narrower record, it declines
// to hand over the fields it dropped, so walking one costs no more than walking
// what it reads through.
type ProjectScan struct {
	s          Scan
	fieldNames []string
}

var _ Scan = (*ProjectScan)(nil)

// NewProjectScan returns the scan of s with only the named fields.
//
// A field s does not have is refused here rather than at the read of it. Such a
// projection would answer HasField for the name, since the name is in its list,
// and then fail the read of it, and nothing downstream could tell which of the
// two answers to go by. Settling it at the start is what lets HasField be taken
// at its word everywhere else.
//
// The names are copied, so a caller that passes a slice it goes on to write to
// does not change what the projection keeps.
func NewProjectScan(s Scan, fieldNames ...string) (*ProjectScan, error) {
	for _, fieldName := range fieldNames {
		if !s.HasField(fieldName) {
			return nil, fmt.Errorf("project onto field %q, which the scan underneath does not have: %w", fieldName, recordmanager.ErrFieldNotFound)
		}
	}

	return &ProjectScan{
		s:          s,
		fieldNames: slices.Clone(fieldNames),
	}, nil
}

// MoveBeforeFirstRecord puts the projection back before its first record by
// putting the scan underneath back before its own.
func (ps *ProjectScan) MoveBeforeFirstRecord() error {
	return ps.s.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves on to the next record of the scan underneath, and
// reports whether there was one.
//
// Every record is kept. A projection narrows the records it is given rather
// than choosing between them, so there is nothing here to decide.
func (ps *ProjectScan) MoveToNextRecord() (bool, error) {
	return ps.s.MoveToNextRecord()
}

// GetInt returns the int field of the record the projection is on.
func (ps *ProjectScan) GetInt(fieldName string) (int32, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return 0, err
	}

	return ps.s.GetInt(fieldName)
}

// GetString returns the varchar field of the record the projection is on.
func (ps *ProjectScan) GetString(fieldName string) (string, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return "", err
	}

	return ps.s.GetString(fieldName)
}

// GetValue returns the field of the record the projection is on as a constant.
func (ps *ProjectScan) GetValue(fieldName string) (Constant, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return Constant{}, err
	}

	return ps.s.GetValue(fieldName)
}

// HasField reports whether the field is one the projection keeps.
//
// The list is walked rather than looked up. A query names a handful of columns,
// and keeping them in order is worth more than the lookup: the order is the one
// the fields were asked for in, which is what the output of the query is laid
// out by.
func (ps *ProjectScan) HasField(fieldName string) bool {
	return slices.Contains(ps.fieldNames, fieldName)
}

// Close closes the scan underneath, which is where the blocks a projection is
// holding really are.
func (ps *ProjectScan) Close() {
	ps.s.Close()
}

// requireKeptField reports that the field is not one this projection keeps,
// which is what the three field methods all have to settle before reading.
//
// The field may well be there in the record underneath. A projection that let
// it through would be handing back a column the query did not ask for, and one
// that a later scan built on this has been told is not there.
func (ps *ProjectScan) requireKeptField(fieldName string) error {
	if !ps.HasField(fieldName) {
		return fmt.Errorf("read field %q, which this projection does not keep: %w", fieldName, recordmanager.ErrFieldNotFound)
	}

	return nil
}
