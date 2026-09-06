package recoverymanager

import (
	"bytes"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// newSetStringRecordBytes builds the on-disk bytes of a set string record
// without going through the log manager. It uses the production encoder, whose
// output TestSetStringRecordLayout pins to an explicit byte sequence.
func newSetStringRecordBytes(t *testing.T, txNum int, blk *filemanager.BlockId, offset int, oldVal string) []byte {
	t.Helper()
	record, err := newSetStringRecord(txNum, blk, offset, oldVal)
	if err != nil {
		t.Fatalf("newSetStringRecord() error = %v", err)
	}
	return record
}

// Both the file name and the overwritten value are stored inline, so this
// record has two length-prefixed fields. The layout is pinned to explicit bytes
// so that a mistake made symmetrically in the encoder and the parser shows up.
func TestSetStringRecordLayout(t *testing.T) {
	t.Run("the bytes on the log read as the op, the transaction, the file name, the block, the offset and the old value, each inline field behind its length", func(t *testing.T) {
		record := newSetStringRecordBytes(t, 1, filemanager.NewBlockId("test.tbl", 2), 80, "hi")

		want := []byte{
			0, 0, 0, 5, // op: SetString
			0, 0, 0, 1, // txNum
			0, 0, 0, 8, // length of the file name
			't', 'e', 's', 't', '.', 't', 'b', 'l',
			0, 0, 0, 2, // block number
			0, 0, 0, 80, // offset of the value within the block
			0, 0, 0, 2, // length of the value that was overwritten
			'h', 'i',
		}

		if !bytes.Equal(record, want) {
			t.Errorf("record = %v, want %v", record, want)
		}
	})
}

func TestNewSetStringRecord(t *testing.T) {
	tests := []struct {
		name     string
		txNum    int
		fileName string
		blkNum   int
	}{
		{
			name:     "it reads back the transaction and the block the record was written for",
			txNum:    1,
			fileName: "test.tbl",
			blkNum:   2,
		},
		{
			name:     "given a file name of another length, the fields after it are still read from the right place",
			txNum:    42,
			fileName: "a.tbl",
			blkNum:   0,
		},
		{
			name:     "given a file name of multi-byte characters, it is read by its bytes rather than its characters",
			txNum:    7,
			fileName: "テーブル.tbl",
			blkNum:   13,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := filemanager.NewBlockId(tt.fileName, tt.blkNum)

			rec, err := NewSetStringRecord(newSetStringRecordBytes(t, tt.txNum, blk, 80, "hi"))
			if err != nil {
				t.Fatalf("NewSetStringRecord() error = %v", err)
			}

			if got := rec.Op(); got != SetString {
				t.Errorf("Op() = %d, want %d (SetString)", got, SetString)
			}
			if got := rec.TxNumber(); got != tt.txNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.txNum)
			}
			if got := rec.Block(); !got.Equals(blk) {
				t.Errorf("Block() = %v, want %v", got, blk)
			}
		})
	}

	// This record has two inline fields, so there is one more way for a
	// truncated record to look plausible than there is for a set int record:
	// the length of the value can claim more bytes than the record holds.
	full := newSetStringRecordBytes(t, 1, filemanager.NewBlockId("test.tbl", 2), 80, "hi")

	shortRecordTests := []struct {
		name string
		size int
	}{
		{
			name: "given no bytes at all, it refuses the record",
			size: 0,
		},
		{
			name: "given a record that stops before the file name length, it refuses it",
			size: 2 * filemanager.IntBytes,
		},
		{
			name: "given a record whose file name length claims more bytes than are there, it refuses it",
			size: 3 * filemanager.IntBytes,
		},
		{
			name: "given a record that stops after the file name, it refuses it",
			size: 5 * filemanager.IntBytes,
		},
		{
			name: "given a record that stops before the offset, it refuses it",
			size: 6 * filemanager.IntBytes,
		},
		{
			name: "given a record that stops before the value length, it refuses it",
			size: 7 * filemanager.IntBytes,
		},
		{
			name: "given a record whose value length claims more bytes than are there, it refuses it",
			size: 8 * filemanager.IntBytes,
		},
		{
			name: "given a record missing the last byte of the value, it refuses it",
			size: len(full) - 1,
		},
	}

	for _, tt := range shortRecordTests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := NewSetStringRecord(full[:tt.size])

			if rec != nil {
				t.Errorf("NewSetStringRecord() = %v, want nil", rec)
			}
			if err == nil {
				t.Fatal("error = nil, want an error")
			}
		})
	}
}

