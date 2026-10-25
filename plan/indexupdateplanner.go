package plan

import (
	"github.com/JunNishimura/GoSQL/index"
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

	if err := insertRecord(us, data); err != nil {
		return 0, err
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

// ExecuteDelete deletes every record of the table the predicate keeps, and
// takes out the entry each index of the table files it under.
//
// The entries go before the record does, since what they are filed under is
// read from the record. An index found not to hold the entry for a record is
// behind the table, and that fails the statement rather than being passed
// over: what it would go on finding is no longer there to find.
func (up *IndexUpdatePlanner) ExecuteDelete(data parse.DeleteData, tx *transaction.Transaction) (int, error) {
	indexes, err := openIndexes(up.mm, data.TableName(), tx)
	if err != nil {
		return 0, err
	}
	defer closeIndexes(indexes)

	return forEachKept(up.mm, data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		rid, err := us.CurrentRecordID()
		if err != nil {
			return err
		}

		for fieldName, idx := range indexes {
			value, err := us.GetValue(fieldName)
			if err != nil {
				return err
			}
			if err := idx.Delete(value, rid); err != nil {
				return err
			}
		}

		return us.DeleteCurrentRecord()
	})
}

// openIndexes opens every index of tableName, filed under the field each is
// on, for a statement to keep up as it goes through the table.
//
// They are opened once for the statement rather than once per record it
// changes, since opening a B-tree reads its files. An index holds no block
// between one search and the next, so holding them all open costs nothing
// while the statement is on some other record. What was opened before a
// failure is closed again.
func openIndexes(mm *metadatamanager.MetadataManager, tableName string, tx *transaction.Transaction) (map[string]index.Index, error) {
	infos, err := mm.GetIndexInfo(tx, tableName)
	if err != nil {
		return nil, err
	}

	indexes := make(map[string]index.Index, len(infos))
	for fieldName, info := range infos {
		idx, err := info.Open()
		if err != nil {
			closeIndexes(indexes)

			return nil, err
		}
		indexes[fieldName] = idx
	}

	return indexes, nil
}

// closeIndexes closes every index openIndexes opened.
func closeIndexes(indexes map[string]index.Index) {
	for _, idx := range indexes {
		idx.Close()
	}
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
