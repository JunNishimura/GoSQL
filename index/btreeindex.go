package index

import (
	"fmt"
	"math"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// What the name of a B-tree index is followed by to name its two files.
//
// They end in letters so that they never name a bucket of a hash index of the
// same name, whose buckets are named after the index and a number.
const (
	leafFileSuffix = "leaf"
	dirFileSuffix  = "dir"
)

// BTreeIndex is an index that keeps its entries in order of the value they are
// filed under, in the leaves of a tree whose directory says which leaf a value
// is in.
//
// The leaves are one file and the directory another, named after the index.
// A search goes down the directory from its root to the one leaf the value
// can be in, and reads its entries there, so what it costs is the depth of the
// tree rather than the size of the index.
type BTreeIndex struct {
	tx           *transaction.Transaction
	leafFileName string
	leafLayout   *recordmanager.Layout
	dirLayout    *recordmanager.Layout
	// rootBlk is the root of the directory. It is always the first block of
	// the directory file, however deep the tree grows.
	rootBlk *filemanager.BlockId
	// leaf reads the leaf being searched. It is nil until the first search,
	// and again once the index is closed.
	leaf *btreeLeaf
}

var _ Index = (*BTreeIndex)(nil)

// NewBTreeIndex opens the B-tree index named indexName, whose leaf entries
// are laid out as leafLayout says, making its files if they do not exist yet.
//
// A new index starts with one empty leaf, and a root that files it under the
// least value of the indexed field's type, so that every value there is falls
// into some leaf.
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
	// The flag is the root's level: at 0, its children are leaves.
	if err := createIfEmpty(tx, dirFileName, dirLayout, 0, firstLeaf); err != nil {
		return nil, err
	}

	return &BTreeIndex{
		tx:           tx,
		leafFileName: leafFileName,
		leafLayout:   leafLayout,
		dirLayout:    dirLayout,
		rootBlk:      filemanager.NewBlockId(dirFileName, 0),
	}, nil
}

// createIfEmpty gives fileName its first block, flagged with flag and holding
// first if it is not nil, unless the file already has blocks, which means the
// index was made before and is only being opened again.
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

// newDirLayout is the layout of the directory records of a B-tree whose leaf
// entries are laid out as leafLayout says: the child block, and the value the
// entries under it start from, of the same type and width as in a leaf.
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

// leastValue is the least value of the type the entries laid out as
// leafLayout are filed under: the least int, or the empty string, which comes
// before every other.
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

// MoveBeforeFirstMatch goes down the directory to the leaf searchKey is filed
// in and opens it before the first entry for searchKey.
func (bi *BTreeIndex) MoveBeforeFirstMatch(searchKey query.Constant) error {
	return nil
}

// MoveToNextMatch moves on to the next entry filed under the search key.
func (bi *BTreeIndex) MoveToNextMatch() (bool, error) {
	return false, nil
}

// DataRecordID is where the table record of the current entry is kept.
func (bi *BTreeIndex) DataRecordID() (*recordmanager.RecordID, error) {
	return nil, nil
}

// Insert files rid under value.
func (bi *BTreeIndex) Insert(value query.Constant, rid *recordmanager.RecordID) error {
	return nil
}

// Delete takes out the entry filing rid under value.
func (bi *BTreeIndex) Delete(value query.Constant, rid *recordmanager.RecordID) error {
	return nil
}

// Close gives back the block of whichever leaf is open.
func (bi *BTreeIndex) Close() {}
