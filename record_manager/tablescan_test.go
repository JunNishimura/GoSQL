package recordmanager

import (
	"errors"
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/transaction"
)

// testTableName is the table the scan tests work on. Its file is testDataFile,
// which is what the record page tests append to directly.
const testTableName = "test"

func TestNewTableScanOnATableWithNoBlocks(t *testing.T) {
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
	if got := ts.rp.blk.Number(); got != 0 {
		t.Errorf("the scan is on block %d, want 0", got)
	}
}

// Every slot of an appended block reads as free already, since the block comes
// back zeroed, so what tells the two branches apart is the other direction: a
// block the table already has must keep the records in it.
func TestNewTableScanOnATableThatAlreadyHasABlock(t *testing.T) {
	const id = 42

	tx := newTestTransaction(t)
	layout := newTestLayout(t)

	blk, err := tx.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	rp, err := NewRecordPage(tx, blk, layout)
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
	if got := ts.rp.blk.Number(); got != 0 {
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
}

func TestTableScanMoveToBlockReleasesTheBlockItLeaves(t *testing.T) {
	tx := newTestTransaction(t)

	ts, err := NewTableScan(tx, testTableName, newTestLayout(t))
	if err != nil {
		t.Fatalf("NewTableScan() error = %v", err)
	}

	if ts.rp == nil {
		t.Fatal("the scan is on no block after it was opened")
	}
	left := ts.rp.blk

	if _, err := tx.Append(testDataFile); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if err := ts.moveToBlock(1); err != nil {
		t.Fatalf("moveToBlock(1) error = %v", err)
	}

	if got := ts.rp.blk.Number(); got != 1 {
		t.Errorf("the scan is on block %d, want 1", got)
	}
	if _, err := tx.GetInt(left, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
		t.Errorf("GetInt() on the block the scan left error = %v, want %v", err, transaction.ErrBlockNotPinned)
	}
}

// newTestTableScanAt opens a scan over a table with one empty block and puts it
// on slot, which is what Next will do for it once that exists.
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
			name: "reads back the value written to the first slot",
			slot: 0,
			val:  42,
		},
		{
			name: "reads back the value written to the last slot of the block",
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
}

