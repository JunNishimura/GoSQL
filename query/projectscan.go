package query

import (
	"fmt"
	"slices"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

// ProjectScan is a scan that exposes only some fields of another scan: the
// column list of a query.
//
// It keeps every record and drops fields. A select does the opposite. The two
// together express which rows and which columns a query returns, and neither
// depends on the other.
//
// It is the only scan whose fields differ from the fields of its underlying
// scan. Every other scan here forwards HasField to its input. ProjectScan
// answers from its own list, and rejects a field that it does not keep even
// if the underlying record has it.
//
// Nothing is copied. A projection does not build a narrower record. It only
// rejects reads of the dropped fields, so scanning it costs no more than
// scanning its input.
type ProjectScan struct {
	s          Scan
	fieldNames []string
}

var _ Scan = (*ProjectScan)(nil)

// NewProjectScan returns a scan of s with only the named fields.
//
// A field that s does not have is rejected here, not when it is read.
// Otherwise the projection would report true from HasField for the name,
// because the name is in its list, and then fail to read the field. Later
// code could not tell which answer is right. With the check here, HasField is
// reliable everywhere else.
//
// The names are copied. A caller that later writes to the slice it passed
// does not change the fields of the projection.
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

// MoveBeforeFirstRecord positions the projection before its first record by
// positioning the underlying scan before its first record.
func (ps *ProjectScan) MoveBeforeFirstRecord() error {
	return ps.s.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves to the next record of the underlying scan and
// reports whether one exists.
//
// Every record is kept. A projection drops fields, not records, so it has no
// decision to make here.
func (ps *ProjectScan) MoveToNextRecord() (bool, error) {
	return ps.s.MoveToNextRecord()
}

// GetInt returns the int field fieldName of the current record.
func (ps *ProjectScan) GetInt(fieldName string) (int32, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return 0, err
	}

	return ps.s.GetInt(fieldName)
}

// GetString returns the varchar field fieldName of the current record.
func (ps *ProjectScan) GetString(fieldName string) (string, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return "", err
	}

	return ps.s.GetString(fieldName)
}

// GetValue returns the field fieldName of the current record as a constant.
func (ps *ProjectScan) GetValue(fieldName string) (Constant, error) {
	if err := ps.requireKeptField(fieldName); err != nil {
		return Constant{}, err
	}

	return ps.s.GetValue(fieldName)
}

// HasField reports whether the projection keeps the field.
//
// The list is searched linearly, not through a map. A query names only a few
// columns, and their order matters more than lookup speed: it is the order in
// which the fields were requested, and the query output follows it.
func (ps *ProjectScan) HasField(fieldName string) bool {
	return slices.Contains(ps.fieldNames, fieldName)
}

// Close closes the underlying scan, which holds the blocks that the
// projection reads.
func (ps *ProjectScan) Close() {
	ps.s.Close()
}

// requireKeptField returns an error if the projection does not keep the
// field. The three field methods all call it before they read.
//
// The underlying record can have the field. A projection that returned it
// would expose a column that the query did not request, and that a scan built
// on this one was told does not exist.
func (ps *ProjectScan) requireKeptField(fieldName string) error {
	if !ps.HasField(fieldName) {
		return fmt.Errorf("read field %q, which this projection does not keep: %w", fieldName, recordmanager.ErrFieldNotFound)
	}

	return nil
}
