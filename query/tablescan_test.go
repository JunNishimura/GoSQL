package query

import (
	"errors"
	"slices"
	"strings"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

// testTableName is the table that the scan tests use. Its file is
// testDataFile. A test that needs to bypass the scan appends to that file
// directly.
const testTableName = "test"

func TestNewTableScan(t *testing.T) {
	t.Run("given a table whose file has no blocks, it appends one and opens before the first record of it", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)

		ts, err := NewTableScan(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("NewTableScan() error = %v", err)
		}

		if ts.tx != tx {
			t.Errorf("tx = %p, want %p", ts.tx, tx)
		}
		if ts.layout != layout {
			t.Errorf("layout = %p, want %p", ts.layout, layout)
		}
		if ts.fileName != testDataFile {
			t.Errorf("fileName = %q, want %q", ts.fileName, testDataFile)
		}
		if ts.currentSlot != beforeFirstSlot {
			t.Errorf("currentSlot = %d, want %d", ts.currentSlot, beforeFirstSlot)
		}

		size, err := tx.Size(testDataFile)
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if size != 1 {
			t.Errorf("the table has %d blocks, want 1: the scan did not append one", size)
		}

		if ts.rp == nil {
			t.Fatal("the scan is on no block, want block 0")
		}
		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Errorf("the scan is on block %d, want 0", got)
		}
	})

	// An appended block is zeroed, so all its slots are already free. The
	// two branches therefore differ only in the other direction: an existing
	// block of the table must keep its records.
	t.Run("given a table that already has a block, it opens on that one and keeps the records in it", func(t *testing.T) {
		const id = 42

		tx := newTestTransaction(t)
		layout := newTestLayout(t)

		blk, err := tx.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}
		rp, err := recordmanager.NewRecordPage(tx, blk, layout)
		if err != nil {
			t.Fatalf("NewRecordPage() error = %v", err)
		}
		slot, err := rp.ClaimFreeSlotAfter(beforeFirstSlot)
		if err != nil {
			t.Fatalf("ClaimFreeSlotAfter() error = %v", err)
		}
		if err := rp.SetInt(slot, "id", id); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		ts, err := NewTableScan(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("NewTableScan() error = %v", err)
		}

		size, err := tx.Size(testDataFile)
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if size != 1 {
			t.Errorf("the table has %d blocks, want 1: the scan appended one it did not need", size)
		}

		if ts.rp == nil {
			t.Fatal("the scan is on no block, want block 0")
		}
		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Fatalf("the scan is on block %d, want 0", got)
		}

		if got, err := ts.rp.NextUsedSlotAfter(beforeFirstSlot); err != nil || got != slot {
			t.Fatalf("NextUsedSlotAfter() = %d, %v, want %d, nil: the record was wiped", got, err, slot)
		}
		got, err := ts.rp.GetInt(slot, "id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != id {
			t.Errorf("GetInt() = %d, want %d: the record was wiped", got, id)
		}
	})
}

// A layout that is too large for a block must be rejected when the scan is
// opened, because every later call assumes that the block of the scan can
// hold records. MoveToNewRecord in particular would handle "no free slot" by
// appending another block, and would never stop.
func TestNewTableScanRejectsASlotWiderThanABlock(t *testing.T) {
	t.Run("given a layout whose slot is wider than a block, when a scan is opened on the table, then recordmanager.ErrSlotWiderThanBlock reaches the caller", func(t *testing.T) {
		tx := newTestTransaction(t)

		layout := newLayoutOfOneStringField(t, overwideFieldLength)

		if _, err := NewTableScan(tx, testTableName, layout); !errors.Is(err, recordmanager.ErrSlotWiderThanBlock) {
			t.Errorf("error = %v, want %v", err, recordmanager.ErrSlotWiderThanBlock)
		}
	})
}

