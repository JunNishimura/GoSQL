package plan

import (
	"fmt"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/parse"
	"github.com/JunNishimura/GoSQL/query"
	"github.com/JunNishimura/GoSQL/transaction"
)

// BasicUpdatePlanner carries out the statements that change the database, the
// simplest way there is: every record of the table is looked at, and the ones
// the predicate keeps are changed.
//
// Each method reports how many records it changed. A statement that changes
// definitions rather than records reports 0.
type BasicUpdatePlanner struct {
	mm *metadatamanager.MetadataManager
}

// NewBasicUpdatePlanner returns a planner that reads and writes tables and
// their definitions through mm.
func NewBasicUpdatePlanner(mm *metadatamanager.MetadataManager) *BasicUpdatePlanner {
	return &BasicUpdatePlanner{
		mm: mm,
	}
}

// ExecuteInsert writes one record of the values given, each to the field it
// was named with. A field not named is left at its zero value.
func (up *BasicUpdatePlanner) ExecuteInsert(data parse.InsertData, tx *transaction.Transaction) (int, error) {
	us, err := up.openUpdateScan(data.TableName(), tx)
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

	return 1, nil
}

// ExecuteDelete deletes every record of the table the predicate keeps.
func (up *BasicUpdatePlanner) ExecuteDelete(data parse.DeleteData, tx *transaction.Transaction) (int, error) {
	return up.forEachKept(data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		return us.DeleteCurrentRecord()
	})
}

// ExecuteModify sets the target field of every record the predicate keeps to
// the new value, read against that record, so that a field in the new value
// stands for the record's own.
func (up *BasicUpdatePlanner) ExecuteModify(data parse.ModifyData, tx *transaction.Transaction) (int, error) {
	return up.forEachKept(data.TableName(), data.Predicate(), tx, func(us query.UpdateScan) error {
		val, err := data.NewValue().Evaluate(us)
		if err != nil {
			return err
		}

		return us.SetValue(data.TargetField(), val)
	})
}

// ExecuteCreateTable records the table's definition.
func (up *BasicUpdatePlanner) ExecuteCreateTable(data parse.CreateTableData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateTable(tx, data.TableName(), data.Schema())
}

// ExecuteCreateView records the view's definition.
//
// The definition is not checked against the catalogs. A view over a table
// that does not exist is refused only when a query reads it.
func (up *BasicUpdatePlanner) ExecuteCreateView(data parse.CreateViewData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateView(tx, data.ViewName(), data.ViewDef())
}

// ExecuteCreateIndex records the index's definition.
func (up *BasicUpdatePlanner) ExecuteCreateIndex(data parse.CreateIndexData, tx *transaction.Transaction) (int, error) {
	return 0, up.mm.CreateIndex(tx, data.IndexName(), data.TableName(), data.FieldName())
}

// forEachKept calls change on every record of the table that pred keeps, and
// reports how many there were.
//
// The predicate is tested here rather than through a select scan, which is a
// Scan and not an UpdateScan. Opening the table's own scan and testing each
// record is all a select would do, and this is the one place that needs it
// done on a scan that can be written through.
func (up *BasicUpdatePlanner) forEachKept(tableName string, pred query.Predicate, tx *transaction.Transaction, change func(us query.UpdateScan) error) (int, error) {
	us, err := up.openUpdateScan(tableName, tx)
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

// openUpdateScan opens a scan over the table that can be written through.
func (up *BasicUpdatePlanner) openUpdateScan(tableName string, tx *transaction.Transaction) (query.UpdateScan, error) {
	tp, err := NewTablePlan(tx, tableName, up.mm)
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
