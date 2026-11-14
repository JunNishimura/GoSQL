package plan

import (
	"fmt"

	"github.com/JunNishimura/GoSQL/index"
	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// IndexUpdatePlanner executes the statements that change the database in the
// same way as BasicUpdatePlanner, and also updates the indexes of the table.
//
// A table does not maintain its indexes. If a record is inserted, deleted or
// modified without an update of the index, an index search returns deleted
// records and misses new ones. Every statement that changes a record of an
// indexed table must therefore go through this planner.
type IndexUpdatePlanner struct {
	mm *metadatamanager.MetadataManager
}

var _ UpdatePlanner = (*IndexUpdatePlanner)(nil)

// NewIndexUpdatePlanner returns a planner that accesses tables, their indexes
// and their definitions through mm.
func NewIndexUpdatePlanner(mm *metadatamanager.MetadataManager) *IndexUpdatePlanner {
	return &IndexUpdatePlanner{
		mm: mm,
	}
}

// ExecuteInsert inserts one record with the given values, each in the field
// that the statement lists for it, and adds an entry for the record to every
// index of the table.
//
// The key of each index entry is the value of the indexed field after the
// record is written, not the value in the statement. A field that is not
// listed keeps its zero value. That is a real value of the record, so a
// search for it must find the record.
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

	indexes, err := openIndexes(up.mm, data.TableName(), tx)
	if err != nil {
		return 0, err
	}
	defer closeIndexes(indexes)

	for fieldName, idx := range indexes {
		value, err := us.GetValue(fieldName)
		if err != nil {
			return 0, err
		}
		if err := idx.Insert(value, rid); err != nil {
			return 0, err
		}
	}

	return 1, nil
}

// ExecuteDelete deletes every record of the table that satisfies the
// predicate, and removes its entry from every index of the table.
//
// The entries are removed before the record is deleted, because their keys
// are read from the record. If an index has no entry for a record, the index
// is out of sync with the table. The statement then fails and does not skip
// the record, because the index can no longer be trusted.
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

// openIndexes opens every index of tableName and returns them as a map from
// the indexed field to its index. A statement updates them while it scans the
// table.
//
// The indexes are opened once per statement and not once per record, because
// opening a B-tree reads its files. An index pins no block between two
// searches, so keeping all of them open costs nothing while the statement
// processes other records. If an open fails, the indexes already opened are
// closed.
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

// ExecuteModify sets the target field of every record that satisfies the
// predicate to the new value. The new value is evaluated against that record,
// as in BasicUpdatePlanner. In the index on the target field, the entry of
// the record is moved from the old value to the new value.
//
// Only that index is updated. An index on another field has a key that the
// statement does not change, and its entry still points to the same record,
// so it stays correct.
func (up *IndexUpdatePlanner) ExecuteModify(data parse.ModifyData, tx *transaction.Transaction) (int, error) {
	targetField := data.TargetField()

	infos, err := up.mm.GetIndexInfo(tx, data.TableName())
	if err != nil {
		return 0, err
	}

	var idx index.Index
	if info, ok := infos[targetField]; ok {
		idx, err = info.Open()
		if err != nil {
			return 0, err
		}
		defer idx.Close()
	}

	return forEachKept(up.mm, data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		newValue, err := data.NewValue().Evaluate(us)
		if err != nil {
			return err
		}
		oldValue, err := us.GetValue(targetField)
		if err != nil {
			return err
		}

		if err := us.SetValue(targetField, newValue); err != nil {
			return err
		}
		if idx == nil {
			return nil
		}

		rid, err := us.CurrentRecordID()
		if err != nil {
			return err
		}
		if err := idx.Delete(oldValue, rid); err != nil {
			return err
		}

		return idx.Insert(newValue, rid)
	})
}

// ExecuteCreateTable stores the definition of the table. A new table has no
// index, so no index needs an update.
func (up *IndexUpdatePlanner) ExecuteCreateTable(data parse.CreateTableData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateTable(tx, data.TableName(), data.Schema())
}

// ExecuteCreateView stores the definition of the view. A view has no records
// of its own, so it has nothing to index.
func (up *IndexUpdatePlanner) ExecuteCreateView(data parse.CreateViewData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateView(tx, data.ViewName(), data.ViewDef())
}

// ExecuteCreateIndex stores the definition of the index and adds an entry for
// every existing record of the table.
//
// SimpleDB stores only the definition. The index then lacks every record that
// was written before it was created: a search misses those records, and a
// delete of one finds no entry to remove. Building the index from the table
// here lets this planner keep it correct afterwards.
//
// The table and the field are looked up before anything is written. A catalog
// entry for an index on a missing table or field could never be described,
// and every planner that reads the table would fail when it requests the
// indexes.
func (up *IndexUpdatePlanner) ExecuteCreateIndex(data parse.CreateIndexData, tx *transaction.Transaction) (int, error) {
	layout, err := up.mm.GetLayout(tx, data.TableName())
	if err != nil {
		return 0, err
	}
	if _, err := layout.Schema().Type(data.FieldName()); err != nil {
		return 0, fmt.Errorf("index field %q of table %q: %w", data.FieldName(), data.TableName(), err)
	}

	if err := up.mm.CreateIndex(tx, data.IndexName(), data.TableName(), data.FieldName()); err != nil {
		return 0, err
	}

	return 0, up.fillIndex(data, layout, tx)
}

// fillIndex adds an entry to the index of data for every record of the table
// of data. The key of each entry is the value of the indexed field in the
// record.
func (up *IndexUpdatePlanner) fillIndex(data parse.CreateIndexData, layout *recordmanager.Layout, tx *transaction.Transaction) error {
	infos, err := up.mm.GetIndexInfo(tx, data.TableName())
	if err != nil {
		return err
	}
	// The index was just registered for this field, so the map has it.
	idx, err := infos[data.FieldName()].Open()
	if err != nil {
		return err
	}
	defer idx.Close()

	ts, err := query.NewTableScan(tx, data.TableName(), layout)
	if err != nil {
		return err
	}
	defer ts.Close()

	for {
		hasNext, err := ts.MoveToNextRecord()
		if err != nil || !hasNext {
			return err
		}

		value, err := ts.GetValue(data.FieldName())
		if err != nil {
			return err
		}
		rid, err := ts.CurrentRecordID()
		if err != nil {
			return err
		}
		if err := idx.Insert(value, rid); err != nil {
			return err
		}
	}
}