func TestTableScanMoveToBlock(t *testing.T) {
	t.Run("when the scan moves to another block, then the pin on the one it leaves is given back", func(t *testing.T) {
		tx := newTestTransaction(t)

		ts, err := NewTableScan(tx, testTableName, newTestLayout(t))
		if err != nil {
			t.Fatalf("NewTableScan() error = %v", err)
		}

		if ts.rp == nil {
			t.Fatal("the scan is on no block after it was opened")
		}
		left := ts.rp.BlockID()

		if _, err := tx.Append(testDataFile); err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		if err := ts.moveToBlock(1); err != nil {
			t.Fatalf("moveToBlock(1) error = %v", err)
		}

		if got := ts.rp.BlockID().Number(); got != 1 {
			t.Errorf("the scan is on block %d, want 1", got)
		}
		if _, err := tx.GetInt(left, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the scan left error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
	})
}

// newTestTableScanAt opens a scan of a table with one empty block and
// positions it at slot, as MoveToNextRecord would.
func newTestTableScanAt(t *testing.T, slot int) *TableScan {
	t.Helper()

	ts, err := NewTableScan(newTestTransaction(t), testTableName, newTestLayout(t))
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}
	ts.currentSlot = slot

	return ts
}

func TestTableScanSetIntAndGetInt(t *testing.T) {
	tests := []struct {
		name string
		slot int
		val  int32
	}{
		{
			name: "when an int is written to the record the scan is on and read back, then it is unchanged",
			slot: 0,
			val:  42,
		},
		{
			name: "given the scan is on the last slot of the block, an int written there is read back unchanged",
			slot: testSlotsInBlock - 1,
			val:  7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, tt.slot)

			if err := ts.SetInt("id", tt.val); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			got, err := ts.GetInt("id")
			if err != nil {
				t.Fatalf("GetInt() error = %v", err)
			}
			if got != tt.val {
				t.Errorf("GetInt() = %d, want %d", got, tt.val)
			}
		})
	}

	// The four field methods take no slot, so they must read and write at the
	// position of the scan. Writing at two slots and returning to the first
	// distinguishes this from always using the same slot.
	t.Run("given two records written through the scan, when it is put back on the first, then it reads that one rather than the last written", func(t *testing.T) {
		ts := newTestTableScanAt(t, 0)

		if err := ts.SetInt("id", 10); err != nil {
			t.Fatalf("SetInt() at slot 0 error = %v", err)
		}

		ts.currentSlot = 1
		if err := ts.SetInt("id", 20); err != nil {
			t.Fatalf("SetInt() at slot 1 error = %v", err)
		}

		ts.currentSlot = 0
		got, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() at slot 0 error = %v", err)
		}
		if got != 10 {
			t.Errorf("GetInt() at slot 0 = %d, want 10: the write at slot 1 landed here", got)
		}
	})
}

func TestTableScanSetStringAndGetString(t *testing.T) {
	tests := []struct {
		name string
		slot int
		val  string
	}{
		{
			name: "when a string is written to the record the scan is on and read back, then it is unchanged",
			slot: 0,
			val:  "alice",
		},
		{
			name: "when an empty string is written and read back, then it is still empty",
			slot: 0,
			val:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, tt.slot)

			if err := ts.SetString("name", tt.val); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}

			got, err := ts.GetString("name")
			if err != nil {
				t.Fatalf("GetString() error = %v", err)
			}
			if got != tt.val {
				t.Errorf("GetString() = %q, want %q", got, tt.val)
			}
		})
	}
}

func TestTableScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a table whose schema has an id field, when the scan is asked for id, then it reports the table has it",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a table whose schema has no such field, when the scan is asked for that name, then it reports the table does not have it",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, 0)

			if got := ts.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}

	// A field belongs to the table, not to a record. HasField therefore does
	// not read the current record and must not require one. A query decides
	// which scan a field comes from before it reads any record, and it calls
	// HasField for that.
	t.Run("given a scan that is on no record, when it is asked for a field the table has, then it still reports the table has it", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if got := ts.HasField("id"); !got {
			t.Errorf("HasField(%q) = false, want true", "id")
		}
	})
}

