package plan

import (
	"errors"
	"slices"
	"testing"

	metadatamanager "github.com/JunNishimura/GoSQL/metadata_manager"
	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// testRecordCount is how many records the tests below write into the test
// table. A slot of it is 52 bytes, the in-use flag (4) plus id (4) plus name
// (4 + 10 * 4), so a block of 800 holds 15 and the table takes up 2 blocks.
// That puts the three numbers a plan reports at 2, 20 and 7, no two of them
// alike, so that a plan answering one question with another's number shows.
const testRecordCount = 20

func TestNewTablePlan(t *testing.T) {
	t.Run("given a table the catalogs do not name, when a plan is made for it, then it reports ErrTableNotFound", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)

		_, err := NewTablePlan(tx, "missing", mm)
		if !errors.Is(err, metadatamanager.ErrTableNotFound) {
			t.Errorf("NewTablePlan() error = %v, want %v", err, metadatamanager.ErrTableNotFound)
		}
	})
}

func TestTablePlanOpen(t *testing.T) {
	t.Run("given a table holding records, when the plan is opened, then the scan reads every record of the table", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer s.Close()

		want := []int32{}
		for i := range testRecordCount {
			want = append(want, int32(i))
		}
		if got := readIDs(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read ids %v, want %v", got, want)
		}
	})

	// An update planner writes through the scan a table plan opens, so the scan
	// has to be one that can be written through, not only read.
	t.Run("given a table, when the plan is opened, then the scan can be written through", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer s.Close()

		if _, ok := s.(query.UpdateScan); !ok {
			t.Errorf("Open() = %T, want a query.UpdateScan", s)
		}
	})
}

func TestTablePlanBlocksAccessed(t *testing.T) {
	t.Run("given a table of 20 records over 2 blocks, when the blocks accessed are asked for, then they are the blocks the statistics count", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)
		stats := mustGetStatistics(t, tx, mm)

		if got, want := p.BlocksAccessed(), stats.BlocksAccessed(); got != want {
			t.Errorf("BlocksAccessed() = %d, want %d", got, want)
		}
	})
}

func TestTablePlanRecordsOutput(t *testing.T) {
	t.Run("given a table of 20 records over 2 blocks, when the records output are asked for, then they are the records the statistics count", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)
		stats := mustGetStatistics(t, tx, mm)

		if got, want := p.RecordsOutput(), stats.RecordsOutput(); got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestTablePlanDistinctValues(t *testing.T) {
	t.Run("given a table of 20 records over 2 blocks, when the distinct values of a field are asked for, then they are what the statistics guess", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)
		stats := mustGetStatistics(t, tx, mm)

		if got, want := p.DistinctValues("id"), stats.DistinctValues("id"); got != want {
			t.Errorf("DistinctValues(%q) = %d, want %d", "id", got, want)
		}
	})
}

func TestTablePlanSchema(t *testing.T) {
	t.Run("given a table, when the schema is asked for, then it holds the fields the table was created with", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, testRecordCount)

		p := mustNewTablePlan(t, tx, mm)

		assertSchema(t, p.Schema(), newTestSchema(t))
	})
}

func mustNewTablePlan(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager) *TablePlan {
	t.Helper()

	p, err := NewTablePlan(tx, testTableName, mm)
	if err != nil {
		t.Fatalf("NewTablePlan(%q) error = %v", testTableName, err)
	}

	return p
}

func mustGetStatistics(t *testing.T, tx *transaction.Transaction, mm *metadatamanager.MetadataManager) *metadatamanager.TableStatistics {
	t.Helper()

	stats, err := mm.GetStatistics(tx, testTableName)
	if err != nil {
		t.Fatalf("GetStatistics(%q) error = %v", testTableName, err)
	}

	return stats
}

// assertSchema compares two schemas field by field: the names in order, then
// the type and length of each.
func assertSchema(t *testing.T, got *recordmanager.Schema, want *recordmanager.Schema) {
	t.Helper()

	if !slices.Equal(got.Fields(), want.Fields()) {
		t.Fatalf("the schema holds fields %v, want %v", got.Fields(), want.Fields())
	}

	for _, fieldName := range want.Fields() {
		gotType, err := got.Type(fieldName)
		if err != nil {
			t.Fatalf("Type(%q) error = %v", fieldName, err)
		}
		wantType, err := want.Type(fieldName)
		if err != nil {
			t.Fatalf("Type(%q) error = %v", fieldName, err)
		}
		if gotType != wantType {
			t.Errorf("field %q is of type %v, want %v", fieldName, gotType, wantType)
		}

		gotLength, err := got.Length(fieldName)
		if err != nil {
			t.Fatalf("Length(%q) error = %v", fieldName, err)
		}
		wantLength, err := want.Length(fieldName)
		if err != nil {
			t.Fatalf("Length(%q) error = %v", fieldName, err)
		}
		if gotLength != wantLength {
			t.Errorf("field %q is %d long, want %d", fieldName, gotLength, wantLength)
		}
	}
}
