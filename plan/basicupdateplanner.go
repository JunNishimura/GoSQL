package plan

import (
	"fmt"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// BasicUpdatePlanner executes the statements that change the database in the
// simplest possible way: it scans every record of the table and changes the
// records that satisfy the predicate.
//
// Each method returns the number of records that it changed. A statement that
// changes definitions and not records returns 0.
//
// It changes only tables. It does not update the indexes of a table that it
// writes to, so their searches would then return deleted records and miss new
// ones. A database with indexes must use IndexUpdatePlanner instead.
type BasicUpdatePlanner struct {
	mm *metadatamanager.MetadataManager
}

// NewBasicUpdatePlanner returns a planner that accesses tables and their
// definitions through mm.
func NewBasicUpdatePlanner(mm *metadatamanager.MetadataManager) *BasicUpdatePlanner {
	return &BasicUpdatePlanner{
		mm: mm,
	}
}

// ExecuteInsert inserts one record with the given values, each in the field
// that the statement lists for it. A field that is not listed keeps its zero
// value.
func (up *BasicUpdatePlanner) ExecuteInsert(data parse.InsertData, tx *transaction.Transaction) (int, error) {
	us, err := openUpdateScan(up.mm, data.TableName(), tx)
	if err != nil {
		return 0, err
	}
	defer us.Close()

	if err := insertRecord(us, data); err != nil {
		return 0, err
	}

	return 1, nil
}

// insertRecord inserts a new record through us and writes the values of data
// to it, each to the field that the statement lists for it. It leaves us on
// that record.
func insertRecord(us query.UpdateScan, data parse.InsertData) error {
	if err := us.MoveToNewRecord(); err != nil {
		return err
	}

	values := data.Values()
	for i, fieldName := range data.Fields() {
		if err := us.SetValue(fieldName, values[i]); err != nil {
			return err
		}
	}

	return nil
}

// ExecuteDelete deletes every record of the table that satisfies the
// predicate.
func (up *BasicUpdatePlanner) ExecuteDelete(data parse.DeleteData, tx *transaction.Transaction) (int, error) {
	return forEachKept(up.mm, data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		return us.DeleteCurrentRecord()
	})
}

// ExecuteModify sets the target field of every record that satisfies the
// predicate to the new value. The new value is evaluated against that record,
// so a field in it refers to the record's own value.
func (up *BasicUpdatePlanner) ExecuteModify(data parse.ModifyData, tx *transaction.Transaction) (int, error) {
	return forEachKept(up.mm, data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		val, err := data.NewValue().Evaluate(us)
		if err != nil {
			return err
		}

		return us.SetValue(data.TargetField(), val)
	})
}

// ExecuteCreateTable stores the definition of the table.
func (up *BasicUpdatePlanner) ExecuteCreateTable(data parse.CreateTableData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateTable(tx, data.TableName(), data.Schema())
}

// ExecuteCreateView stores the definition of the view.
//
// The definition is not checked against the catalogs. A view on a table that
// does not exist is rejected only when a query reads the view.
func (up *BasicUpdatePlanner) ExecuteCreateView(data parse.CreateViewData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateView(tx, data.ViewName(), data.ViewDef())
}

// ExecuteCreateIndex stores the definition of the index.
func (up *BasicUpdatePlanner) ExecuteCreateIndex(data parse.CreateIndexData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateIndex(tx, data.IndexName(), data.TableName(), data.FieldName())
}

// forEachKept calls change for every record of the table that satisfies pred,
// and returns the number of such records.
//
// The predicate is tested here and not through a select scan, because a
// select scan is a Scan and not an UpdateScan. A select would only open the
// table scan and test each record, and this function needs that on a scan
// that supports writes.
func forEachKept(mm *metadatamanager.MetadataManager, tableName string, pred query.Predicate, tx *transaction.Transaction, change func(us query.UpdateScan) error) (int, error) {
	us, err := openUpdateScan(mm, tableName, tx)
	if err != nil {
		return 0, err
	}
	defer us.Close()

	count := 0
	for {
		hasNext, err := us.MoveToNextRecord()
		if err != nil {
			return 0, err
		}
		if !hasNext {
			return count, nil
		}

		kept, err := pred.IsSatisfied(us)
		if err != nil {
			return 0, err
		}
		if !kept {
			continue
		}

		if err := change(us); err != nil {
			return 0, err
		}
		count++
	}
}

// openUpdateScan opens a scan of the table that supports writes. It reads the
// definition of the table through mm.
func openUpdateScan(mm *metadatamanager.MetadataManager, tableName string, tx *transaction.Transaction) (query.UpdateScan, error) {
	tp, err := NewTablePlan(tx, tableName, mm)
	if err != nil {
		return nil, err
	}

	s, err := tp.Open()
	if err != nil {
		return nil, err
	}

	us, ok := s.(query.UpdateScan)
	if !ok {
		s.Close()
		return nil, fmt.Errorf("open table %q for writing: its scan is a %T, not an UpdateScan", tableName, s)
	}

	return us, nil
}
