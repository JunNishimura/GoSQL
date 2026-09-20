package query

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// newTestProductSide opens a scan over a table of its own holding one record
// for each id, narrowed to the one named field.
//
// The two sides of a product in these tests keep different fields, so every
// field of the product belongs to exactly one side and a read that went to the
// wrong side comes back wrong rather than right by chance.
func newTestProductSide(t *testing.T, fieldName string, ids ...int32) *ProjectScan {
	t.Helper()

	ps, err := NewProjectScan(newTestTableOfRecords(t, ids...), fieldName)
	if err != nil {
		t.Fatalf("NewProjectScan(%q) error = %v", fieldName, err)
	}

	return ps
}

// newTestProduct opens the product of a left side holding the ids and a right
// side holding the names the ids were given.
func newTestProduct(t *testing.T, leftIDs, rightIDs []int32) *ProductScan {
	t.Helper()

	p, err := NewProductScan(
		newTestProductSide(t, "id", leftIDs...),
		newTestProductSide(t, "name", rightIDs...),
	)
	if err != nil {
		t.Fatalf("NewProductScan() error = %v", err)
	}

	return p
}

// walkTestProduct reads every record left in the product as its two fields put
// together, which shows both which pairs came out and what order they came out
// in.
func walkTestProduct(t *testing.T, s Scan) []string {
	t.Helper()

	pairs := []string{}
	for {
		onRecord, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !onRecord {
			return pairs
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		name, err := s.GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}

		pairs = append(pairs, fmt.Sprintf("%d %s", id, name))
	}
}

func TestProductScanMoveToNextRecord(t *testing.T) {
	tests := []struct {
		name     string
		leftIDs  []int32
		rightIDs []int32
		want     []string
	}{
		// The order is what says which side is the outer loop. Read with the
		// right side outside, the same four pairs would come out as 10, 20, 10,
		// 20 down the ids instead.
		{
			name:     "given two records on each side, when the product is walked, then it returns every pair, with the right side read through once for each record of the left",
			leftIDs:  []int32{10, 20},
			rightIDs: []int32{10, 20},
			want:     []string{"10 name10", "10 name20", "20 name10", "20 name20"},
		},
		{
			name:     "given one record on the left and three on the right, when the product is walked, then it returns three pairs",
			leftIDs:  []int32{10},
			rightIDs: []int32{10, 20, 30},
			want:     []string{"10 name10", "10 name20", "10 name30"},
		},
		{
			name:     "given three records on the left and one on the right, when the product is walked, then it returns three pairs",
			leftIDs:  []int32{10, 20, 30},
			rightIDs: []int32{10},
			want:     []string{"10 name10", "20 name10", "30 name10"},
		},
		// A product with nothing on one side is empty. The left being empty is
		// where a product that steps the right side first goes wrong: it would
		// hand out a record while the left side was on none.
		{
			name:     "given no records on the left and two on the right, when the product is walked, then it returns none",
			leftIDs:  []int32{},
			rightIDs: []int32{10, 20},
			want:     []string{},
		},
		{
			name:     "given two records on the left and none on the right, when the product is walked, then it returns none",
			leftIDs:  []int32{10, 20},
			rightIDs: []int32{},
			want:     []string{},
		},
		{
			name:     "given no records on either side, when the product is walked, then it returns none",
			leftIDs:  []int32{},
			rightIDs: []int32{},
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProduct(t, tt.leftIDs, tt.rightIDs)

			if got := walkTestProduct(t, p); !slices.Equal(got, tt.want) {
				t.Errorf("the pairs read = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("given a product that has been read to the end, when it is asked again, then it stays at the end", func(t *testing.T) {
		p := newTestProduct(t, []int32{10}, []int32{10})

		if got := walkTestProduct(t, p); !slices.Equal(got, []string{"10 name10"}) {
			t.Fatalf("the pairs read = %v, want [10 name10]", got)
		}

		onRecord, err := p.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if onRecord {
			t.Errorf("MoveToNextRecord() = true, want false once the walk has ended")
		}
	})
}

func TestProductScanMoveBeforeFirstRecord(t *testing.T) {
	// Going back has to put both sides back, and put the left one onto its
	// first record rather than before it. A product that reset only the right
	// side would come back with the pairs of the last record of the left.
	t.Run("given a product that has been read to the end, when it is put back to the start, then it returns the same pairs again", func(t *testing.T) {
		p := newTestProduct(t, []int32{10, 20}, []int32{10, 20})

		want := []string{"10 name10", "10 name20", "20 name10", "20 name20"}
		if got := walkTestProduct(t, p); !slices.Equal(got, want) {
			t.Fatalf("the pairs read = %v, want %v", got, want)
		}

		if err := p.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := walkTestProduct(t, p); !slices.Equal(got, want) {
			t.Errorf("the pairs read the second time = %v, want %v", got, want)
		}
	})
}

// newTestProductOnARecord opens a product of one record on each side and moves
// it onto the one pair it has.
func newTestProductOnARecord(t *testing.T) *ProductScan {
	t.Helper()

	p := newTestProduct(t, []int32{10}, []int32{20})

	onRecord, err := p.MoveToNextRecord()
	if err != nil {
		t.Fatalf("MoveToNextRecord() error = %v", err)
	}
	if !onRecord {
		t.Fatal("the product returned no records, want the one pair its sides make")
	}

	return p
}

// A product has the fields of both sides, which is what makes a join of two
// tables readable as one run of records.
func TestProductScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a field the left side has, when the product is asked for it, then it reports the field is there",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a field the right side has, when the product is asked for it, then it reports the field is there",
			fieldName: "name",
			want:      true,
		},
		{
			name:      "given a field neither side has, when the product is asked for it, then it reports the field is not there",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newTestProduct(t, []int32{10}, []int32{20})

			if got := p.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestProductScanGetInt(t *testing.T) {
	t.Run("given a field the left side has, when it is read, then it is what the left side's record holds", func(t *testing.T) {
		got, err := newTestProductOnARecord(t).GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 10 {
			t.Errorf("GetInt(%q) = %d, want 10", "id", got)
		}
	})

	t.Run("given a field neither side has, when it is read, then it reports ErrFieldNotFound", func(t *testing.T) {
		p := newTestProductOnARecord(t)

		if _, err := p.GetInt("missing"); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("GetInt(%q) error = %v, want %v", "missing", err, recordmanager.ErrFieldNotFound)
		}
	})
}

func TestProductScanGetString(t *testing.T) {
	t.Run("given a field the right side has, when it is read, then it is what the right side's record holds", func(t *testing.T) {
		got, err := newTestProductOnARecord(t).GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}
		if want := testNameOf(20); got != want {
			t.Errorf("GetString(%q) = %q, want %q", "name", got, want)
		}
	})

	t.Run("given a field neither side has, when it is read, then it reports ErrFieldNotFound", func(t *testing.T) {
		p := newTestProductOnARecord(t)

		if _, err := p.GetString("missing"); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("GetString(%q) error = %v, want %v", "missing", err, recordmanager.ErrFieldNotFound)
		}
	})
}

func TestProductScanGetValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      Constant
		wantErr   error
	}{
		{
			name:      "given a field the left side has, when it is read as a value, then it is the left side's field as an int constant",
			fieldName: "id",
			want:      NewIntConstant(10),
		},
		{
			name:      "given a field the right side has, when it is read as a value, then it is the right side's field as a varchar constant",
			fieldName: "name",
			want:      NewStringConstant(testNameOf(20)),
		},
		{
			name:      "given a field neither side has, when it is read as a value, then it reports ErrFieldNotFound",
			fieldName: "missing",
			wantErr:   recordmanager.ErrFieldNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newTestProductOnARecord(t).GetValue(tt.fieldName)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetValue(%q) error = %v, want %v", tt.fieldName, err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("GetValue(%q) error = %v", tt.fieldName, err)
			}
			if got != tt.want {
				t.Errorf("GetValue(%q) = %s, want %s", tt.fieldName, got, tt.want)
			}
		})
	}
}

// A name that both sides have is ambiguous in the query that asked for it, and
// nothing here can qualify it with the table it came from. The left side
// answering is a rule rather than a right answer, so it is written down here to
// keep it from becoming whichever side the code happens to try first.
func TestProductScanReadsAFieldBothSidesHaveFromTheLeft(t *testing.T) {
	t.Run("given a field both sides have, when it is read, then it is the left side's", func(t *testing.T) {
		p, err := NewProductScan(
			newTestProductSide(t, "id", 10),
			newTestProductSide(t, "id", 20),
		)
		if err != nil {
			t.Fatalf("NewProductScan() error = %v", err)
		}

		onRecord, err := p.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !onRecord {
			t.Fatal("the product returned no records, want the one pair its sides make")
		}

		got, err := p.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 10 {
			t.Errorf("GetInt(%q) = %d, want 10: the field was read from the right side", "id", got)
		}
	})
}

func TestProductScanClose(t *testing.T) {
	// A product holds two scans, so a Close that reached only one of them would
	// leave the other's buffer taken for the rest of the transaction.
	t.Run("when a product is closed, then both of the scans it reads through are closed, so the blocks they held are given back", func(t *testing.T) {
		leftTable := newTestTableOfRecords(t, 10)
		rightTable := newTestTableOfRecords(t, 20)

		leftHeld := leftTable.rp.BlockID()
		rightHeld := rightTable.rp.BlockID()

		left, err := NewProjectScan(leftTable, "id")
		if err != nil {
			t.Fatalf("NewProjectScan() for the left side error = %v", err)
		}
		right, err := NewProjectScan(rightTable, "name")
		if err != nil {
			t.Fatalf("NewProjectScan() for the right side error = %v", err)
		}

		p, err := NewProductScan(left, right)
		if err != nil {
			t.Fatalf("NewProductScan() error = %v", err)
		}
		p.Close()

		if _, err := leftTable.tx.GetInt(leftHeld, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the left side held error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
		if _, err := rightTable.tx.GetInt(rightHeld, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the right side held error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
	})
}
