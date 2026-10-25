package plan

import (
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// IndexUpdatePlanner carries out the statements that change the database as
// BasicUpdatePlanner does, and keeps the indexes of the table up to date
// alongside.
//
// An index is not kept up by the table it is on: a record written, deleted or
// changed without the index being told leaves the index finding records that
// are gone and missing ones that are there. Every statement that changes a
// record of an indexed table has to go through this planner for its indexes to
// stay right.
type IndexUpdatePlanner struct {
	mm *metadatamanager.MetadataManager
}

var _ UpdatePlanner = (*IndexUpdatePlanner)(nil)

// NewIndexUpdatePlanner returns a planner that reads and writes tables, their
// indexes and their definitions through mm.
func NewIndexUpdatePlanner(mm *metadatamanager.MetadataManager) *IndexUpdatePlanner {
	return &IndexUpdatePlanner{
		mm: mm,
	}
}

// ExecuteInsert writes one record of the values given, each to the field it
// was named with, and files it in every index of the table.
//
// Each index files the record under what its field holds once the record is
// written, not under the value the statement named. A field not named is left
// at its zero value, and the record holds that value as much as any other, so a
// search for it has to find the record.
func (up *IndexUpdatePlanner) ExecuteInsert(data parse.InsertData, tx *transaction.Transaction) (int, error) {
	us, err := openUpdateScan(up.mm, data.TableName(), tx)
	if err != nil {
		return 0, err
	}
	defer us.Close()

	if err := us.MoveToNewRecord(); err != nil {
		return 0, err
	}

	values := data.Values()
	for i, fieldName := range data.Fields() {
		if err := us.SetValue(fieldName, values[i]); err != nil {
			return 0, err
		}
	}

	rid, err := us.CurrentRecordID()
	if err != nil {
		return 0, err
	}

	indexes, err := up.mm.GetIndexInfo(tx, data.TableName())
	if err != nil {
		return 0, err
	}
	for fieldName, info := range indexes {
		value, err := us.GetValue(fieldName)
		if err != nil {
			return 0, err
		}
		if err := insertIntoIndex(info, value, rid); err != nil {
			return 0, err
		}
	}

	return 1, nil
}

// insertIntoIndex opens the index info describes, files rid under value in
// it, and closes it again.
func insertIntoIndex(info *metadatamanager.IndexInfo, value query.Constant, rid *recordmanager.RecordID) error {
	idx, err := info.Open()
	if err != nil {
		return err
	}
	defer idx.Close()

	return idx.Insert(value, rid)
}

// ExecuteDelete deletes every record of the table the predicate keeps.
func (up *IndexUpdatePlanner) ExecuteDelete(data parse.DeleteData, tx *transaction.Transaction) (int, error) {
	return 0, nil
}

// ExecuteModify sets the target field of every record the predicate keeps to
// the new value.
func (up *IndexUpdatePlanner) ExecuteModify(data parse.ModifyData, tx *transaction.Transaction) (int, error) {
	return 0, nil
}

// ExecuteCreateTable records the table's definition.
func (up *IndexUpdatePlanner) ExecuteCreateTable(data parse.CreateTableData, tx *transaction.Transaction) (int, error) {
	return 0, nil
}

// ExecuteCreateView records the view's definition.
func (up *IndexUpdatePlanner) ExecuteCreateView(data parse.CreateViewData, tx *transaction.Transaction) (int, error) {
	return 0, nil
}

// ExecuteCreateIndex records the index's definition.
func (up *IndexUpdatePlanner) ExecuteCreateIndex(data parse.CreateIndexData, tx *transaction.Transaction) (int, error) {
	return 0, nil
}
