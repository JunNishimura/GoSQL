package query

// SelectScan is the records of another scan that meet a predicate: the "where"
// of a query.
//
// It keeps whole records. Every field of the scan underneath is still there,
// and each record handed out is one of that scan's records rather than
// something built out of them, which is why reading a field here is a read of
// the scan underneath with nothing in between and why the records come out in
// the order they were already in. Dropping fields is a projection's job.
//
// It holds no blocks of its own. Everything it hands out belongs to the scan it
// reads through, which is also why closing it is closing that one.
//
// It is a Scan and not an UpdateScan, even though a record it hands out is a
// record of a real table whenever the scan underneath is one, and so could be
// written to. Writing through a select is what an update statement needs, and
// the planner that runs those is not here yet. When it is, it gets a type whose
// constructor takes an UpdateScan, rather than this one asking at every write
// whether the scan it was handed happens to be updatable.
type SelectScan struct {
	s    Scan
	pred Predicate
}

var _ Scan = (*SelectScan)(nil)

// NewSelectScan returns the scan of the records of s that meet pred.
//
// The predicate is not checked against what s has. A term naming a field that
// is not there comes out when the select is walked, since that is the first
// point at which there is a record to read it from.
func NewSelectScan(s Scan, pred Predicate) *SelectScan {
	return &SelectScan{
		s:    s,
		pred: pred,
	}
}

// MoveBeforeFirstRecord puts the select back before its first record by putting
// the scan underneath back before its own.
//
// It keeps nothing of its own to reset. Where the select is, is where the scan
// underneath is, so starting that one over starts this one over.
func (sc *SelectScan) MoveBeforeFirstRecord() error {
	return sc.s.MoveBeforeFirstRecord()
}

// MoveToNextRecord moves on to the next record of the scan underneath that
// meets the predicate, and reports whether there was one.
//
// The records in between are read and dropped. That is what a select costs
// without an index to go on: every record of the input is looked at once, and
// the predicate settles it.
//
// The predicate is tested against the scan underneath rather than against this
// one. Both answer alike, since every read here goes straight through, and
// asking the one underneath keeps each field read from going down a level for
// nothing.
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

// GetInt returns the int field of the record the select is on.
func (sc *SelectScan) GetInt(fieldName string) (int32, error) {
	return sc.s.GetInt(fieldName)
}

// GetString returns the varchar field of the record the select is on.
func (sc *SelectScan) GetString(fieldName string) (string, error) {
	return sc.s.GetString(fieldName)
}

// GetValue returns the field of the record the select is on as a constant.
func (sc *SelectScan) GetValue(fieldName string) (Constant, error) {
	return sc.s.GetValue(fieldName)
}

// HasField reports whether the field is one the select has, which is one the
// scan underneath has: a select drops records and keeps every field of the ones
// it keeps.
func (sc *SelectScan) HasField(fieldName string) bool {
	return sc.s.HasField(fieldName)
}

// Close closes the scan underneath, which is where the blocks a select is
// holding really are.
func (sc *SelectScan) Close() {
	sc.s.Close()
}
