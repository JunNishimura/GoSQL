package metadatamanager

import (
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
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
}

// NewIndexInfo returns what is known about the index named indexName, which is
// on fieldName of a table whose schema is tableSchema and whose measurements
// are tableStatistics.
func NewIndexInfo(
	indexName string,
	fieldName string,
	tableSchema *recordmanager.Schema,
	tableStatistics *TableStatistics,
) *IndexInfo {
	return &IndexInfo{
		indexName:       indexName,
		fieldName:       fieldName,
		tableSchema:     tableSchema,
		tableStatistics: tableStatistics,
	}
}