func TestTableScanSetStringAndGetString(t *testing.T) {
	tests := []struct {
		name string
		slot int
		val  string
	}{
		{
			name: "reads back the value written to the first slot",
			slot: 0,
			val:  "alice",
		},
		{
			name: "reads back an empty string",
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

// The four field methods take no slot, so what they read and write has to
// follow the scan. Writing at two slots and coming back to the first is what
// tells that apart from always working on the same one.
func TestTableScanFieldsFollowTheSlotTheScanIsOn(t *testing.T) {
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
}

func TestTableScanRejectsFieldsBeforeTheFirstRecord(t *testing.T) {
	tests := []struct {
		name string
		call func(ts *TableScan) error
	}{
		{
			name: "GetInt refuses a scan that is on no record",
			call: func(ts *TableScan) error {
				_, err := ts.GetInt("id")
				return err
			},
		},
		{
			name: "SetInt refuses a scan that is on no record",
			call: func(ts *TableScan) error {
				return ts.SetInt("id", 1)
			},
		},
		{
			name: "GetString refuses a scan that is on no record",
			call: func(ts *TableScan) error {
				_, err := ts.GetString("name")
				return err
			},
		},
		{
			name: "SetString refuses a scan that is on no record",
			call: func(ts *TableScan) error {
				return ts.SetString("name", "x")
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

// The scan adds no checking of its own on the field, so what the record page
// refuses has to reach the caller unchanged.
func TestTableScanPassesOnTheRecordPagesFieldErrors(t *testing.T) {
	tests := []struct {
		name    string
		call    func(ts *TableScan) error
		wantErr error
	}{
		{
			name: "GetInt on a varchar field",
			call: func(ts *TableScan) error {
				_, err := ts.GetInt("name")
				return err
			},
			wantErr: ErrFieldTypeMismatch,
		},
		{
			name: "SetString on an int field",
			call: func(ts *TableScan) error {
				return ts.SetString("id", "x")
			},
			wantErr: ErrFieldTypeMismatch,
		},
		{
			name: "GetString on a field the schema does not have",
			call: func(ts *TableScan) error {
				_, err := ts.GetString("missing")
				return err
			},
			wantErr: ErrFieldNotFound,
		},
		{
			name: "SetInt on a field the schema does not have",
			call: func(ts *TableScan) error {
				return ts.SetInt("missing", 1)
			},
			wantErr: ErrFieldNotFound,
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

// The scan is taken to a second block and put part way into it, so that going
// back to the start has both a block and a slot to undo.
func TestTableScanMoveBeforeFirstRecordReturnsToTheStart(t *testing.T) {
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

	if got := ts.rp.blk.Number(); got != 0 {
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
}

// Going back to the start has to reset the slot even when the scan has not left
// the first block, which an implementation that returns early would miss.
func TestTableScanMoveBeforeFirstRecordFromWithinTheFirstBlock(t *testing.T) {
	ts := newTestTableScanAt(t, 2)

	if err := ts.MoveBeforeFirstRecord(); err != nil {
		t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
	}

	if got := ts.rp.blk.Number(); got != 0 {
		t.Errorf("the scan is on block %d, want 0", got)
	}
	if ts.currentSlot != beforeFirstSlot {
		t.Errorf("currentSlot = %d, want %d", ts.currentSlot, beforeFirstSlot)
	}

	if _, err := ts.GetInt("id"); !errors.Is(err, ErrNoCurrentRecord) {
		t.Errorf("GetInt() error = %v, want %v: the scan is on a record it should not be on", err, ErrNoCurrentRecord)
	}
}

// claimTestRecord marks slot of the block the scan is on as holding a record
// and writes id into it. Insert does not exist yet, so a test that needs
// records in particular slots puts them there itself.
func claimTestRecord(t *testing.T, ts *TableScan, slot int, id int32) {
	t.Helper()

	if err := ts.rp.setSlotState(slot, slotInUse); err != nil {
		t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
	}
	if err := ts.rp.SetInt(slot, "id", id); err != nil {
		t.Fatalf("SetInt(%d) error = %v", slot, err)
	}
}

// walkTestRecords reads the id of every record from where the scan is to the
// end of the table.
func walkTestRecords(t *testing.T, ts *TableScan) []int32 {
	t.Helper()

	ids := []int32{}
	for {
		onRecord, err := ts.MoveToNextRecord()
		if err != nil {
			t.Fatalf("MoveToNextRecord() error = %v", err)
		}
		if !onRecord {
			return ids
		}

		id, err := ts.GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		ids = append(ids, id)
	}
}

// Slot 1 is left free between the two records, so a walk that reported every
// slot rather than every record would come back with three ids.
func TestTableScanMoveToNextRecordWalksOneBlock(t *testing.T) {
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
}

// The first record is in the last slot of block 0 and the second in block 1, so
// the walk has to carry on past the end of a block to find both.
func TestTableScanMoveToNextRecordCrossesIntoTheNextBlock(t *testing.T) {
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
}

// Block 0 holds nothing at all, so the walk has to move on from a block it
// found no records in rather than take that for the end of the table.
func TestTableScanMoveToNextRecordPassesOverAnEmptyBlock(t *testing.T) {
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
}

func TestTableScanMoveToNextRecordOnATableWithNoRecords(t *testing.T) {
	ts := newTestTableScanAt(t, beforeFirstSlot)

	onRecord, err := ts.MoveToNextRecord()
	if err != nil {
		t.Fatalf("MoveToNextRecord() error = %v", err)
	}
	if onRecord {
		t.Errorf("MoveToNextRecord() = true, want false on a table that holds no records")
	}
}

// A walk that has ended stays ended: asking again must not wrap round to the
// start or run off the end of the file.
func TestTableScanMoveToNextRecordStaysAtTheEnd(t *testing.T) {
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
}

// Two scans over the same table in one transaction, one of which appends a
// block. The other has to find the records in it, which is what stops a scan
// from settling on a block count it read when it opened.
func TestTableScanMoveToNextRecordSeesABlockAnotherScanAppended(t *testing.T) {
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
}