// The schema determines the type of the returned constant. The scan has no
// other source, because the bytes in the slot can be read as either type.
//
// The record is written with the typed setters and not with SetValue. A bug
// in SetValue then fails its own test, and these cases cannot pass because
// two mistakes cancel out.
func TestTableScanGetValue(t *testing.T) {
	tests := []struct {
		name  string
		write func(ts *TableScan) error
		field string
		want  Constant
	}{
		{
			name:  "given a record whose int field holds a number, when the field is asked for as a value, then it is that number as an int constant",
			write: func(ts *TableScan) error { return ts.SetInt("id", 42) },
			field: "id",
			want:  NewIntConstant(42),
		},
		{
			name:  "given a record whose varchar field holds text, when the field is asked for as a value, then it is that text as a varchar constant",
			write: func(ts *TableScan) error { return ts.SetString("name", "alice") },
			field: "name",
			want:  NewStringConstant("alice"),
		},
		// The text is the digits of a number. A scan that derived the type
		// from the bytes and not from the schema would return an int here.
		{
			name:  "given a record whose varchar field holds digits, when the field is asked for as a value, then it is a varchar constant rather than an int one",
			write: func(ts *TableScan) error { return ts.SetString("name", "42") },
			field: "name",
			want:  NewStringConstant("42"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, 0)

			if err := tt.write(ts); err != nil {
				t.Fatalf("writing the record error = %v", err)
			}

			got, err := ts.GetValue(tt.field)
			if err != nil {
				t.Fatalf("GetValue(%q) error = %v", tt.field, err)
			}
			if got != tt.want {
				t.Errorf("GetValue(%q) = %s, want %s", tt.field, got, tt.want)
			}
		})
	}
}

func TestTableScanSetValue(t *testing.T) {
	tests := []struct {
		name  string
		field string
		val   Constant
	}{
		{
			name:  "given an int field, when an int constant is written to it and read back as a value, then it is unchanged",
			field: "id",
			val:   NewIntConstant(42),
		},
		{
			name:  "given a varchar field, when a varchar constant is written to it and read back as a value, then it is unchanged",
			field: "name",
			val:   NewStringConstant("alice"),
		},
		{
			name:  "given a varchar field, when a varchar constant of the empty string is written to it and read back as a value, then it is still the empty string",
			field: "name",
			val:   NewStringConstant(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, 0)

			if err := ts.SetValue(tt.field, tt.val); err != nil {
				t.Fatalf("SetValue(%q, %s) error = %v", tt.field, tt.val, err)
			}

			got, err := ts.GetValue(tt.field)
			if err != nil {
				t.Fatalf("GetValue(%q) error = %v", tt.field, err)
			}
			if got != tt.val {
				t.Errorf("GetValue(%q) = %s, want %s", tt.field, got, tt.val)
			}
		})
	}

	// SetValue must write the same bytes as the typed setter. Otherwise a
	// record would read differently depending on which of the two a caller
	// used.
	t.Run("given an int constant written as a value, when the field is read with the typed getter, then it is the number the constant held", func(t *testing.T) {
		ts := newTestTableScanAt(t, 0)

		if err := ts.SetValue("id", NewIntConstant(42)); err != nil {
			t.Fatalf("SetValue() error = %v", err)
		}

		got, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 42 {
			t.Errorf("GetInt(%q) = %d, want 42", "id", got)
		}
	})
}

func TestTableScanRejectsFieldsBeforeTheFirstRecord(t *testing.T) {
	tests := []struct {
		name string
		call func(ts *TableScan) error
	}{
		{
			name: "given a scan that is on no record, when GetInt is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				_, err := ts.GetInt("id")
				return err
			},
		},
		{
			name: "given a scan that is on no record, when SetInt is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				return ts.SetInt("id", 1)
			},
		},
		{
			name: "given a scan that is on no record, when GetString is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				_, err := ts.GetString("name")
				return err
			},
		},
		{
			name: "given a scan that is on no record, when SetString is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				return ts.SetString("name", "x")
			},
		},
		{
			name: "given a scan that is on no record, when GetValue is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				_, err := ts.GetValue("id")
				return err
			},
		},
		{
			name: "given a scan that is on no record, when SetValue is called, then it reports ErrNoCurrentRecord",
			call: func(ts *TableScan) error {
				return ts.SetValue("id", NewIntConstant(1))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, beforeFirstSlot)

			if err := tt.call(ts); !errors.Is(err, ErrNoCurrentRecord) {
				t.Errorf("error = %v, want %v", err, ErrNoCurrentRecord)
			}
		})
	}
}

