package recordmanager

import (
	"errors"
	"fmt"
	"testing"

	buffermanager "github.com/JunNishimura/GoSQL/buffer_manager"
	concurrencymanager "github.com/JunNishimura/GoSQL/concurrency_manager"
	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

const (
	testLogFile    = "test.log"
	testDataFile   = "test.tbl"
	testBlockSize  = 400
	testNumBuffers = 3
)

// newTestTransaction builds a transaction over a database of its own, so that
// one test cannot see what another wrote.
func newTestTransaction(t *testing.T) *transaction.Transaction {
	t.Helper()

	fm, err := filemanager.NewFileManager(t.TempDir(), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	bm, err := buffermanager.NewBufferManager(fm, lm, testNumBuffers)
	if err != nil {
		t.Fatalf("NewBufferManager() error = %v", err)
	}

	tx, err := transaction.NewTransaction(fm, lm, bm, concurrencymanager.NewLockTable(), 1)
	if err != nil {
		t.Fatalf("NewTransaction() error = %v", err)
	}

	return tx
}

// newTestLayout builds a layout over the schema the record page tests share:
// "id" is an int and "name" a varchar of 20 characters. One field of each kind
// is what lets a test reach for the wrong one on purpose.
func newTestLayout(t *testing.T) *Layout {
	t.Helper()

	s := NewSchema()
	mustAddIntField(t, s, "id")
	mustAddStringField(t, s, "name", 20)

	return NewLayout(s)
}

func TestNewRecordPage(t *testing.T) {
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

	if rp.tx != tx {
		t.Errorf("tx = %p, want %p", rp.tx, tx)
	}
	if rp.blk != blk {
		t.Errorf("blk = %v, want %v", rp.blk, blk)
	}
	if rp.layout != layout {
		t.Errorf("layout = %p, want %p", rp.layout, layout)
	}
}

func TestNewRecordPagePinsTheBlock(t *testing.T) {
	tx := newTestTransaction(t)
	layout := newTestLayout(t)

	blk, err := tx.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if _, err := tx.GetInt(blk, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
		t.Fatalf("GetInt() before the record page error = %v, want %v", err, transaction.ErrBlockNotPinned)
	}

	if _, err := NewRecordPage(tx, blk, layout); err != nil {
		t.Fatalf("NewRecordPage() error = %v", err)
	}

	if _, err := tx.GetInt(blk, 0); err != nil {
		t.Errorf("GetInt() after the record page error = %v, want nil: the block was not pinned", err)
	}
}

// The numbers the slot tests rely on, spelled out once:
//
//	a block         400 bytes
//	a slot           92 bytes, the in-use flag (4) plus id (4) plus name (84)
//	slots per block   4, so 0 through 3 fit and 4 does not
const (
	testSlotSize     = 92
	testSlotsInBlock = testBlockSize / testSlotSize
)

// newTestRecordPage builds a record page over a freshly appended block, so that
// every slot in it starts out zeroed.
func newTestRecordPage(t *testing.T) *RecordPage {
	t.Helper()

	tx := newTestTransaction(t)

	blk, err := tx.Append(testDataFile)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	rp, err := NewRecordPage(tx, blk, newTestLayout(t))
	if err != nil {
		t.Fatalf("NewRecordPage() error = %v", err)
	}

	return rp
}

func TestRecordPageSetIntAndGetInt(t *testing.T) {
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
			name: "reads back the value written to the last slot that fits",
			slot: testSlotsInBlock - 1,
			val:  7,
		},
		{
			name: "reads back a negative value",
			slot: 0,
			val:  -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			if err := rp.SetInt(tt.slot, "id", tt.val); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			got, err := rp.GetInt(tt.slot, "id")
			if err != nil {
				t.Fatalf("GetInt() error = %v", err)
			}
			if got != tt.val {
				t.Errorf("GetInt() = %d, want %d", got, tt.val)
			}
		})
	}
}

func TestRecordPageSetStringAndGetString(t *testing.T) {
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
			name: "reads back the value written to the last slot that fits",
			slot: testSlotsInBlock - 1,
			val:  "bob",
		},
		{
			name: "reads back an empty string",
			slot: 0,
			val:  "",
		},
		{
			name: "reads back a string of multi-byte characters",
			slot: 0,
			val:  "テスト",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			if err := rp.SetString(tt.slot, "name", tt.val); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}

			got, err := rp.GetString(tt.slot, "name")
			if err != nil {
				t.Fatalf("GetString() error = %v", err)
			}
			if got != tt.val {
				t.Errorf("GetString() = %q, want %q", got, tt.val)
			}
		})
	}
}

