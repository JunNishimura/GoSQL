package query

// SelectScan is a scan of the records of another scan that satisfy a
// predicate: the "where" clause of a query.
//
// It keeps whole records. Every field of the underlying scan is available,
// and each record is a record of that scan, not a copy. A field read here is
// therefore a direct read of the underlying scan, and the records keep their
// original order. Dropping fields is the task of a projection.
//
// It holds no blocks itself. Everything it returns belongs to the underlying
// scan, so closing it closes that scan.
//
// It is a Scan and not an UpdateScan. When the underlying scan is a table
// scan, its records are stored records and could be modified. The update
// planner, which modifies the records that a predicate keeps, does not use a
// select for this. It opens the table scan and tests the predicate on each
// record itself. A select would do nothing more, and this type then never has
// to check at each write whether its input is updatable.
type SelectScan struct {
	s    Scan
	pred Predicate
}

var _ Scan = (*SelectScan)(nil)

// NewSelectScan returns a scan of the records of s that satisfy pred.
//
// The predicate is not checked against the fields of s. A term with a missing
// field is reported when the scan is read, because that is the first time a
// record is available to evaluate the term against.
func NewSelectScan(s Scan, pred Predicate) *SelectScan {
	return &SelectScan{
		s:    s,
		pred: pred,
	}
}

// MoveBeforeFirstRecord positions the select before its first record by
// positioning the underlying scan before its first record.
//
// The select has no state of its own to reset. Its position is the position
// of the underlying scan.
func (sc *SelectScan) MoveBeforeFirstRecord() error {
	return sc.s.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves to the next record of the underlying scan that
// satisfies the predicate, and reports whether one exists.
//
// The records in between are read and skipped. This is the cost of a select
// without an index: every record of the input is read once and tested against
// the predicate.
//
// The predicate is evaluated against the underlying scan, not against this
// scan. Both give the same values, because every read here is forwarded
// unchanged. Evaluating against the underlying scan saves one level of
// forwarding for each field read.
func (sc *SelectScan) MoveToNextRecord() (bool, error) {
	for {
		onRecord, err := sc.s.MoveToNextRecord()
		if err != nil {
			return false, err
		}
		if !onRecord {
			return false, nil
		}

		satisfied, err := sc.pred.IsSatisfied(sc.s)
		if err != nil {
			return false, err
		}
		if satisfied {
			return true, nil
		}
	}
}

// GetInt returns the int field fieldName of the current record.
func (sc *SelectScan) GetInt(fieldName string) (int32, error) {
	return sc.s.GetInt(fieldName)
}

// GetString returns the varchar field fieldName of the current record.
func (sc *SelectScan) GetString(fieldName string) (string, error) {
	return sc.s.GetString(fieldName)
}

// GetValue returns the field fieldName of the current record as a constant.
func (sc *SelectScan) GetValue(fieldName string) (Constant, error) {
	return sc.s.GetValue(fieldName)
}

// HasField reports whether the underlying scan has the field. A select drops
// records, and keeps every field of the records it keeps.
func (sc *SelectScan) HasField(fieldName string) bool {
	return sc.s.HasField(fieldName)
}

// Close closes the underlying scan, which holds the blocks that the select
// reads.
func (sc *SelectScan) Close() {
	sc.s.Close()
}
