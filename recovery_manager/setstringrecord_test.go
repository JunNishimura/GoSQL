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
}

func TestNewSetStringRecord(t *testing.T) {
	tests := []struct {
		name     string
		txNum    int
		fileName string
		blkNum   int
	}{
		{
			name:     "reads a record written by transaction 1",
			txNum:    1,
			fileName: "test.tbl",
			blkNum:   2,
		},
		{
			name:     "reads a record naming a file whose name is a different length",
			txNum:    42,
			fileName: "a.tbl",
			blkNum:   0,
		},
		{
			name:     "reads a record naming a multi-byte file name",
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
			name:     "restores a value at the start of the block",
			offset:   0,
			oldVal:   "hi",
			overwith: "bye",
		},
		{
			name:     "restores a value shorter than the one that replaced it",
			offset:   80,
			oldVal:   "a",
			overwith: "a much longer value",
		},
		{
			name:     "restores an empty value",
			offset:   80,
			oldVal:   "",
			overwith: "something",
		},
		{
			name:     "restores a multi-byte value",
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
			name:   "formats the transaction, block, offset and old value",
			txNum:  1,
			offset: 80,
			oldVal: "hi",
			want:   `<SETSTRING 1 [file test.tbl, block 2] 80 "hi">`,
		},
		{
			// Quoting keeps an empty value from reading as a missing field.
			name:   "formats an empty old value",
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

// This record has two inline fields, so there is one more way for a truncated
// record to look plausible than there is for a set int record: the length of
// the value can claim more bytes than the record holds.
func TestNewSetStringRecordRejectsShortRecords(t *testing.T) {
	full := newSetStringRecordBytes(t, 1, filemanager.NewBlockId("test.tbl", 2), 80, "hi")

	tests := []struct {
		name string
		size int
	}{
		{
			name: "rejects an empty record",
			size: 0,
		},
		{
			name: "rejects a record that stops before the file name length",
			size: 2 * filemanager.IntBytes,
		},
		{
			name: "rejects a record whose file name length exceeds what is present",
			size: 3 * filemanager.IntBytes,
		},
		{
			name: "rejects a record that stops after the file name",
			size: 5 * filemanager.IntBytes,
		},
		{
			name: "rejects a record that stops before the offset",
			size: 6 * filemanager.IntBytes,
		},
		{
			name: "rejects a record that stops before the value length",
			size: 7 * filemanager.IntBytes,
		},
		{
			name: "rejects a record whose value length exceeds what is present",
			size: 8 * filemanager.IntBytes,
		},
		{
			name: "rejects a record missing the last byte of the value",
			size: len(full) - 1,
		},
	}

	for _, tt := range tests {
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

func TestWriteSetStringRecordToLog(t *testing.T) {
	tests := []struct {
		name    string
		txNums  []int
		wantLSN int
	}{
		{
			name:    "returns LSN 1 for the first record appended to an empty log",
			txNums:  []int{1},
			wantLSN: 1,
		},
		{
			name:    "returns an increasing LSN for each appended record",
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
