package index

import (
	"fmt"
	"math"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// Suffixes appended to the name of a B-tree index to name its two files.
//
// The suffixes are letters so that the file names cannot collide with the
// buckets of a hash index of the same name. A bucket name is the index name
// followed by a number.
const (
	leafFileSuffix = "leaf"
	dirFileSuffix  = "dir"
)

// BTreeIndex is an index that stores its entries in leaf blocks, sorted by
// key. A directory tree maps each key to the leaf that holds it.
//
// The leaves are stored in one file and the directory in another. Both files
// are named after the index. A search descends the directory from the root to
// the one leaf that can hold the key, and reads the entries there. The cost
// of a search therefore depends on the depth of the tree, not on the size of
// the index.
type BTreeIndex struct {
	tx           *transaction.Transaction
	indexName    string
	leafFileName string
	leafLayout   *recordmanager.Layout
	dirLayout    *recordmanager.Layout
	// rootBlk is the root block of the directory. It is always the first
	// block of the directory file, however deep the tree grows.
	rootBlk *filemanager.BlockId
	// leaf reads the leaf of the current search. It is nil when no search is
	// in progress.
	leaf *btreeLeaf
}

var _ Index = (*BTreeIndex)(nil)

// NewBTreeIndex opens the B-tree index named indexName. leafLayout is the
// layout of its leaf entries. The index files are created if they do not
// exist yet.
//
// A new index starts with one empty leaf. Its root holds one directory entry
// that maps the minimum key of the indexed field's type to that leaf, so
// every possible key belongs to some leaf.
func NewBTreeIndex(tx *transaction.Transaction, indexName string, leafLayout *recordmanager.Layout) (*BTreeIndex, error) {
	leafFileName := indexName + leafFileSuffix
	if err := createIfEmpty(tx, leafFileName, leafLayout, -1, nil); err != nil {
		return nil, err
	}

	dirLayout, err := newDirLayout(leafLayout)
	if err != nil {
		return nil, err
	}
	least, err := leastValue(leafLayout)
	if err != nil {
		return nil, err
	}
	dirFileName := indexName + dirFileSuffix
	firstLeaf := &dirEntry{value: least, childBlockNumber: 0}
	// The flag of a directory block is its level. At level 0, the children
	// are leaves.
	if err := createIfEmpty(tx, dirFileName, dirLayout, 0, firstLeaf); err != nil {
		return nil, err
	}

	return &BTreeIndex{
		tx:           tx,
		indexName:    indexName,
		leafFileName: leafFileName,
		leafLayout:   leafLayout,
		dirLayout:    dirLayout,
		rootBlk:      filemanager.NewBlockId(dirFileName, 0),
	}, nil
}

// createIfEmpty appends the first block to fileName if the file has no
// blocks. The block's flag is set to flag, and first is inserted when it is
// not nil. A file that already has blocks belongs to an existing index, and
// is left unchanged.
func createIfEmpty(
	tx *transaction.Transaction,
	fileName string,
	layout *recordmanager.Layout,
	flag int32,
	first *dirEntry,
) error {
	size, err := tx.Size(fileName)
	if err != nil {
		return fmt.Errorf("measure B-tree file %s: %w", fileName, err)
	}
	if size > 0 {
		return nil
	}

	blk, err := appendBTreeBlock(tx, fileName, layout, flag)
	if err != nil {
		return err
	}
	if first == nil {
		return nil
	}

	page, err := newBTreePage(tx, blk, layout)
	if err != nil {
		return err
	}
	defer page.close()

	return page.insertDir(0, first.value, first.childBlockNumber)
}

// newDirLayout returns the layout of the directory entries for a B-tree whose
// leaf entries use leafLayout. A directory entry holds a child block number
// and a key. The key has the same type and length as in a leaf entry.
func newDirLayout(leafLayout *recordmanager.Layout) (*recordmanager.Layout, error) {
	leafSchema := leafLayout.Schema()

	fieldType, err := leafSchema.Type(IndexedValueField)
	if err != nil {
		return nil, err
	}
	length, err := leafSchema.Length(IndexedValueField)
	if err != nil {
		return nil, err
	}

	schema := recordmanager.NewSchema()
	if err := schema.AddIntField(BlockNumberField); err != nil {
		return nil, err
	}
	if err := schema.AddField(IndexedValueField, fieldType, length); err != nil {
		return nil, err
	}

	return recordmanager.NewLayout(schema), nil
}

// leastValue returns the minimum key for the leaf entries that use
// leafLayout. This is the minimum int for an int key, and the empty string
// for a varchar key.
func leastValue(leafLayout *recordmanager.Layout) (query.Constant, error) {
	fieldType, err := leafLayout.Schema().Type(IndexedValueField)
	if err != nil {
		return query.Constant{}, err
	}

	if fieldType == recordmanager.FieldTypeInt {
		return query.NewIntConstant(math.MinInt32), nil
	}

	return query.NewStringConstant(""), nil
}

// MoveBeforeFirstMatch descends the directory to the leaf that searchKey
// belongs to, and opens that leaf before the first entry for searchKey.
//
// The directory is only read during the descent. Its block is unpinned once
// the leaf is known, and from then on the search pins only the leaf.
func (bi *BTreeIndex) MoveBeforeFirstMatch(searchKey query.Constant) error {
	bi.Close()

	root, err := newBTreeDir(bi.tx, bi.rootBlk, bi.dirLayout)
	if err != nil {
		return err
	}
	leafBlkNum, err := root.search(searchKey)
	root.close()
	if err != nil {
		return err
	}

	leaf, err := newBTreeLeaf(bi.tx, filemanager.NewBlockId(bi.leafFileName, leafBlkNum), bi.leafLayout, searchKey)
	if err != nil {
		return err
	}
	bi.leaf = leaf

	return nil
}

// MoveToNextMatch moves to the next entry for the search key.
func (bi *BTreeIndex) MoveToNextMatch() (bool, error) {
	if err := bi.requireSearch(); err != nil {
		return false, err
	}

	return bi.leaf.next()
}

// DataRecordID returns the record id of the table record that the current
// entry points to.
func (bi *BTreeIndex) DataRecordID() (*recordmanager.RecordID, error) {
	if err := bi.requireSearch(); err != nil {
		return nil, err
	}

	return bi.leaf.dataRecordID()
}

// requireSearch returns ErrNoSearch unless a leaf is open. A leaf is open
// from a search until the index is closed, or until an entry is inserted or
// deleted.
func (bi *BTreeIndex) requireSearch() error {
	if bi.leaf == nil {
		return fmt.Errorf("read B-tree index %s: %w", bi.indexName, ErrNoSearch)
	}

	return nil
}

// Insert adds an entry that maps value to rid.
//
// Insert searches for value to find the leaf, so it discards any search in
// progress. It unpins the leaf once the entry is inserted.
//
// If the leaf splits, Insert adds a directory entry for the new leaf,
// starting from the root. Without that entry, a search would not find the
// entries moved to the new leaf. If the root splits as a result, the tree
// grows by one level. The root block stays at the same position.
func (bi *BTreeIndex) Insert(value query.Constant, rid *recordmanager.RecordID) error {
	if err := bi.MoveBeforeFirstMatch(value); err != nil {
		return err
	}
	e, err := bi.leaf.insert(rid)
	bi.Close()
	if err != nil || e == nil {
		return err
	}

	root, err := newBTreeDir(bi.tx, bi.rootBlk, bi.dirLayout)
	if err != nil {
		return err
	}
	defer root.close()

	fromRoot, err := root.insert(*e)
	if err != nil || fromRoot == nil {
		return err
	}

	return root.makeNewRoot(*fromRoot)
}

// Delete removes the entry that maps value to rid. It returns
// ErrEntryNotFound if no such entry exists.
//
// Delete searches for value to find the entry, so it discards any search in
// progress. It unpins the leaf once the entry is removed. The directory does
// not change: a leaf with fewer entries is not merged with its neighbor, so
// the directory still maps value to that leaf.
func (bi *BTreeIndex) Delete(value query.Constant, rid *recordmanager.RecordID) error {
	if err := bi.MoveBeforeFirstMatch(value); err != nil {
		return err
	}
	err := bi.leaf.delete(rid)
	bi.Close()

	return err
}

// BTreeIndexSearchCost returns the number of blocks that a search reads in a
// B-tree index with numBlocks leaf blocks, where each block holds
// recordsPerBlock records.
//
// A search reads one directory block per level and then one leaf. The root
// always exists, so the tree has at least one level. Each level multiplies
// the number of reachable blocks by the fan-out of a directory block. The
// number of levels is therefore the smallest n for which the fan-out to the
// power n reaches numBlocks. This is the logarithm of numBlocks, rounded up.
//
// The function computes this by repeated multiplication instead of a
// floating-point logarithm. At an exact power of the fan-out, a
// floating-point result can fall slightly above or below the integer, and
// the level count would then be off by one.
//
// The fan-out is assumed to be recordsPerBlock, the number of leaf records in
// a block, because that is the only figure the planner has. A directory
// record is narrower than a leaf record, so the real fan-out is higher. The
// estimate can therefore have more levels than the tree, which overstates the
// cost of using the index.
//
// A block that holds one record cannot branch. Its fan-out is taken as 2, the
// minimum that can form a tree.
//
// The estimate also assumes that every leaf is full and that all entries for
// one key fit in one leaf. Neither always holds. A split leaves both leaves
// half full, and the entries for one key can overflow into a chain of blocks.
// In both cases a search reads more blocks than this estimate.
func BTreeIndexSearchCost(numBlocks int, recordsPerBlock int) int {
	fanOut := max(recordsPerBlock, 2)

	levels := 1
	for reach := fanOut; reach < numBlocks; reach *= fanOut {
		levels++
	}

	return levels + 1
}

// Close unpins the block of the open leaf, if there is one.
func (bi *BTreeIndex) Close() {
	if bi.leaf == nil {
		return
	}

	bi.leaf.close()
	bi.leaf = nil
}
