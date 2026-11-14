package plan

import (
	"errors"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestNewMultibufferProductPlan(t *testing.T) {
	t.Run("given two plans that both have a field a, when a multibuffer product of them is made, then it reports ErrDuplicateField", func(t *testing.T) {
		tx := newTestTransaction(t)
		left := newTestLeftPlan(t)
		right := newTestRightPlan(t)
		right.schema = newIntSchema(t, "a", "c")

		_, err := NewMultibufferProductPlan(tx, left, right)
		if !errors.Is(err, recordmanager.ErrDuplicateField) {
			t.Errorf("NewMultibufferProductPlan() error = %v, want %v", err, recordmanager.ErrDuplicateField)
		}
	})
}

func TestMultibufferProductPlanOpen(t *testing.T) {
	t.Run("given a table of 3 records and one of 2, when their multibuffer product is opened, then the scan reads every record of the first beside every record of the second", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createOtherTable(t, tx, mm, 2)
		p := mustNewMultibufferProductPlan(t, tx, mustNewTablePlan(t, tx, mm), mustNewTablePlanOf(t, tx, mm, otherTableName))

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if s == nil {
			t.Fatal("Open() returned no scan")
		}
		defer s.Close()

		want := [][2]int32{
			{0, 0}, {0, 1},
			{1, 0}, {1, 1},
			{2, 0}, {2, 1},
		}
		if got := readIDPairs(t, s); !slices.Equal(got, want) {
			t.Errorf("the scan read (id, oid) pairs %v, want %v", got, want)
		}
	})

	t.Run("given a table of 3 records and one of 2, when their multibuffer product is opened and closed, then every buffer is available again", func(t *testing.T) {
		tx := newTestTransaction(t)
		mm := newTestMetadataManager(t, tx)
		createTestTable(t, tx, mm, 3)
		createOtherTable(t, tx, mm, 2)
		p := mustNewMultibufferProductPlan(t, tx, mustNewTablePlan(t, tx, mm), mustNewTablePlanOf(t, tx, mm, otherTableName))

		s, err := p.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if s == nil {
			t.Fatal("Open() returned no scan")
		}
		s.Close()

		if got := tx.AvailableBuffers(); got != testNumBuffers {
			t.Errorf("AvailableBuffers() = %d, want %d", got, testNumBuffers)
		}
	})
}

// The right side is read once, for the copy to a temp table. The materialized
// left side is read once for every chunk of that copy.
//
// Both sides have a schema of two ints. A slot is 12 bytes, so 66 records fit
// in a block. Nothing is pinned, so all 8 buffers of the test pool are free,
// and 6 remain without the reserved buffers. The left side of 100 records
// materializes to 2 blocks.
func TestMultibufferProductPlanBlocksAccessed(t *testing.T) {
	tests := []struct {
		name         string
		rightRecords int
		want         int
	}{
		{
			name:         "given 8 buffers free and a right plan of 3 blocks whose copy takes 1 block, when the blocks accessed are asked for, then they are 3 plus the 2 of the left for the one chunk",
			rightRecords: 66,
			want:         3 + 2*1,
		},
		{
			name:         "given 8 buffers free and a right plan of 3 blocks whose copy takes 10 blocks, read in 2 chunks of 5, when the blocks accessed are asked for, then they are 3 plus the 2 of the left for each chunk",
			rightRecords: 660,
			want:         3 + 2*2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			right := newTestRightPlan(t)
			right.recordsOutput = tt.rightRecords
			p := mustNewMultibufferProductPlan(t, tx, newTestLeftPlan(t), right)

			if got := p.BlocksAccessed(); got != tt.want {
				t.Errorf("BlocksAccessed() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMultibufferProductPlanRecordsOutput(t *testing.T) {
	t.Run("given a left plan of 100 records and a right plan of 20, when their multibuffer product is asked for the records output, then they are 100 times 20", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := mustNewMultibufferProductPlan(t, tx, newTestLeftPlan(t), newTestRightPlan(t))

		if got, want := p.RecordsOutput(), 100*20; got != want {
			t.Errorf("RecordsOutput() = %d, want %d", got, want)
		}
	})
}

func TestMultibufferProductPlanDistinctValues(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      int
	}{
		{
			name:      "given a left plan whose a holds 10 distinct values, when their multibuffer product is asked for those of a, then they are the 10 of the left",
			fieldName: "a",
			want:      10,
		},
		{
			name:      "given a right plan whose c holds 7 distinct values, when their multibuffer product is asked for those of c, then they are the 7 of the right",
			fieldName: "c",
			want:      7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newTestTransaction(t)
			p := mustNewMultibufferProductPlan(t, tx, newTestLeftPlan(t), newTestRightPlan(t))

			if got := p.DistinctValues(tt.fieldName); got != tt.want {
				t.Errorf("DistinctValues(%q) = %d, want %d", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestMultibufferProductPlanSchema(t *testing.T) {
	t.Run("given a left plan of a and b and a right plan of c and d, when their multibuffer product is asked for its schema, then it holds the fields of the left followed by those of the right", func(t *testing.T) {
		tx := newTestTransaction(t)
		p := mustNewMultibufferProductPlan(t, tx, newTestLeftPlan(t), newTestRightPlan(t))

		assertSchema(t, p.Schema(), newIntSchema(t, "a", "b", "c", "d"))
	})
}

func mustNewMultibufferProductPlan(t *testing.T, tx *transaction.Transaction, lhs, rhs Plan) *MultibufferProductPlan {
	t.Helper()

	p, err := NewMultibufferProductPlan(tx, lhs, rhs)
	if err != nil {
		t.Fatalf("NewMultibufferProductPlan() error = %v", err)
	}

	return p
}