func TestRecordPageSlotsDoNotOverlap(t *testing.T) {
	rp := newTestRecordPage(t)

	for slot := range testSlotsInBlock {
		if err := rp.SetInt(slot, "id", int32(slot)); err != nil {
			t.Fatalf("SetInt(%d) error = %v", slot, err)
		}
		if err := rp.SetString(slot, "name", fmt.Sprintf("name %d", slot)); err != nil {
			t.Fatalf("SetString(%d) error = %v", slot, err)
		}
	}

	for slot := range testSlotsInBlock {
		gotID, err := rp.GetInt(slot, "id")
		if err != nil {
			t.Fatalf("GetInt(%d) error = %v", slot, err)
		}
		if gotID != int32(slot) {
			t.Errorf("GetInt(%d) = %d, want %d: a later slot wrote over it", slot, gotID, slot)
		}

		wantName := fmt.Sprintf("name %d", slot)
		gotName, err := rp.GetString(slot, "name")
		if err != nil {
			t.Fatalf("GetString(%d) error = %v", slot, err)
		}
		if gotName != wantName {
			t.Errorf("GetString(%d) = %q, want %q: a later slot wrote over it", slot, gotName, wantName)
		}
	}
}

// Slot testSlotsInBlock is the first one whose bytes would run past the end of
// the block, and -1 stands for any slot below the first.
func TestRecordPageRejectsASlotOutsideTheBlock(t *testing.T) {
	tests := []struct {
		name string
		call func(rp *RecordPage) error
	}{
		{
			name: "GetInt refuses the first slot that does not fit",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(testSlotsInBlock, "id")
				return err
			},
		},
		{
			name: "GetInt refuses a negative slot",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(-1, "id")
				return err
			},
		},
		{
			name: "SetInt refuses the first slot that does not fit",
			call: func(rp *RecordPage) error {
				return rp.SetInt(testSlotsInBlock, "id", 1)
			},
		},
		{
			name: "SetInt refuses a negative slot",
			call: func(rp *RecordPage) error {
				return rp.SetInt(-1, "id", 1)
			},
		},
		{
			name: "GetString refuses the first slot that does not fit",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(testSlotsInBlock, "name")
				return err
			},
		},
		{
			name: "GetString refuses a negative slot",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(-1, "name")
				return err
			},
		},
		{
			name: "SetString refuses the first slot that does not fit",
			call: func(rp *RecordPage) error {
				return rp.SetString(testSlotsInBlock, "name", "x")
			},
		},
		{
			name: "SetString refuses a negative slot",
			call: func(rp *RecordPage) error {
				return rp.SetString(-1, "name", "x")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(newTestRecordPage(t)); !errors.Is(err, ErrSlotOutOfRange) {
				t.Errorf("error = %v, want %v", err, ErrSlotOutOfRange)
			}
		})
	}
}

// The test schema has "id" as an int and "name" as a varchar, so each case
// below reaches for the field of the type its method does not read or write.
func TestRecordPageRejectsTheWrongFieldType(t *testing.T) {
	tests := []struct {
		name string
		call func(rp *RecordPage) error
	}{
		{
			name: "GetInt refuses a varchar field",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(0, "name")
				return err
			},
		},
		{
			name: "SetInt refuses a varchar field",
			call: func(rp *RecordPage) error {
				return rp.SetInt(0, "name", 1)
			},
		},
		{
			name: "GetString refuses an int field",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(0, "id")
				return err
			},
		},
		{
			name: "SetString refuses an int field",
			call: func(rp *RecordPage) error {
				return rp.SetString(0, "id", "x")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(newTestRecordPage(t)); !errors.Is(err, ErrFieldTypeMismatch) {
				t.Errorf("error = %v, want %v", err, ErrFieldTypeMismatch)
			}
		})
	}
}