// The scan adds no check of its own on the field, so an error from the record
// page must reach the caller unchanged.
func TestTableScanPassesOnTheRecordPagesFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		call    func(ts *TableScan) error
		wantErr error
	}{
		{
			name: "given a varchar field, when GetInt is called on it, then the record page ErrFieldTypeMismatch reaches the caller",
			call: func(ts *TableScan) error {
				_, err := ts.GetInt("name")
				return err
			},
			wantErr: recordmanager.ErrFieldTypeMismatch,
		},
		{
			name: "given an int field, when SetString is called on it, then the record page ErrFieldTypeMismatch reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetString("id", "x")
			},
			wantErr: recordmanager.ErrFieldTypeMismatch,
		},
		{
			name: "given a field the schema does not have, when GetString asks for it, then ErrFieldNotFound reaches the caller",
			call: func(ts *TableScan) error {
				_, err := ts.GetString("missing")
				return err
			},
			wantErr: recordmanager.ErrFieldNotFound,
		},
		{
			name: "given a field the schema does not have, when SetInt writes to it, then ErrFieldNotFound reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetInt("missing", 1)
			},
			wantErr: recordmanager.ErrFieldNotFound,
		},
		{
			name: "given a varchar field, when a string over its limit is written, then ErrStringTooLong reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetString("name", strings.Repeat("a", testStringFieldLength+1))
			},
			wantErr: recordmanager.ErrStringTooLong,
		},
		// A constant has its own type, so a write involves two types: the
		// type of the constant and the type in the schema. If they differ,
		// the write is rejected, whichever of the two is the int.
		{
			name: "given an int field, when a varchar constant is written to it, then ErrFieldTypeMismatch reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetValue("id", NewStringConstant("x"))
			},
			wantErr: recordmanager.ErrFieldTypeMismatch,
		},
		{
			name: "given a varchar field, when an int constant is written to it, then ErrFieldTypeMismatch reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetValue("name", NewIntConstant(1))
			},
			wantErr: recordmanager.ErrFieldTypeMismatch,
		},
		{
			name: "given a field the schema does not have, when its value is asked for, then ErrFieldNotFound reaches the caller",
			call: func(ts *TableScan) error {
				_, err := ts.GetValue("missing")
				return err
			},
			wantErr: recordmanager.ErrFieldNotFound,
		},
		{
			name: "given a field the schema does not have, when a value is written to it, then ErrFieldNotFound reaches the caller",
			call: func(ts *TableScan) error {
				return ts.SetValue("missing", NewIntConstant(1))
			},
			wantErr: recordmanager.ErrFieldNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestTableScanAt(t, 0)

			if err := tt.call(ts); !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestTableScanMoveBeforeFirstRecord(t *testing.T) {
	// The scan is moved to a second block and to a slot inside it, so that a
	// restart must reset both a block and a slot.
	t.Run("given a scan part way into a later block, it returns to the first block without appending one or wiping it", func(t *testing.T) {
		const id = 42

		ts := newTestTableScanAt(t, 0)

		if err := ts.SetInt("id", id); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}

		if err := ts.moveToNewBlock(); err != nil {
			t.Fatalf("moveToNewBlock() error = %v", err)
		}
		ts.currentSlot = 2

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Errorf("the scan is on block %d, want 0", got)
		}
		if ts.currentSlot != beforeFirstSlot {
			t.Errorf("currentSlot = %d, want %d", ts.currentSlot, beforeFirstSlot)
		}

		size, err := ts.tx.Size(ts.fileName)
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if size != 2 {
			t.Errorf("the table has %d blocks, want 2: going back to the start appended one", size)
		}

		ts.currentSlot = 0
		got, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != id {
			t.Errorf("GetInt() = %d, want %d: going back to the start wiped the block", got, id)
		}
	})

	// A restart must reset the slot even if the scan is still on the first block.
	// An implementation that returns early in that case would not reset it.
	t.Run("given a scan part way into the first block, it still resets the slot, so a field read reports ErrNoCurrentRecord", func(t *testing.T) {
		ts := newTestTableScanAt(t, 2)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Errorf("the scan is on block %d, want 0", got)
		}
		if ts.currentSlot != beforeFirstSlot {
			t.Errorf("currentSlot = %d, want %d", ts.currentSlot, beforeFirstSlot)
		}

		if _, err := ts.GetInt("id"); !errors.Is(err, ErrNoCurrentRecord) {
			t.Errorf("GetInt() error = %v, want %v: the scan is on a record it should not be on", err, ErrNoCurrentRecord)
		}
	})
}

