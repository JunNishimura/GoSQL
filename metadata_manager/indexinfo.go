package metadatamanager

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// IndexInfo describes one index: the table and field it is on, and the
// estimated cost of a plan that uses it.
//
// These are two different responsibilities in one type, which is why the name
// is vague. The description lets a planner find out that an index exists on a
// field. The cost estimate lets the planner decide whether the index is
// cheaper than a table scan. Both are in one type because the cost is derived
// from the description: the cost of an index depends on the layout of its
// records, and the layout depends on the indexed field.
//
// An IndexInfo holds a transaction, while the managers in this package take
// one per call. A manager is created once and lives as long as the database,
// so it would outlive any transaction it held. An IndexInfo is created for
// one query, from data that a manager read in the transaction of that query.
// It is used only while that query is planned, so it lives no longer than the
// transaction. Opening the index and estimating a search both need a
// transaction, and the caller would otherwise have to pass the same one back.
//
// The cost of this design is that an IndexInfo must not be kept after its
// query. It would then read through a transaction that has ended. Nothing
// here prevents that, so no caller should hold an IndexInfo longer than the
// plan it was created for.
type IndexInfo struct {
	// tx is the transaction in which this info was read. The info also reads
	// through it.
	tx *transaction.Transaction
	// indexName is the name of the index. It is also the name of the table
	// that stores the index records, because an index is stored as a table.
	indexName string
	// fieldName is the indexed field of the table. One index covers one
	// field.
	fieldName string
	// tableSchema is the schema of the indexed table. It gives the type and
	// length of the indexed field, which determine the layout of the index
	// records.
	tableSchema *recordmanager.Schema
	// tableStatistics holds the statistics of the indexed table. The cost of
	// an index search depends on the number of records in the table, so the
	// estimate uses these statistics and not the index itself.
	tableStatistics *TableStatistics
	// indexLayout is the layout of the index records. It is derived from the
	// indexed field and is not fixed. It determines how many index records
	// fit in a block, which determines the cost of an index search.
	indexLayout *recordmanager.Layout
}

// NewIndexInfo returns the info for the index named indexName. The index is
// on fieldName of a table with the schema tableSchema and the statistics
// tableStatistics.
//
// The layout of the index records is computed here and not on first use. An
// index on a field that the table does not have is therefore rejected where
// it is described. Such an index is unusable: it has no records and no
// layout, and every call on it would fail for the same reason.
func NewIndexInfo(
	tx *transaction.Transaction,
	indexName string,
	fieldName string,
	tableSchema *recordmanager.Schema,
	tableStatistics *TableStatistics,
) (*IndexInfo, error) {
	indexLayout, err := createIndexLayout(tableSchema, fieldName)
	if err != nil {
		return nil, err
	}

	// An index whose records do not fit in a block is rejected here. A record
	// page would reject it too, so it could never be opened. However, the
	// cost of a search is requested before the index is opened, and the cost
	// divides by the number of index records per block. For such an index
	// that number is zero, and the division would panic instead of reporting
	// an invalid index.
	if slotSize := indexLayout.SlotSize(); slotSize > tx.BlockSize() {
		return nil, fmt.Errorf(
			"describe an index on field %q, whose records are %d bytes and a block of %d bytes cannot hold one of: %w",
			fieldName, slotSize, tx.BlockSize(), recordmanager.ErrSlotWiderThanBlock,
		)
	}

	return &IndexInfo{
		tx:              tx,
		indexName:       indexName,
		fieldName:       fieldName,
		tableSchema:     tableSchema,
		tableStatistics: tableStatistics,
		indexLayout:     indexLayout,
	}, nil
}

