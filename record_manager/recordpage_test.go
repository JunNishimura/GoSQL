package recordmanager

import (
	"errors"
	"fmt"
	"strings"
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
	// testStringFieldLength is the character limit of the varchar field the
	// tests share. It is named rather than written out so that a case about the
	// limit reads as being about the limit, whatever the number happens to be.
	testStringFieldLength = 20
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
	mustAddStringField(t, s, "name", testStringFieldLength)

	return NewLayout(s)
}

func TestNewRecordPage(t *testing.T) {
	t.Run("it carries the transaction, the block and the layout it was made from", func(t *testing.T) {
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
	})

	t.Run("given a block the transaction has not pinned, it pins it, so the page can be read", func(t *testing.T) {
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
	})
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

// The two widths either side of a block, for a layout of one varchar field:
//
//	varchar(98)  4 + (4 + 392) = 400 bytes, exactly one slot to a block
//	varchar(99)  4 + (4 + 396) = 404 bytes, and no slot fits at all
const (
	widestFittingFieldLength = 98
	overwideFieldLength      = 99
)

// newLayoutOfOneStringField builds a layout of a single varchar, which is what
// lets a test put a slot at a width of its choosing.
func newLayoutOfOneStringField(t *testing.T, length int) *Layout {
	t.Helper()

	s := NewSchema()
	mustAddStringField(t, s, "definition", length)

	return NewLayout(s)
}

// A layout whose slot does not fit in a block is refused when the record page
// is made, rather than at the first read or write of it.
//
// Nothing can be done with such a layout: slot 0 already runs past the end of
// the block, so every slot is out of range and the page holds no records at
// all. Left to be found later it is worse than useless, because the caller that
// finds it is TableScan.MoveToNewRecord, which reads "no free slot in this
// block" as "append another block and look again" and does so forever, growing
// the file until the disk fills.
func TestNewRecordPageRejectsASlotWiderThanABlock(t *testing.T) {
	t.Run("given a layout whose slot is wider than a block, when a record page is made on it, then it reports ErrSlotWiderThanBlock", func(t *testing.T) {
		tx := newTestTransaction(t)

		blk, err := tx.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		layout := newLayoutOfOneStringField(t, overwideFieldLength)

		if _, err := NewRecordPage(tx, blk, layout); !errors.Is(err, ErrSlotWiderThanBlock) {
			t.Errorf("error = %v, want %v", err, ErrSlotWiderThanBlock)
		}

		// The block is left as it was found. Pinning it and then refusing would
		// hold a buffer for a page that was never handed out, and nothing would
		// know to give it back.
		if _, err := tx.GetInt(blk, 0); err == nil {
			t.Error("GetInt() after the refused record page error = nil, want an error: the block was pinned and left that way")
		}
	})

	t.Run("given a layout whose slot is exactly as wide as a block, when a record page is made on it, then it is made", func(t *testing.T) {
		tx := newTestTransaction(t)

		blk, err := tx.Append(testDataFile)
		if err != nil {
			t.Fatalf("Append() error = %v", err)
		}

		layout := newLayoutOfOneStringField(t, widestFittingFieldLength)
		if got := layout.SlotSize(); got != testBlockSize {
			t.Fatalf("the layout has slots of %d bytes, want %d: the case is not testing the boundary", got, testBlockSize)
		}

		if _, err := NewRecordPage(tx, blk, layout); err != nil {
			t.Errorf("NewRecordPage() error = %v, want nil: one slot of this width fits", err)
		}
	})
}

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
			name: "when an int is written to the first slot and read back, then it is unchanged",
			slot: 0,
			val:  42,
		},
		{
			name: "when an int is written to the last slot that fits and read back, then it is unchanged",
			slot: testSlotsInBlock - 1,
			val:  7,
		},
		{
			name: "when a negative int is written and read back, then the sign survives",
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
			name: "when a string is written to the first slot and read back, then it is unchanged",
			slot: 0,
			val:  "alice",
		},
		{
			name: "when a string is written to the last slot that fits and read back, then it is unchanged",
			slot: testSlotsInBlock - 1,
			val:  "bob",
		},
		{
			name: "when an empty string is written and read back, then it is still empty",
			slot: 0,
			val:  "",
		},
		{
			name: "when a string of multi-byte characters is written and read back, then it is unchanged",
			slot: 0,
			val:  "テスト",
		},
		{
			name: "when a string of exactly as many characters as the field allows is written and read back, then it is unchanged",
			slot: 0,
			val:  strings.Repeat("a", testStringFieldLength),
		},
		{
			name: "when a string of multi-byte characters fills the field to its limit and is read back, then it is unchanged",
			slot: 0,
			val:  strings.Repeat("あ", testStringFieldLength),
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

func TestRecordPageSlotOffset(t *testing.T) {
	t.Run("when every slot of the block is written to, then each one keeps its own values and none writes over its neighbour", func(t *testing.T) {
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
	})
}

// Slot testSlotsInBlock is the first one whose bytes would run past the end of
// the block, and -1 stands for any slot below the first.
func TestRecordPageRejectsASlotOutsideTheBlock(t *testing.T) {
	tests := []struct {
		name string
		call func(rp *RecordPage) error
	}{
		{
			name: "when GetInt is given the first slot that does not fit the block, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(testSlotsInBlock, "id")
				return err
			},
		},
		{
			name: "when GetInt is given a negative slot, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(-1, "id")
				return err
			},
		},
		{
			name: "when SetInt is given the first slot that does not fit the block, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				return rp.SetInt(testSlotsInBlock, "id", 1)
			},
		},
		{
			name: "when SetInt is given a negative slot, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				return rp.SetInt(-1, "id", 1)
			},
		},
		{
			name: "when GetString is given the first slot that does not fit the block, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(testSlotsInBlock, "name")
				return err
			},
		},
		{
			name: "when GetString is given a negative slot, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(-1, "name")
				return err
			},
		},
		{
			name: "when SetString is given the first slot that does not fit the block, then it reports ErrSlotOutOfRange",
			call: func(rp *RecordPage) error {
				return rp.SetString(testSlotsInBlock, "name", "x")
			},
		},
		{
			name: "when SetString is given a negative slot, then it reports ErrSlotOutOfRange",
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
			name: "given a varchar field, when GetInt is called on it, then it reports ErrFieldTypeMismatch",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(0, "name")
				return err
			},
		},
		{
			name: "given a varchar field, when SetInt is called on it, then it reports ErrFieldTypeMismatch",
			call: func(rp *RecordPage) error {
				return rp.SetInt(0, "name", 1)
			},
		},
		{
			name: "given an int field, when GetString is called on it, then it reports ErrFieldTypeMismatch",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(0, "id")
				return err
			},
		},
		{
			name: "given an int field, when SetString is called on it, then it reports ErrFieldTypeMismatch",
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

// A varchar field is given room for its character limit in the widest encoding
// there is, so an over-long string only reaches past the field once it is
// longer than that: four bytes for every character the field allows. Below
// that the string is written and read back intact, and the only thing wrong
// with it is that the field now holds more characters than its schema says it
// can. Above it, the write runs into whatever follows the field in the block,
// which for the last field of a slot is the next slot's in-use flag.
//
// Both are the same fault, so both are refused by the same rule, and the cases
// below cover either side of that boundary.
func TestRecordPageRejectsAStringLongerThanItsField(t *testing.T) {
	// A slot holds the in-use flag, an int and the varchar, so a string long
	// enough to run past the varchar runs into the slot after it.
	const charsPastTheSlot = testStringFieldLength*4 + 1

	tests := []struct {
		name string
		val  string
	}{
		{
			name: "given a varchar field, when a string one character over its limit is written, then it reports ErrStringTooLong",
			val:  strings.Repeat("a", testStringFieldLength+1),
		},
		{
			name: "given a varchar field, when a string of multi-byte characters one character over its limit is written, then it reports ErrStringTooLong",
			val:  strings.Repeat("あ", testStringFieldLength+1),
		},
		{
			name: "given a varchar field, when a string long enough to reach past the slot is written, then it reports ErrStringTooLong",
			val:  strings.Repeat("a", charsPastTheSlot),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			// The slot after the one written to is marked, so that a write that
			// reached past its own slot shows up as that mark being gone rather
			// than as a byte no test can name.
			if err := rp.setSlotState(1, slotInUse); err != nil {
				t.Fatalf("setSlotState() error = %v", err)
			}

			if err := rp.SetString(0, "name", tt.val); !errors.Is(err, ErrStringTooLong) {
				t.Errorf("error = %v, want %v", err, ErrStringTooLong)
			}

			slotOffset, err := rp.slotOffset(1)
			if err != nil {
				t.Fatalf("slotOffset() error = %v", err)
			}
			flag, err := rp.tx.GetInt(rp.blk, slotOffset)
			if err != nil {
				t.Fatalf("GetInt() error = %v", err)
			}
			if slotState(flag) != slotInUse {
				t.Errorf("the slot after the one written to is %d, want %d: the write reached past its own slot", flag, slotInUse)
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
			name: "given a field the schema does not have, when GetInt asks for it, then it reports ErrFieldNotFound",
			call: func(rp *RecordPage) error {
				_, err := rp.GetInt(0, "missing")
				return err
			},
		},
		{
			name: "given a field the schema does not have, when SetInt writes to it, then it reports ErrFieldNotFound",
			call: func(rp *RecordPage) error {
				return rp.SetInt(0, "missing", 1)
			},
		},
		{
			name: "given a field the schema does not have, when GetString asks for it, then it reports ErrFieldNotFound",
			call: func(rp *RecordPage) error {
				_, err := rp.GetString(0, "missing")
				return err
			},
		},
		{
			name: "given a field the schema does not have, when SetString writes to it, then it reports ErrFieldNotFound",
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
			name:   "when a slot is marked as holding a record, then it reads as in use",
			states: []slotState{slotInUse},
			want:   slotInUse,
		},
		{
			name:   "given a slot already in use, when it is marked as holding none, then it reads as free again",
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

func TestRecordPageDelete(t *testing.T) {
	// A block comes back from Append filled with zeroes, which is slotEmpty, so
	// the slot has to be marked in use before a delete can be seen to have done
	// anything.
	t.Run("given a slot holding a record, when it is deleted, then the slot reads as free", func(t *testing.T) {
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
	})

	t.Run("given every slot holding a record, when one is deleted, then the others still read as in use", func(t *testing.T) {
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
	})

	t.Run("when a slot is deleted, then it writes the flag alone and leaves every field where it was", func(t *testing.T) {
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
	})

	slotTests := []struct {
		name string
		slot int
	}{
		{
			name: "given the first slot that does not fit the block, it reports ErrSlotOutOfRange",
			slot: testSlotsInBlock,
		},
		{
			name: "given a negative slot, it reports ErrSlotOutOfRange",
			slot: -1,
		},
	}

	for _, tt := range slotTests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)

			if err := rp.Delete(tt.slot); !errors.Is(err, ErrSlotOutOfRange) {
				t.Errorf("Delete(%d) error = %v, want %v", tt.slot, err, ErrSlotOutOfRange)
			}
		})
	}
}

func TestRecordPageHasSlot(t *testing.T) {
	tests := []struct {
		name string
		slot int
		want bool
	}{
		{
			name: "given the first slot of the block, it reports the slot is one the block holds",
			slot: 0,
			want: true,
		},
		{
			name: "given the last slot whose bytes all fit, it reports the slot is one the block holds",
			slot: testSlotsInBlock - 1,
			want: true,
		},
		{
			name: "given a slot whose bytes would run past the end of the block, it reports the block does not hold it",
			slot: testSlotsInBlock,
			want: false,
		},
		{
			name: "given a negative slot, it reports the block does not hold it",
			slot: -1,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newTestRecordPage(t).HasSlot(tt.slot); got != tt.want {
				t.Errorf("HasSlot(%d) = %t, want %t", tt.slot, got, tt.want)
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
			name:  "given slots 1 and 3 in use, when the search starts before the first slot, then it finds slot 1",
			inUse: []int{1, 3},
			start: -1,
			state: slotInUse,
			want:  1,
		},
		{
			name:  "given slots 1 and 3 in use, when the search starts at slot 1, then it skips it and finds slot 3",
			inUse: []int{1, 3},
			start: 1,
			state: slotInUse,
			want:  3,
		},
		{
			name:  "given only the last slot of the block in use, it is found rather than passed over",
			inUse: []int{3},
			start: -1,
			state: slotInUse,
			want:  3,
		},
		{
			name:  "given slots in use either side of a free one, when a free slot is searched for, then the one between them is found",
			inUse: []int{0, 1, 3},
			start: -1,
			state: slotEmpty,
			want:  2,
		},
		{
			name:  "given every slot in use, when the search starts before the first slot, then it finds slot 0",
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

	noSuchSlotTests := []struct {
		name  string
		inUse []int
		start int
		state slotState
	}{
		{
			name:  "given a block with no slot in use, it reports ErrNoSuchSlot",
			inUse: nil,
			start: -1,
			state: slotInUse,
		},
		{
			name:  "given the only slot in use is the one the search starts from, it reports ErrNoSuchSlot",
			inUse: []int{1},
			start: 1,
			state: slotInUse,
		},
		{
			name:  "given a search starting from the last slot of the block, it reports ErrNoSuchSlot",
			inUse: nil,
			start: testSlotsInBlock - 1,
			state: slotEmpty,
		},
		{
			name:  "given a search starting past the end of the block, it reports ErrNoSuchSlot rather than a slot error",
			inUse: []int{0},
			start: testSlotsInBlock,
			state: slotInUse,
		},
	}

	for _, tt := range noSuchSlotTests {
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

func TestRecordPageNextUsedSlotAfter(t *testing.T) {
	tests := []struct {
		name  string
		inUse []int
		start int
		want  int
	}{
		{
			name:  "given records at slots 1 and 3, when the walk starts before the first slot, then it finds the one at slot 1",
			inUse: []int{1, 3},
			start: -1,
			want:  1,
		},
		{
			name:  "given records at slots 1 and 3, when the walk starts at slot 1, then it skips it and finds the one at slot 3",
			inUse: []int{1, 3},
			start: 1,
			want:  3,
		},
		{
			name:  "given the only record in the last slot of the block, it is found rather than passed over",
			inUse: []int{3},
			start: -1,
			want:  3,
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

			got, err := rp.NextUsedSlotAfter(tt.start)
			if err != nil {
				t.Fatalf("NextUsedSlotAfter(%d) error = %v", tt.start, err)
			}
			if got != tt.want {
				t.Errorf("NextUsedSlotAfter(%d) = %d, want %d", tt.start, got, tt.want)
			}
		})
	}

	noSuchSlotTests := []struct {
		name  string
		inUse []int
		start int
	}{
		{
			name:  "given a block that holds no records, it reports ErrNoSuchSlot",
			inUse: nil,
			start: -1,
		},
		{
			name:  "given the last record is at the slot the walk starts from, it reports ErrNoSuchSlot",
			inUse: []int{2},
			start: 2,
		},
	}

	for _, tt := range noSuchSlotTests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)
			for _, slot := range tt.inUse {
				if err := rp.setSlotState(slot, slotInUse); err != nil {
					t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
				}
			}

			if _, err := rp.NextUsedSlotAfter(tt.start); !errors.Is(err, ErrNoSuchSlot) {
				t.Errorf("NextUsedSlotAfter(%d) error = %v, want %v", tt.start, err, ErrNoSuchSlot)
			}
		})
	}
}

func TestRecordPageClaimFreeSlotAfter(t *testing.T) {
	tests := []struct {
		name  string
		inUse []int
		start int
		want  int
	}{
		{
			name:  "given a block with no records, it takes slot 0",
			inUse: nil,
			start: -1,
			want:  0,
		},
		{
			name:  "given slots 0, 1 and 3 in use, it takes slot 2, the first free one",
			inUse: []int{0, 1, 3},
			start: -1,
			want:  2,
		},
		{
			name:  "given a block with no records, when it starts at slot 0, then it skips that slot and takes slot 1",
			inUse: nil,
			start: 0,
			want:  1,
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

			got, err := rp.ClaimFreeSlotAfter(tt.start)
			if err != nil {
				t.Fatalf("ClaimFreeSlotAfter(%d) error = %v", tt.start, err)
			}
			if got != tt.want {
				t.Fatalf("ClaimFreeSlotAfter(%d) = %d, want %d", tt.start, got, tt.want)
			}

			if state := readSlotState(t, rp, got); state != slotInUse {
				t.Errorf("slot %d flag = %d, want %d: the slot was returned but not marked", got, state, slotInUse)
			}
		})
	}

	// Claiming from the start of the block over and over is what an insert into
	// a table does, so each claim has to see the marks the ones before it left.
	t.Run("when it is asked from the start of the block over and over, then it gives out each slot once and then reports ErrNoSuchSlot", func(t *testing.T) {
		rp := newTestRecordPage(t)

		for want := range testSlotsInBlock {
			got, err := rp.ClaimFreeSlotAfter(-1)
			if err != nil {
				t.Fatalf("ClaimFreeSlotAfter(-1) error = %v on claim %d", err, want)
			}
			if got != want {
				t.Fatalf("ClaimFreeSlotAfter(-1) = %d, want %d: an earlier claim did not stick", got, want)
			}
		}

		if _, err := rp.ClaimFreeSlotAfter(-1); !errors.Is(err, ErrNoSuchSlot) {
			t.Errorf("ClaimFreeSlotAfter(-1) error = %v, want %v once the block is full", err, ErrNoSuchSlot)
		}
	})

	noSuchSlotTests := []struct {
		name  string
		inUse []int
		start int
	}{
		{
			name:  "given every slot in the block in use, it reports ErrNoSuchSlot",
			inUse: []int{0, 1, 2, 3},
			start: -1,
		},
		{
			name:  "given the only free slots are at or before the one it starts from, it reports ErrNoSuchSlot",
			inUse: []int{2, 3},
			start: 1,
		},
	}

	for _, tt := range noSuchSlotTests {
		t.Run(tt.name, func(t *testing.T) {
			rp := newTestRecordPage(t)
			for _, slot := range tt.inUse {
				if err := rp.setSlotState(slot, slotInUse); err != nil {
					t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
				}
			}

			if _, err := rp.ClaimFreeSlotAfter(tt.start); !errors.Is(err, ErrNoSuchSlot) {
				t.Errorf("ClaimFreeSlotAfter(%d) error = %v, want %v", tt.start, err, ErrNoSuchSlot)
			}
		})
	}
}

