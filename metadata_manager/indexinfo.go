package metadatamanager

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// The fields of an index's own records.
//
// An index is kept as a table like anything else, and one of its records says
// that a value is held somewhere: the first two fields are a record id taken
// apart, and the third is the value that record is filed under.
const (
	// indexBlockNumberField is the block of the indexed table the record sits
	// in.
	indexBlockNumberField = "block_number"
	// indexSlotField is the slot of that block. Together with the block number
	// it is a record id, which is how a table is read from an index without
	// reading the records before the one wanted.
	indexSlotField = "slot"
	// indexedValueField is a copy of what the indexed field holds in that
	// record. It is what the index is searched by, so it is kept here rather
	// than read from the table, which is the whole point of having the index.
	indexedValueField = "indexed_value"
)

// IndexInfo is what is known about one index: which field of which table it is
// on, and what a plan that went through it would cost.
//
// Those are two different kinds of thing to hold in one place, and the name is
// vague because of it. An index is described here so that a planner can find
// out one index exists on a field, and costed here so that it can work out
// whether going through it beats reading the table. The two are kept together
// because the second is answered from the first: what an index costs follows
// from the shape of the records it holds, which follows from the field it is
// on.
//
// It holds a transaction, where the managers in this package take one per call.
// They are built once and live as long as the database, so a transaction of
// theirs would be one they had outlived. This is built for one query out of
// what a manager read under that query's transaction, and answers only while
// that query is being planned, so the transaction is as short-lived as it is.
// Both of the things it has to do with one, opening the index and costing a
// search through it, would otherwise be handed back the same transaction it was
// described under.
//
// What that buys is paid for if one of these is kept past its query: it would
// go on reading through a transaction that had ended. Nothing here stops that,
// and nothing should hold one longer than the plan it was made for.
type IndexInfo struct {
	// tx is the transaction this was described under, and the one it reads
	// through.
	tx *transaction.Transaction
	// indexName is what the index is called. It is the name of the table its
	// own records are kept in, since an index is stored as a table like
	// anything else.
	indexName string
	// fieldName is the field of the indexed table this index is on. One index
	// covers one field.
	fieldName string
	// tableSchema is the schema of the table being indexed. It is what says
	// how wide the indexed field is, which is what the index's own records are
	// laid out from.
	tableSchema *recordmanager.Schema
	// tableStatistics is what is known about the indexed table. The cost of
	// going through the index follows from how many records the table holds,
	// so the answer comes from here rather than from the index itself.
	tableStatistics *TableStatistics
	// indexLayout is the shape of the index's own records, which is worked out
	// from the indexed field rather than fixed. It is what says how many index
	// records go in a block, and that is what a search through the index costs.
	indexLayout *recordmanager.Layout
}

// NewIndexInfo returns what is known about the index named indexName, which is
// on fieldName of a table whose schema is tableSchema and whose measurements
// are tableStatistics.
//
// The shape of the index's records is worked out here rather than when it is
// first needed, so that an index on a field the table does not have is refused
// where it is described. Such an index is nothing at all: it has no records to
// hold and no shape for the records it does not hold, and every call on it
// would fail one at a time for the same reason.
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

	// An index whose own records do not fit in a block is refused here. A record
	// page would refuse it too, so it could never be opened, but the cost of a
	// search through it is asked before it is opened, and that is worked out by
	// dividing by how many of its records go in a block. For this one that is
	// none, and dividing by it would take the database down rather than report
	// an index nobody can have.
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

// createIndexLayout works out the shape of the records an index on fieldName
// holds.
//
// Two of the three fields are the same for every index, since every index says
// where a record is in the same way. The third is the value the record is filed
// under, and it takes its type and width from the indexed field: what an index
// files is whatever that field holds. That is why this is worked out per index
// rather than written down once.
//
// It is a function rather than a method because the constructor calls it. As a
// method it could only be reached through the constructor, and a test of it
// would have to go through the thing it is part of building.
func createIndexLayout(tableSchema *recordmanager.Schema, fieldName string) (*recordmanager.Layout, error) {
	fieldType, err := tableSchema.Type(fieldName)
	if err != nil {
		return nil, fmt.Errorf("lay out an index on field %q: %w", fieldName, err)
	}

	// Length cannot fail here: Type has already found the field in the same
	// schema. The error is returned rather than dropped so that this stays true
	// if it grows another reason to refuse one.
	length, err := tableSchema.Length(fieldName)
	if err != nil {
		return nil, err
	}

	schema := recordmanager.NewSchema()

	// Adding a field cannot fail here either, since the three names are fixed
	// by this package and no two of them are the same.
	if err := schema.AddIntField(indexBlockNumberField); err != nil {
		return nil, err
	}
	if err := schema.AddIntField(indexSlotField); err != nil {
		return nil, err
	}
	// The length is passed whatever the type is, since a schema ignores it for
	// an int and holds 0 for one, which is what the indexed field gives back.
	if err := schema.AddField(indexedValueField, fieldType, length); err != nil {
		return nil, err
	}

	return recordmanager.NewLayout(schema), nil
}

// BlocksAccessed is how many blocks a search through this index touches.
//
// It is the size of the index in blocks: one index record stands for one record
// of the table, so how many blocks they take follows from how many of them go
// in one. That is worked out from the index's own records rather than the
// table's, and those are three fields against however many the table has, which
// is the whole reason a search through an index beats reading the table.
//
// Reading all of it is the most a search could cost. What a real index charges
// is less, and by how much is its own business: a hash index reads the one
// bucket a key falls in, a B-tree the one path down to it. Until there is an
// index to ask, this stands in for the answer, and it errs towards saying an
// index costs more than it does.
//
// The division rounds up, since records that fill a block and start another are
// in two blocks, and the second is read like the first.
func (info *IndexInfo) BlocksAccessed() int {
	recordsPerBlock := info.tx.BlockSize() / info.indexLayout.SlotSize()

	return ceilDivide(info.tableStatistics.RecordsOutput(), recordsPerBlock)
}

// RecordsOutput is how many records a search through this index gives back.
//
// A search is for one value, so what comes back is the records holding that
// value rather than the whole table: the records spread over the values the
// indexed field takes, and one value's worth of them.
//
// This is what an index is for. A table of a hundred records whose indexed
// field takes thirty-four different values gives back two of them per search,
// and it is against that two that reading all hundred is weighed.
func (info *IndexInfo) RecordsOutput() int {
	return info.tableStatistics.RecordsOutput() / info.tableStatistics.DistinctValues(info.fieldName)
}

// DistinctValues is how many different values fieldName takes among the records
// a search through this index gives back.
//
// For the indexed field it is one. Every record that came back was filed under
// the value that was searched for, so that field holds that one value across
// all of them however many the table holds.
//
// For any other field it is whatever the table says. Picking out the records
// that share one value of the indexed field says nothing about how varied some
// other field is among them, and this layer has no way to find out; taking the
// table's answer amounts to supposing the two fields have nothing to do with
// each other, which is the most that can be said without looking.
func (info *IndexInfo) DistinctValues(fieldName string) int {
	if fieldName == info.fieldName {
		return 1
	}

	return info.tableStatistics.DistinctValues(fieldName)
}

// ceilDivide is dividend/divisor rounded up.
//
// Blocks are counted this way because a part of a block is read as a whole one:
// records that fill one and start another are in two, and both are read.
func ceilDivide(dividend int, divisor int) int {
	return (dividend + divisor - 1) / divisor
}
