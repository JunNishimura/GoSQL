package query

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// testNameOf is the name the record with this id is given, so that a case can
// name a record by either of its two fields and mean the same record.
func testNameOf(id int32) string {
	return fmt.Sprintf("name%d", id)
}

// newTestTableOfRecords opens a scan over a table holding one record for each
// of the given ids, and leaves it before the first record.
//
// The records go in through the scan rather than into slots chosen by the test,
// because what a select is read for is the run of records it keeps, not where
// they sit.
func newTestTableOfRecords(t *testing.T, ids ...int32) *TableScan {
	t.Helper()

	ts := newTestTableScanAt(t, beforeFirstSlot)

	for _, id := range ids {
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}
		if err := ts.SetInt("id", id); err != nil {
			t.Fatalf("SetInt(%d) error = %v", id, err)
		}
		if err := ts.SetString("name", testNameOf(id)); err != nil {
			t.Fatalf("SetString(%q) error = %v", testNameOf(id), err)
		}
	}

	if err := ts.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	return ts
}

// termOnID is the term keeping the record whose id is this one.
func termOnID(id int32) Term {
	return NewTerm(NewFieldExpression("id"), NewConstantExpression(NewIntConstant(id)))
}

// termOnName is the term keeping the record whose name is the one the id was
// given, which is the same record termOnID keeps and a different way of asking
// for it.
func termOnName(id int32) Term {
	return NewTerm(NewFieldExpression("name"), NewConstantExpression(NewStringConstant(testNameOf(id))))
}

func TestSelectScanMoveToNextRecord(t *testing.T) {
	tests := []struct {
		name string
		pred Predicate
		want []int32
	}{
		{
			name: "given a predicate no record meets, when the select is walked, then it returns none of them",
			pred: NewPredicate(termOnID(99)),
			want: []int32{},
		},
		{
			name: "given a predicate one record in the middle meets, when the select is walked, then it returns that one and passes over the rest",
			pred: NewPredicate(termOnID(20)),
			want: []int32{20},
		},
		{
			name: "given a predicate only the last record meets, when the select is walked, then it reads to the end to find it",
			pred: NewPredicate(termOnID(30)),
			want: []int32{30},
		},
		{
			name: "given a predicate of no terms, when the select is walked, then it returns every record of the table in order",
			pred: NewPredicate(),
			want: []int32{10, 20, 30},
		},
		// The two terms name the same record by its two different fields, so a
		// select that tested only one of them would keep the same record and
		// pass for the wrong reason. The case below, whose terms name two
		// different records, is what tells them apart.
		{
			name: "given a predicate of two terms that one record meets, when the select is walked, then it returns that record",
			pred: NewPredicate(termOnID(20), termOnName(20)),
			want: []int32{20},
		},
		{
			name: "given a predicate of two terms that name different records, when the select is walked, then it returns none of them",
			pred: NewPredicate(termOnID(20), termOnName(30)),
			want: []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewSelectScan(newTestTableOfRecords(t, 10, 20, 30), tt.pred)

			if got := walkTestRecords(t, sc); !slices.Equal(got, tt.want) {
				t.Errorf("the ids read = %v, want %v", got, tt.want)
			}
		})
	}

	// A walk that has ended stays ended, rather than starting over on the
	// table underneath.
	t.Run("given a select that has been read to the end, when it is asked again, then it stays at the end", func(t *testing.T) {
		sc := NewSelectScan(newTestTableOfRecords(t, 10, 20, 30), NewPredicate(termOnID(20)))

		if got := walkTestRecords(t, sc); !slices.Equal(got, []int32{20}) {
			t.Fatalf("the ids read = %v, want [20]", got)
		}

		onRecord, err := sc.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if onRecord {
			t.Errorf("MoveToNextRecord() = true, want false once the walk has ended")
		}
	})

	// The select does no testing of its own, so what the predicate refuses has
	// to reach the caller rather than read as a record that does not match.
	t.Run("given a predicate naming a field the table does not have, when the select is walked, then ErrFieldNotFound reaches the caller", func(t *testing.T) {
		missing := NewTerm(NewFieldExpression("missing"), NewConstantExpression(NewIntConstant(10)))
		sc := NewSelectScan(newTestTableOfRecords(t, 10), NewPredicate(missing))

		if _, err := sc.MoveToNextRecord(); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("MoveToNextRecord() error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}
	})
}

