package index

import (
	"fmt"
	"hash/fnv"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// HashIndex is an index that spreads its entries over a fixed number of
// buckets by a hash of the value they are filed under.
//
// Each bucket is a table of its own, named after the index and the bucket's
// number. A search reads the one bucket its value hashes to and passes over
// the entries there that were filed under some other value, so what it costs
// is the size of a bucket rather than of the whole index.
type HashIndex struct {
	tx        *transaction.Transaction
	indexName string
	layout    *recordmanager.Layout
	// searchKey is the value being searched for, and is only meaningful while
	// ts is open.
	searchKey query.Constant
	// ts reads the bucket searchKey hashes to. It is nil until the first
	// search, since which bucket to open is not known before then.
	ts *query.TableScan
}

var _ Index = (*HashIndex)(nil)

// NewHashIndex returns the hash index named indexName, whose entries are laid
// out as layout says.
//
// Nothing is opened here, so nothing can fail: a bucket is opened by the
// search that needs it.
func NewHashIndex(tx *transaction.Transaction, indexName string, layout *recordmanager.Layout) *HashIndex {
	return &HashIndex{
		tx:        tx,
		indexName: indexName,
		layout:    layout,
	}
}

// MoveBeforeFirstMatch closes whichever bucket was open and opens the one
// searchKey hashes to, before its first entry.
func (hi *HashIndex) MoveBeforeFirstMatch(searchKey query.Constant) error {
	hi.Close()

	hi.searchKey = searchKey
	bucketName := fmt.Sprintf("%s%d", hi.indexName, bucketOf(searchKey))
	ts, err := query.NewTableScan(hi.tx, bucketName, hi.layout)
	if err != nil {
		return fmt.Errorf("open bucket %s of index %s: %w", bucketName, hi.indexName, err)
	}
	hi.ts = ts

	return nil
}

// MoveToNextMatch moves on through the bucket to the next entry filed under
// the search key, passing over the entries of other values that hash to the
// same bucket.
func (hi *HashIndex) MoveToNextMatch() (bool, error) {
	for {
		ok, err := hi.ts.MoveToNextRecord()
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}

		value, err := hi.ts.GetValue(IndexedValueField)
		if err != nil {
			return false, err
		}
		if value == hi.searchKey {
			return true, nil
		}
	}
}

// DataRecordID puts the record id of the current entry back together from the
// two fields it was taken apart into.
func (hi *HashIndex) DataRecordID() (*recordmanager.RecordID, error) {
	blkNum, err := hi.ts.GetInt(BlockNumberField)
	if err != nil {
		return nil, err
	}
	slot, err := hi.ts.GetInt(SlotField)
	if err != nil {
		return nil, err
	}

	return recordmanager.NewRecordID(int(blkNum), int(slot)), nil
}

// Insert adds an entry filing rid under value to the bucket value hashes to.
//
// It searches for value to get there, so whatever search was under way is
// dropped, as it is by any other search.
func (hi *HashIndex) Insert(value query.Constant, rid *recordmanager.RecordID) error {
	if err := hi.MoveBeforeFirstMatch(value); err != nil {
		return err
	}
	if err := hi.ts.MoveToNewRecord(); err != nil {
		return err
	}
	if err := hi.ts.SetInt(BlockNumberField, int32(rid.BlockNumber())); err != nil {
		return err
	}
	if err := hi.ts.SetInt(SlotField, int32(rid.Slot())); err != nil {
		return err
	}

	return hi.ts.SetValue(IndexedValueField, value)
}

func (hi *HashIndex) Delete(value query.Constant, rid *recordmanager.RecordID) error {
	return nil
}

// Close gives back the block of whichever bucket is open.
func (hi *HashIndex) Close() {
	if hi.ts == nil {
		return
	}

	hi.ts.Close()
	hi.ts = nil
}

// numBuckets is how many buckets a hash index spreads its entries over.
const numBuckets = 100

// bucketOf is the number of the bucket the entries filed under value go in.
//
// The bucket a value goes in is written to disk as part of a file name, so the
// hash has to come out the same in every process that opens the index. That
// rules out hash/maphash, whose seed is picked afresh each time; an int is
// taken as it is and a varchar through FNV-1a, neither of which has a seed.
//
// The remainder is kept non-negative. Go's % takes the sign of the dividend,
// and a negative int would otherwise land in a bucket that does not exist.
func bucketOf(value query.Constant) int {
	if value.Type() == recordmanager.FieldTypeInt {
		// AsInt cannot fail here: the type has just been checked.
		n, _ := value.AsInt()

		return ((int(n) % numBuckets) + numBuckets) % numBuckets
	}

	// AsString cannot fail either, a constant being one of the two kinds.
	s, _ := value.AsString()
	h := fnv.New32a()
	// Writing to a hash never fails.
	_, _ = h.Write([]byte(s))

	return int(h.Sum32() % numBuckets)
}