// createIndexLayout computes the layout of the records of an index on
// fieldName.
//
// Two of the three fields are the same for every index, because every index
// stores a record id in the same way. The third field is the key. It takes
// its type and length from the indexed field, because the key is a copy of
// that field's value. The layout is therefore computed per index and is not a
// constant.
//
// This is a function and not a method because the constructor calls it. A
// method could only be called on a constructed value, so a test of it would
// have to go through the constructor that it is part of.
func createIndexLayout(tableSchema *recordmanager.Schema, fieldName string) (*recordmanager.Layout, error) {
	fieldType, err := tableSchema.Type(fieldName)
	if err != nil {
		return nil, fmt.Errorf("lay out an index on field %q: %w", fieldName, err)
	}

	// Length cannot fail here, because Type has already found the field in the
	// same schema. The error is still returned, so that the code stays correct
	// if Length gains another failure case.
	length, err := tableSchema.Length(fieldName)
	if err != nil {
		return nil, err
	}

	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here either, because this package defines the
	// three names and they are all different.
	if err := schema.AddIntField(index.BlockNumberField); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(index.SlotField); err != nil {
		return nil, err
	}
	// The length is passed for either type. A schema ignores it for an int and
	// stores 0, which is also the length that the indexed int field returns.
	if err := schema.AddField(index.IndexedValueField, fieldType, length); err != nil {
		return nil, err
	}

	return recordmanager.NewLayout(schema), nil
}

// Open opens the index, in the transaction in which this info was read. Every
// index is a B-tree index.
//
// Open can fail, because opening a B-tree reads its files, and creates them
// the first time the index is opened.
func (info *IndexInfo) Open() (index.Index, error) {
	idx, err := index.NewBTreeIndex(info.tx, info.indexName, info.indexLayout)
	if err != nil {
		return nil, fmt.Errorf("open index %s: %w", info.indexName, err)
	}

	return idx, nil
}

// BlocksAccessed returns the estimated number of blocks that a search through
// this index reads.
//
// The estimate starts from the size of the index in blocks. The index has one
// record per table record, so its size follows from the number of index
// records per block. That number comes from the layout of the index records,
// not of the table records. An index record has three fields, and a table
// record usually has more. This difference is why an index search is cheaper
// than a table scan.
//
// A search does not read the whole index. How much it reads depends on the
// index type, so the index type computes the final estimate. Every index is a
// B-tree, as in Open. A B-tree search reads one block per directory level and
// then one leaf.
//
// The division rounds up. Records that fill one block and start another
// occupy two blocks, and the second block is read like the first.
func (info *IndexInfo) BlocksAccessed() int {
	recordsPerBlock := info.tx.BlockSize() / info.indexLayout.SlotSize()
	numBlocks := ceilDivide(info.tableStatistics.RecordsOutput(), recordsPerBlock)

	return index.BTreeIndexSearchCost(numBlocks, recordsPerBlock)
}

// RecordsOutput returns the estimated number of records that a search through
// this index returns.
//
// A search is for one value, so it returns the records with that value and
// not the whole table. The estimate is the number of records divided by the
// number of distinct values of the indexed field.
//
// This is the benefit of an index. For a table of a hundred records whose
// indexed field has thirty-four distinct values, a search returns about two
// records. A planner compares those two with a scan of all hundred.
func (info *IndexInfo) RecordsOutput() int {
	return info.tableStatistics.RecordsOutput() / info.tableStatistics.DistinctValues(info.fieldName)
}

// DistinctValues returns the estimated number of distinct values of fieldName
// among the records that a search through this index returns.
//
// For the indexed field the result is one. Every returned record has the
// value that was searched for, whatever the size of the table.
//
// For any other field the result is the table's estimate. Selecting the
// records with one value of the indexed field gives no information about the
// variety of another field among them, and this layer cannot measure it.
// Using the table's estimate assumes that the two fields are independent,
// which is the best available assumption without reading the data.
func (info *IndexInfo) DistinctValues(fieldName string) int {
	if fieldName == info.fieldName {
		return 1
	}

	return info.tableStatistics.DistinctValues(fieldName)
}

// ceilDivide returns dividend/divisor, rounded up.
//
// Blocks are counted this way because a partly used block is read as a whole
// block. Records that fill one block and start another occupy two blocks, and
// both are read.
func ceilDivide(dividend int, divisor int) int {
	return (dividend + divisor - 1) / divisor
}