func TestSelectScanMoveBeforeFirstRecord(t *testing.T) {
	// Going back has to reach the table underneath. A select that reset only
	// itself would find the table already read to the end and return nothing.
	t.Run("given a select that has been read to the end, when it is put back to the start, then it returns the same records again", func(t *testing.T) {
		sc := NewSelectScan(newTestTableOfRecords(t, 10, 20, 30), NewPredicate(termOnID(20)))

		want := []int32{20}
		if got := walkTestRecords(t, sc); !slices.Equal(got, want) {
			t.Fatalf("the ids read = %v, want %v", got, want)
		}

		if err := sc.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := walkTestRecords(t, sc); !slices.Equal(got, want) {
			t.Errorf("the ids read the second time = %v, want %v", got, want)
		}
	})
}

// newTestSelectOnARecord opens a select over a table of one record, id 20, and
// moves it onto that record, which is where the field methods are read from.
func newTestSelectOnARecord(t *testing.T) *SelectScan {
	t.Helper()

	sc := NewSelectScan(newTestTableOfRecords(t, 10, 20, 30), NewPredicate(termOnID(20)))

	onRecord, err := sc.MoveToNextRecord()
	if err != nil {
		t.Fatalf("MoveToNextRecord() error = %v", err)
	}
	if !onRecord {
		t.Fatal("the select kept no records, want the one its predicate names")
	}

	return sc
}

func TestSelectScanGetInt(t *testing.T) {
	t.Run("given a select on a record, when an int field is read, then it is what the record underneath holds", func(t *testing.T) {
		got, err := newTestSelectOnARecord(t).GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 20 {
			t.Errorf("GetInt(%q) = %d, want 20", "id", got)
		}
	})
}

func TestSelectScanGetString(t *testing.T) {
	t.Run("given a select on a record, when a varchar field is read, then it is what the record underneath holds", func(t *testing.T) {
		got, err := newTestSelectOnARecord(t).GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}
		if want := testNameOf(20); got != want {
			t.Errorf("GetString(%q) = %q, want %q", "name", got, want)
		}
	})
}

func TestSelectScanGetValue(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      Constant
	}{
		{
			name:      "given a select on a record, when its int field is read as a value, then it is that field as an int constant",
			fieldName: "id",
			want:      NewIntConstant(20),
		},
		{
			name:      "given a select on a record, when its varchar field is read as a value, then it is that field as a varchar constant",
			fieldName: "name",
			want:      NewStringConstant(testNameOf(20)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newTestSelectOnARecord(t).GetValue(tt.fieldName)
			if err != nil {
				t.Fatalf("GetValue() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("GetValue(%q) = %s, want %s", tt.fieldName, got, tt.want)
			}
		})
	}
}

// A select keeps the fields of what it reads: it drops records, and a
// projection is what drops fields.
func TestSelectScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a field the table underneath has, when the select is asked for it, then it reports the field is there",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a field the table underneath does not have, when the select is asked for it, then it reports the field is not there",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := NewSelectScan(newTestTableOfRecords(t, 10), NewPredicate())

			if got := sc.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestSelectScanClose(t *testing.T) {
	// A select holds no blocks of its own, so closing it is only worth anything
	// if it reaches the scan underneath.
	t.Run("when a select is closed, then the scan it reads through is closed too, so the block that one held is given back", func(t *testing.T) {
		ts := newTestTableOfRecords(t, 10)

		held := ts.rp.BlockID()

		NewSelectScan(ts, NewPredicate()).Close()

		if _, err := ts.tx.GetInt(held, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the table scan held error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
	})
}