// Undo writes the pre-image into the page it is handed. The offset and the old
// value are not otherwise readable, so this is what verifies they survived the
// round trip through the log format.
func TestSetStringRecordUndo(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		oldVal   string
		overwith string
	}{
		{
			name:     "given a record for the start of the block, the old value is put back there",
			offset:   0,
			oldVal:   "hi",
			overwith: "bye",
		},
		{
			name:     "given an old value shorter than the one that replaced it, the length prefix is put back too",
			offset:   80,
			oldVal:   "a",
			overwith: "a much longer value",
		},
		{
			name:     "given an empty old value, the field is put back empty rather than left as it was",
			offset:   80,
			oldVal:   "",
			overwith: "something",
		},
		{
			name:     "given an old value of multi-byte characters, it is put back unchanged",
			offset:   80,
			oldVal:   "テーブル",
			overwith: "x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := filemanager.NewBlockId("test.tbl", 2)
			rec, err := NewSetStringRecord(newSetStringRecordBytes(t, 1, blk, tt.offset, tt.oldVal))
			if err != nil {
				t.Fatalf("NewSetStringRecord() error = %v", err)
			}

			// Stand in for the block as it looks after the change the record
			// describes: the offset holds the new value, not the old one.
			p := filemanager.NewPageByBlockSize(testBlockSize)
			if err := p.SetString(tt.offset, tt.overwith); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}

			if err := rec.Undo(p); err != nil {
				t.Fatalf("Undo() error = %v", err)
			}

			if got := p.GetString(tt.offset); got != tt.oldVal {
				t.Errorf("GetString(%d) = %q, want %q", tt.offset, got, tt.oldVal)
			}
		})
	}
}

func TestSetStringRecordString(t *testing.T) {
	tests := []struct {
		name   string
		txNum  int
		offset int
		oldVal string
		want   string
	}{
		{
			name:   "it reads as SETSTRING alongside the transaction, block, offset and old value",
			txNum:  1,
			offset: 80,
			oldVal: "hi",
			want:   `<SETSTRING 1 [file test.tbl, block 2] 80 "hi">`,
		},
		{
			// Quoting keeps an empty value from reading as a missing field.
			name:   "given an empty old value, the empty string is shown rather than left out",
			txNum:  42,
			offset: 0,
			oldVal: "",
			want:   `<SETSTRING 42 [file test.tbl, block 2] 0 "">`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := filemanager.NewBlockId("test.tbl", 2)
			rec, err := NewSetStringRecord(newSetStringRecordBytes(t, tt.txNum, blk, tt.offset, tt.oldVal))
			if err != nil {
				t.Fatalf("NewSetStringRecord() error = %v", err)
			}

			if got := rec.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteSetStringRecordToLog(t *testing.T) {
	tests := []struct {
		name    string
		txNums  []int
		wantLSN int
	}{
		{
			name:    "given a log with nothing on it, the record it writes gets lsn 1",
			txNums:  []int{1},
			wantLSN: 1,
		},
		{
			name:    "given records already on the log, each one it writes gets the next lsn",
			txNums:  []int{1, 2, 3},
			wantLSN: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lm := newTestLogManager(t)
			blk := filemanager.NewBlockId("test.tbl", 2)

			var lsn int
			var err error
			for _, txNum := range tt.txNums {
				lsn, err = WriteSetStringRecordToLog(lm, txNum, blk, 80, "hi")
				if err != nil {
					t.Fatalf("WriteSetStringRecordToLog() error = %v", err)
				}
			}

			if lsn != tt.wantLSN {
				t.Errorf("WriteSetStringRecordToLog() = %d, want %d", lsn, tt.wantLSN)
			}

			it, err := lm.Iterator()
			if err != nil {
				t.Fatalf("Iterator() error = %v", err)
			}
			if !it.HasNext() {
				t.Fatal("HasNext() = false, want true")
			}
			record, err := it.Next()
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}

			rec, err := NewSetStringRecord(record)
			if err != nil {
				t.Fatalf("NewSetStringRecord() error = %v", err)
			}
			wantTxNum := tt.txNums[len(tt.txNums)-1]
			if got := rec.TxNumber(); got != wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, wantTxNum)
			}
			if got := rec.Block(); !got.Equals(blk) {
				t.Errorf("Block() = %v, want %v", got, blk)
			}
		})
	}
}