// claimTestRecord marks slot of the current block as in use and writes id to
// it. It bypasses the scan, so that a test can place records in slots of its
// choice and not in the slots that MoveToNewRecord would choose.
//
// The claim starts from the slot before the wanted one, because a record page
// searches after the slot it is given. If the claim returns another slot, the
// wanted slot was already in use. That means the test set up a different
// state than it intended, so the helper reports it immediately.
func claimTestRecord(t *testing.T, ts *TableScan, slot int, id int32) {
	t.Helper()

	claimed, err := ts.rp.ClaimFreeSlotAfter(slot - 1)
	if err != nil {
		t.Fatalf("ClaimFreeSlotAfter(%d) error = %v", slot-1, err)
	}
	if claimed != slot {
		t.Fatalf("ClaimFreeSlotAfter(%d) = %d, want %d: slot %d was already taken", slot-1, claimed, slot, slot)
	}

	if err := ts.rp.SetInt(slot, "id", id); err != nil {
		t.Fatalf("SetInt(%d) error = %v", slot, err)
	}
}

// walkTestRecords reads the id of every record from the current position of
// the scan to its end.
//
// It takes a Scan and not a TableScan, so that a scan built on another scan
// can be read in the same way. A select is compared with its underlying table
// this way.
func walkTestRecords(t *testing.T, s Scan) []int32 {
	t.Helper()

	ids := []int32{}
	for {
		onRecord, err := s.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !onRecord {
			return ids
		}

		id, err := s.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		ids = append(ids, id)
	}
}

func TestTableScanMoveToNextRecord(t *testing.T) {
	// Slot 1 is free between the two records. A scan that returned every
	// slot instead of every record would return three ids.
	t.Run("given records with a free slot between them, the walk returns the records and passes over the gap", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, 0, 10)
		claimTestRecord(t, ts, 2, 30)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []int32{10, 30}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	// The first record is in the last slot of block 0 and the second is in block
	// 1, so the scan must continue past the end of a block to find both.
	t.Run("given records either side of a block boundary, the walk crosses into the next block to find them all", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, testSlotsInBlock-1, 10)

		if err := ts.moveToNewBlock(); err != nil {
			t.Fatalf("moveToNewBlock() error = %v", err)
		}
		claimTestRecord(t, ts, 0, 20)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []int32{10, 20}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	// Block 0 holds no records. The scan must move on from a block without
	// records and must not treat it as the end of the table.
	t.Run("given a block with no records before one that has them, the walk carries on rather than taking the empty block for the end", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if err := ts.moveToNewBlock(); err != nil {
			t.Fatalf("moveToNewBlock() error = %v", err)
		}
		claimTestRecord(t, ts, 1, 99)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []int32{99}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	t.Run("given a table that holds no records, it reports there is none rather than failing", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		onRecord, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if onRecord {
			t.Errorf("MoveToNextRecord() = true, want false on a table that holds no records")
		}
	})

	// A scan that has reached its end stays at the end. Another move must
	// neither restart the scan nor read past the end of the file.
	t.Run("given a walk that has reached the end, when it is asked again, then it stays at the end rather than starting over", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, 0, 10)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		if got := walkTestRecords(t, ts); !slices.Equal(got, []int32{10}) {
			t.Fatalf("the ids read = %v, want [10]", got)
		}

		onRecord, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if onRecord {
			t.Errorf("MoveToNextRecord() = true, want false once the walk has ended")
		}
	})

	// Two scans of the same table run in one transaction, and one of them
	// appends a block. The other scan must find the records in that block, so
	// it cannot rely on the block count that it read when it was opened.
	t.Run("given another scan in the same transaction that appended a block, the walk still finds the records in it", func(t *testing.T) {
		tx := newTestTransaction(t)
		layout := newTestLayout(t)

		reader, err := NewTableScan(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("NewTableScan() for the reader error = %v", err)
		}
		writer, err := NewTableScan(tx, testTableName, layout)
		if err != nil {
			t.Fatalf("NewTableScan() for the writer error = %v", err)
		}

		if err := writer.moveToNewBlock(); err != nil {
			t.Fatalf("moveToNewBlock() error = %v", err)
		}
		claimTestRecord(t, writer, 0, 77)

		if err := reader.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []int32{77}
		if got := walkTestRecords(t, reader); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v: the scan stopped at the blocks it knew about", got, want)
		}
	})
}