func TestRecordPageInitializeNewBlock(t *testing.T) {
	// A block that has just been appended is already all zeroes, so the block
	// is filled in first: an initialize that did nothing at all would pass
	// otherwise.
	t.Run("given a block whose slots all hold records, when it is initialized, then every slot is free and every field its zero value", func(t *testing.T) {
		rp := newTestRecordPage(t)

		for slot := range testSlotsInBlock {
			if err := rp.setSlotState(slot, slotInUse); err != nil {
				t.Fatalf("setSlotState(%d, slotInUse) error = %v", slot, err)
			}
			if err := rp.SetInt(slot, "id", int32(slot)+1); err != nil {
				t.Fatalf("SetInt(%d) error = %v", slot, err)
			}
			if err := rp.SetString(slot, "name", "written"); err != nil {
				t.Fatalf("SetString(%d) error = %v", slot, err)
			}
		}

		if err := rp.InitializeNewBlock(); err != nil {
			t.Fatalf("InitializeNewBlock() error = %v", err)
		}

		for slot := range testSlotsInBlock {
			if state := readSlotState(t, rp, slot); state != slotEmpty {
				t.Errorf("slot %d flag = %d, want %d", slot, state, slotEmpty)
			}

			gotID, err := rp.GetInt(slot, "id")
			if err != nil {
				t.Fatalf("GetInt(%d) error = %v", slot, err)
			}
			if gotID != 0 {
				t.Errorf("GetInt(%d) = %d, want 0", slot, gotID)
			}

			gotName, err := rp.GetString(slot, "name")
			if err != nil {
				t.Fatalf("GetString(%d) error = %v", slot, err)
			}
			if gotName != "" {
				t.Errorf("GetString(%d) = %q, want \"\"", slot, gotName)
			}
		}
	})

	// The block holds four whole slots and 32 bytes over, which belong to no
	// slot. A marker is left in them to catch an initialize that runs past the
	// last one.
	t.Run("it leaves the bytes past the last whole slot alone, since no slot owns them", func(t *testing.T) {
		const marker = 12345

		rp := newTestRecordPage(t)

		past := testSlotsInBlock * testSlotSize
		if err := rp.tx.SetInt(rp.blk, past, marker); err != nil {
			t.Fatalf("SetInt() past the last slot error = %v", err)
		}

		if err := rp.InitializeNewBlock(); err != nil {
			t.Fatalf("InitializeNewBlock() error = %v", err)
		}

		got, err := rp.tx.GetInt(rp.blk, past)
		if err != nil {
			t.Fatalf("GetInt() past the last slot error = %v", err)
		}
		if got != marker {
			t.Errorf("the bytes past the last slot = %d, want %d", got, marker)
		}
	})
}
