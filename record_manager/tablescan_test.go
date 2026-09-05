package recordmanager

import (
	"errors"
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
