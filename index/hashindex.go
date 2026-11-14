package index

import (
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// ErrEntryNotFound is returned when a delete targets an entry that the index
// does not hold.
//
// An entry is only deleted together with the record it points to. A missing
// entry therefore means that the index and its table are already out of sync.
// The delete reports this instead of ignoring it, so the inconsistency is
// detected where it first appears.
var ErrEntryNotFound = errors.New("index entry not found")

// ErrNoSearch is returned when an index is read while no search is in
// progress: either no search has been started, or the index has been closed
// since.
var ErrNoSearch = errors.New("index has no search under way")

// HashIndex is an index that distributes its entries over a fixed number of
// buckets by a hash of their key.
//
// Each bucket is a separate table, named after the index and the bucket
// number. A search reads the one bucket that its key hashes to, and skips the
// entries there that have another key. The cost of a search therefore depends
// on the size of one bucket, not on the size of the index.
type HashIndex struct {
	tx        *transaction.Transaction
	indexName string
	layout    *recordmanager.Layout
	// searchKey is the key of the current search. It is only meaningful while
	// ts is open.
	searchKey query.Constant
	// ts scans the bucket that searchKey hashes to. It is nil before the first
	// search, because the bucket to open is not known until then.
	ts *query.TableScan
}

var _ Index = (*HashIndex)(nil)

// NewHashIndex returns the hash index named indexName, whose entries use
// layout.
//
// NewHashIndex opens nothing, so it cannot fail. Each search opens the bucket
// it needs.
func NewHashIndex(tx *transaction.Transaction, indexName string, layout *recordmanager.Layout) *HashIndex {
	return &HashIndex{
		tx:        tx,
		indexName: indexName,
		layout:    layout,
	}
}

// MoveBeforeFirstMatch closes the open bucket, if any, and opens the bucket
// that searchKey hashes to. The index is positioned before the first entry of
// that bucket.
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

// MoveToNextMatch moves through the bucket to the next entry for the search
// key. It skips entries for other keys that hash to the same bucket.
func (hi *HashIndex) MoveToNextMatch() (bool, error) {
	if err := hi.requireSearch(); err != nil {
		return false, err
	}

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

// DataRecordID builds the record id of the current entry from the two fields
// that store it.
func (hi *HashIndex) DataRecordID() (*recordmanager.RecordID, error) {
	if err := hi.requireSearch(); err != nil {
		return nil, err
	}

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

// Insert adds an entry that maps value to rid to the bucket that value hashes
// to.
//
// Insert searches for value to open the bucket, so it discards any search in
// progress.
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

// Delete removes the entry that maps value to rid.
//
// Delete searches for value to find the entry, so it discards any search in
// progress. Both the key and the record id must match: an entry in the same
// bucket with the same record id but another key is a different entry, and is
// not removed.
func (hi *HashIndex) Delete(value query.Constant, rid *recordmanager.RecordID) error {
	if err := hi.MoveBeforeFirstMatch(value); err != nil {
		return err
	}

	for {
		ok, err := hi.MoveToNextMatch()
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("delete %s filed under %s from index %s: %w", rid, value, hi.indexName, ErrEntryNotFound)
		}

		current, err := hi.DataRecordID()
		if err != nil {
			return err
		}
		if current.Equals(rid) {
			return hi.ts.DeleteCurrentRecord()
		}
	}
}

// requireSearch returns ErrNoSearch unless a bucket is open. A bucket is open
// from a search until the index is closed.
func (hi *HashIndex) requireSearch() error {
	if hi.ts == nil {
		return fmt.Errorf("read index %s: %w", hi.indexName, ErrNoSearch)
	}

	return nil
}

// Close unpins the block of the open bucket, if there is one.
func (hi *HashIndex) Close() {
	if hi.ts == nil {
		return
	}

	hi.ts.Close()
	hi.ts = nil
}

// numBuckets is the number of buckets in a hash index.
const numBuckets = 100

// HashIndexSearchCost returns the number of blocks that a search reads in a
// hash index with numBlocks blocks, where each block holds recordsPerBlock
// entries.
//
// A search reads the one bucket that its key hashes to. The buckets divide
// the blocks between them, so the estimate is one bucket's share of the
// blocks. This assumes that the keys are distributed evenly, which is the
// purpose of a hash. A key shared by many records makes its bucket larger
// than the share, and the estimate does not account for that.
//
// The share is rounded up. A bucket whose entries fill one block and start
// another occupies two blocks, and any non-empty bucket occupies at least
// one.
//
// recordsPerBlock is not used. It is a parameter because the cost of a B-tree
// search depends on it, and the planner should be able to pass the same
// arguments whatever the index type.
func HashIndexSearchCost(numBlocks int, recordsPerBlock int) int {
	return (numBlocks + numBuckets - 1) / numBuckets
}

// bucketOf returns the number of the bucket that holds the entries for value.
//
// The bucket number is written to disk as part of a file name, so the hash
// must give the same result in every process that opens the index. This rules
// out hash/maphash, which picks a new seed in each process. An int is used
// as it is, and a varchar is hashed with FNV-1a. Neither uses a seed.
//
// The remainder is made non-negative. Go's % operator takes the sign of the
// dividend, so a negative int would otherwise map to a bucket that does not
// exist.
func bucketOf(value query.Constant) int {
	if value.Type() == recordmanager.FieldTypeInt {
		// AsInt cannot fail here: the type has just been checked.
		n, _ := value.AsInt()

		return ((int(n) % numBuckets) + numBuckets) % numBuckets
	}

	// AsString cannot fail either: a constant is either an int or a string.
	s, _ := value.AsString()
	h := fnv.New32a()
	h.Write([]byte(s))

	return int(h.Sum32() % numBuckets)
}