func TestRecordPageRejectsAnUnknownField(t *testing.T) {
	tests := []struct {
		name string
		call func(rp *RecordPage) error
	}{
		{
			name: "GetInt refuses a field the schema does not have",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(0, "missing")
				return err
			},
		},
		{
			name: "SetInt refuses a field the schema does not have",
			call: func(rp *RecordPage) error {
				return rp.SetInt(0, "missing", 1)
			},
		},
		{
			name: "GetString refuses a field the schema does not have",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(0, "missing")
				return err
			},
		},
		{
			name: "SetString refuses a field the schema does not have",
			call: func(rp *RecordPage) error {
				return rp.SetString(0, "missing", "x")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(newTestRecordPage(t)); !errors.Is(err, ErrFieldNotFound) {
				t.Errorf("error = %v, want %v", err, ErrFieldNotFound)
			}
		})
	}
}

// readSlotState reads the flag at the front of slot. Nothing exposes it, so a
// test that wants to see what Delete wrote has to go through the transaction
// the record page writes with.
func readSlotState(t *testing.T, rp *RecordPage, slot int) slotState {
	t.Helper()

	offset, err := rp.slotOffset(slot)
	if err != nil {
		t.Fatalf("slotOffset(%d) error = %v", slot, err)
	}

	flag, err := rp.tx.GetInt(rp.blk, offset)
	if err != nil {
		t.Fatalf("GetInt() at the flag of slot %d error = %v", slot, err)
	}

	return slotState(flag)
}

func TestRecordPageSetSlotState(t *testing.T) {
	tests := []struct {
		name string
		// states are written to the same slot in turn, so that a case can
		// check a slot goes back to a state it has already left.
		states []slotState
		want   slotState
	}{
		{
			name:   "marks a slot as holding a record",
			states: []slotState{slotInUse},
			want:   slotInUse,
		},
		{
			name:   "marks a slot that held a record as holding none",
			states: []slotState{slotInUse, slotEmpty},
			want:   slotEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			for _, state := range tt.states {
				if err := rp.setSlotState(0, state); err != nil {
					t.Fatalf("setSlotState(0, %d) error = %v", state, err)
				}
			}

			if got := readSlotState(t, rp, 0); got != tt.want {
				t.Errorf("slot 0 flag = %d, want %d", got, tt.want)
			}
		})
	}
}

// A block comes back from Append filled with zeroes, which is slotEmpty, so the
// slot has to be marked in use before a delete can be seen to have done
// anything.
func TestRecordPageDelete(t *testing.T) {
	rp := newTestRecordPage(t)

	if err := rp.setSlotState(1, slotInUse); err != nil {
		t.Fatalf("setSlotState(1, slotInUse) error = %v", err)
	}
	if got := readSlotState(t, rp, 1); got != slotInUse {
		t.Fatalf("slot 1 flag = %d, want %d before the delete", got, slotInUse)
	}

	if err := rp.Delete(1); err != nil {
		t.Fatalf("Delete(1) error = %v", err)
	}

	if got := readSlotState(t, rp, 1); got != slotEmpty {
		t.Errorf("slot 1 flag = %d, want %d", got, slotEmpty)
	}
}

func TestRecordPageDeleteLeavesTheOtherSlotsAlone(t *testing.T) {
	const deleted = 1

	rp := newTestRecordPage(t)

	for slot := range testSlotsInBlock {
		if err := rp.setSlotState(slot, slotInUse); err != nil {
			t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
		}
	}

	if err := rp.Delete(deleted); err != nil {
		t.Fatalf("Delete(%d) error = %v", deleted, err)
	}

	for slot := range testSlotsInBlock {
		want := slotInUse
		if slot == deleted {
			want = slotEmpty
		}

		if got := readSlotState(t, rp, slot); got != want {
			t.Errorf("slot %d flag = %d, want %d", slot, got, want)
		}
	}
}

func TestRecordPageDeleteKeepsTheFieldsOfTheOtherSlots(t *testing.T) {
	const deleted = 1

	rp := newTestRecordPage(t)

	for slot := range testSlotsInBlock {
		if err := rp.SetInt(slot, "id", int32(slot)); err != nil {
			t.Fatalf("SetInt(%d) error = %v", slot, err)
		}
	}

	if err := rp.Delete(deleted); err != nil {
		t.Fatalf("Delete(%d) error = %v", deleted, err)
	}

	for slot := range testSlotsInBlock {
		got, err := rp.GetInt(slot, "id")
		if err != nil {
			t.Fatalf("GetInt(%d) error = %v", slot, err)
		}
		if got != int32(slot) {
			t.Errorf("GetInt(%d) = %d, want %d: the delete wrote outside the flag", slot, got, slot)
		}
	}
}