func TestTableScanMoveToNewRecord(t *testing.T) {
	t.Run("given a table with a free slot, it takes that slot and marks it as holding a record", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}

		if ts.currentSlot != 0 {
			t.Errorf("currentSlot = %d, want 0", ts.currentSlot)
		}
		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Errorf("the scan is on block %d, want 0", got)
		}

		// The slot must now hold a record, and not only be the position of the
		// scan. Only a scan that restarts can show this.
		if err := ts.SetInt("id", 5); err != nil {
			t.Fatalf("SetInt() error = %v", err)
		}
		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		want := []int32{5}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	// The test inserts one more record than a block holds, so the last record
	// has no slot until the table grows.
	t.Run("given a table with no room left, it appends a block rather than failing", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		want := []int32{}
		for i := range testSlotsInBlock + 1 {
			if err := ts.MoveToNewRecord(); err != nil {
				t.Fatalf("MoveToNewRecord() error = %v on record %d", err, i)
			}
			if err := ts.SetInt("id", int32(i)); err != nil {
				t.Fatalf("SetInt() error = %v on record %d", err, i)
			}
			want = append(want, int32(i))
		}

		size, err := ts.tx.Size(ts.fileName)
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if size != 2 {
			t.Errorf("the table has %d blocks, want 2", size)
		}

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	// The block is filled, one record in the middle is deleted, and the scan is
	// restarted. The next insert must reuse the space of the deleted record, and
	// the table must not grow.
	t.Run("given a block whose records fill it and one deleted, when the scan is put back to the start, then the freed slot is used again rather than the table growing", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		for slot := range testSlotsInBlock {
			claimTestRecord(t, ts, slot, int32(slot))
		}
		ts.currentSlot = 1
		if err := ts.DeleteCurrentRecord(); err != nil {
			t.Fatalf("DeleteCurrentRecord() error = %v", err)
		}

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		if err := ts.MoveToNewRecord(); err != nil {
			t.Fatalf("MoveToNewRecord() error = %v", err)
		}

		if ts.currentSlot != 1 {
			t.Errorf("currentSlot = %d, want 1", ts.currentSlot)
		}
		size, err := ts.tx.Size(ts.fileName)
		if err != nil {
			t.Fatalf("Size() error = %v", err)
		}
		if size != 1 {
			t.Errorf("the table has %d blocks, want 1: the freed slot was passed over", size)
		}
	})
}

func TestTableScanDeleteCurrentRecord(t *testing.T) {
	t.Run("given three records, when the middle one is deleted, then a walk from the start returns the other two", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, 0, 10)
		claimTestRecord(t, ts, 1, 20)
		claimTestRecord(t, ts, 2, 30)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		for range 2 {
			if _, err := ts.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}
		}

		if err := ts.DeleteCurrentRecord(); err != nil {
			t.Fatalf("DeleteCurrentRecord() error = %v", err)
		}

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		want := []int32{10, 30}
		if got := walkTestRecords(t, ts); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})

	t.Run("given a scan that is on no record, it reports ErrNoCurrentRecord", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if err := ts.DeleteCurrentRecord(); !errors.Is(err, ErrNoCurrentRecord) {
			t.Errorf("DeleteCurrentRecord() error = %v, want %v", err, ErrNoCurrentRecord)
		}
	})
}

