package metadatamanager

import (
	"fmt"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
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
// It holds no transaction. Every manager in this package is handed one per
// call rather than keeping one, since what a transaction may read depends on
// when it is asked, and an object outliving the transaction it was built with
// would go on answering from it. The calls here that need one take one.
type IndexInfo struct {
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
	indexName string,
	fieldName string,
	tableSchema *recordmanager.Schema,
	tableStatistics *TableStatistics,
) (*IndexInfo, error) {
	indexLayout, err := createIndexLayout(tableSchema, fieldName)
	if err != nil {
		return nil, err
	}

	return &IndexInfo{
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