func TestRecordPageDeleteRejectsASlotOutsideTheBlock(t *testing.T) {
	tests := []struct {
		name string
		slot int
	}{
		{
			name: "refuses the first slot that does not fit",
			slot: testSlotsInBlock,
		},
		{
			name: "refuses a negative slot",
			slot: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			if err := rp.Delete(tt.slot); !errors.Is(err, ErrSlotOutOfRange) {
				t.Errorf("Delete(%d) error = %v, want %v", tt.slot, err, ErrSlotOutOfRange)
			}
		})
	}
}

func TestRecordPageIsValidSlot(t *testing.T) {
	tests := []struct {
		name string
		slot int
		want bool
	}{
		{
			name: "accepts the first slot",
			slot: 0,
			want: true,
		},
		{
			name: "accepts the last slot whose bytes all fit in the block",
			slot: testSlotsInBlock - 1,
			want: true,
		},
		{
			name: "refuses the slot whose bytes would run past the end of the block",
			slot: testSlotsInBlock,
			want: false,
		},
		{
			name: "refuses a negative slot",
			slot: -1,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newTestRecordPage(t).isValidSlot(tt.slot); got != tt.want {
				t.Errorf("isValidSlot(%d) = %t, want %t", tt.slot, got, tt.want)
			}
		})
	}
}

// A block starts out as four empty slots, so a case marks the ones it wants in
// use and leaves the rest. Slot -1 is how a search asks to start at slot 0.
func TestRecordPageSearchAfter(t *testing.T) {
	tests := []struct {
		name  string
		inUse []int
		start int
		state slotState
		want  int
	}{
		{
			name:  "finds the first slot in use when asked to start at the beginning",
			inUse: []int{1, 3},
			start: -1,
			state: slotInUse,
			want:  1,
		},
		{
			name:  "skips the slot it is given and finds the next one in use",
			inUse: []int{1, 3},
			start: 1,
			state: slotInUse,
			want:  3,
		},
		{
			name:  "finds the last slot of the block",
			inUse: []int{3},
			start: -1,
			state: slotInUse,
			want:  3,
		},
		{
			name:  "finds an empty slot among slots in use",
			inUse: []int{0, 1, 3},
			start: -1,
			state: slotEmpty,
			want:  2,
		},
		{
			name:  "finds the first slot when every slot matches",
			inUse: []int{0, 1, 2, 3},
			start: -1,
			state: slotInUse,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)
			for _, slot := range tt.inUse {
				if err := rp.setSlotState(slot, slotInUse); err != nil {
					t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
				}
			}

			got, err := rp.searchAfter(tt.start, tt.state)
			if err != nil {
				t.Fatalf("searchAfter(%d) error = %v", tt.start, err)
			}
			if got != tt.want {
				t.Errorf("searchAfter(%d) = %d, want %d", tt.start, got, tt.want)
			}
		})
	}
}

func TestRecordPageSearchAfterReportsNoSuchSlot(t *testing.T) {
	tests := []struct {
		name  string
		inUse []int
		start int
		state slotState
	}{
		{
			name:  "when no slot in the block is in use",
			inUse: nil,
			start: -1,
			state: slotInUse,
		},
		{
			name:  "when the only slot in use is at or before the one it starts from",
			inUse: []int{1},
			start: 1,
			state: slotInUse,
		},
		{
			name:  "when it starts from the last slot of the block",
			inUse: nil,
			start: testSlotsInBlock - 1,
			state: slotEmpty,
		},
		{
			name:  "when it starts from beyond the end of the block",
			inUse: []int{0},
			start: testSlotsInBlock,
			state: slotInUse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)
			for _, slot := range tt.inUse {
				if err := rp.setSlotState(slot, slotInUse); err != nil {
					t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
				}
			}

			if _, err := rp.searchAfter(tt.start, tt.state); !errors.Is(err, ErrNoSuchSlot) {
				t.Errorf("searchAfter(%d) error = %v, want %v", tt.start, err, ErrNoSuchSlot)
			}
		})
	}
}