func TestTableScanClose(t *testing.T) {
	t.Run("when a scan is closed, then the pin on the block it held is given back", func(t *testing.T) {
		tx := newTestTransaction(t)

		ts, err := NewTableScan(tx, testTableName, newTestLayout(t))
		if err != nil {
			t.Fatalf("NewTableScan() error = %v", err)
		}
		if ts.rp == nil {
			t.Fatal("the scan is on no block after it was opened")
		}
		held := ts.rp.BlockID()

		ts.Close()

		if _, err := tx.GetInt(held, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the scan held error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
	})

	// A caller that defers Close and also closes early on some path closes
	// twice, so the second Close must be harmless.
	t.Run("when a scan that is already closed is closed again, then nothing happens, so a caller may both defer it and close early", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		ts.Close()
		ts.Close()
	})

	t.Run("given a scan that has been closed, when a field is read, then it reports ErrNoCurrentRecord rather than reading whatever the buffer now holds", func(t *testing.T) {
		ts := newTestTableScanAt(t, 0)

		ts.Close()

		if _, err := ts.GetInt("id"); !errors.Is(err, ErrNoCurrentRecord) {
			t.Errorf("GetInt() error = %v, want %v", err, ErrNoCurrentRecord)
		}
	})

}
func TestTableScanCurrentRecordID(t *testing.T) {
	t.Run("it names the block and the slot of the record the scan is on", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, 0, 10)
		claimTestRecord(t, ts, 2, 30)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		for range 2 {
			if _, err := ts.MoveToNextRecord(); err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}
		}

		got, err := ts.CurrentRecordID()
		if err != nil {
			t.Fatalf("CurrentRecordID() error = %v", err)
		}

		want := recordmanager.NewRecordID(0, 2)
		if got == nil {
			t.Fatalf("CurrentRecordID() = nil, want %s", want)
		}
		if !got.Equals(want) {
			t.Errorf("CurrentRecordID() = %s, want %s", got, want)
		}
	})

	t.Run("given a scan that is on no record, it reports ErrNoCurrentRecord", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if _, err := ts.CurrentRecordID(); !errors.Is(err, ErrNoCurrentRecord) {
			t.Errorf("CurrentRecordID() error = %v, want %v", err, ErrNoCurrentRecord)
		}
	})

}

func TestTableScanMoveToRecordID(t *testing.T) {
	// A record id exists so that a caller can save a position, read past it,
	// and return to it. The test therefore checks the whole round trip, not
	// each half separately.
	t.Run("given a record noted while walking, when the scan is moved back to it after reading past it, then it reads that record again", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		claimTestRecord(t, ts, 0, 10)
		claimTestRecord(t, ts, 2, 30)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		if _, err := ts.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		noted, err := ts.CurrentRecordID()
		if err != nil {
			t.Fatalf("CurrentRecordID() error = %v", err)
		}

		// Read past the record, so that the scan has to move back to it.
		if _, err := ts.MoveToNextRecord(); err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}

		if err := ts.MoveToRecordID(noted); err != nil {
			t.Fatalf("MoveToRecordID(%s) error = %v", noted, err)
		}

		got, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 10 {
			t.Errorf("GetInt() = %d, want 10", got)
		}
	})

	// The record is in a block that the scan is not on, so reaching it requires a
	// change of block and not only of slot.
	t.Run("given a record id naming a block the scan is not on, it moves onto that block to reach the record", func(t *testing.T) {
		ts := newTestTableScanAt(t, beforeFirstSlot)

		if err := ts.moveToNewBlock(); err != nil {
			t.Fatalf("moveToNewBlock() error = %v", err)
		}
		claimTestRecord(t, ts, 1, 77)

		if err := ts.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}
		if got := ts.rp.BlockID().Number(); got != 0 {
			t.Fatalf("the scan is on block %d, want 0 before the move", got)
		}

		if err := ts.MoveToRecordID(recordmanager.NewRecordID(1, 1)); err != nil {
			t.Fatalf("MoveToRecordID() error = %v", err)
		}

		if got := ts.rp.BlockID().Number(); got != 1 {
			t.Errorf("the scan is on block %d, want 1", got)
		}
		got, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 77 {
			t.Errorf("GetInt() = %d, want 77", got)
		}
	})

	t.Run("given a record id whose slot the block does not hold, it reports recordmanager.ErrSlotOutOfRange", func(t *testing.T) {
		tests := []struct {
			name string
			rid  *recordmanager.RecordID
		}{
			{
				name: "given a record id whose slot does not fit the block, it reports ErrSlotOutOfRange",
				rid:  recordmanager.NewRecordID(0, testSlotsInBlock),
			},
			{
				name: "given a record id whose slot is negative, it reports ErrSlotOutOfRange",
				rid:  recordmanager.NewRecordID(0, -1),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ts := newTestTableScanAt(t, beforeFirstSlot)

				if err := ts.MoveToRecordID(tt.rid); !errors.Is(err, recordmanager.ErrSlotOutOfRange) {
					t.Errorf("MoveToRecordID(%s) error = %v, want %v", tt.rid, err, recordmanager.ErrSlotOutOfRange)
				}
			})
		}
	})
}
