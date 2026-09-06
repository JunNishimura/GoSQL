package recoverymanager

import (
	"bytes"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// newSetIntRecordBytes builds the on-disk bytes of a set int record without
// going through the log manager. It uses the production encoder, whose output
// TestSetIntRecordLayout pins to an explicit byte sequence.
func newSetIntRecordBytes(t *testing.T, txNum int, blk *filemanager.BlockId, offset int, oldVal int32) []byte {
	t.Helper()
	record, err := newSetIntRecord(txNum, blk, offset, oldVal)
	if err != nil {
		t.Fatalf("newSetIntRecord() error = %v", err)
	}
	return record
}

// The layout is written to disk, so it is pinned to explicit bytes rather than
// to the offsets the encoder itself computes. A mistake made symmetrically in
// the encoder and the parser would otherwise go unnoticed.
func TestSetIntRecordLayout(t *testing.T) {
	t.Run("the bytes on the log read as the op, the transaction, the file name, the block, the offset and the old value, in that order", func(t *testing.T) {
		record := newSetIntRecordBytes(t, 1, filemanager.NewBlockId("test.tbl", 2), 80, 99)

		want := []byte{
			0, 0, 0, 4, // op: SetInt
			0, 0, 0, 1, // txNum
			0, 0, 0, 8, // length of the file name
			't', 'e', 's', 't', '.', 't', 'b', 'l',
			0, 0, 0, 2, // block number
			0, 0, 0, 80, // offset of the value within the block
			0, 0, 0, 99, // the value that was overwritten
		}

		if !bytes.Equal(record, want) {
			t.Errorf("record = %v, want %v", record, want)
		}
	})
}

func TestNewSetIntRecord(t *testing.T) {
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

			rec, err := NewSetIntRecord(newSetIntRecordBytes(t, tt.txNum, blk, 80, 99))
			if err != nil {
				t.Fatalf("NewSetIntRecord() error = %v", err)
			}

			if got := rec.Op(); got != SetInt {
				t.Errorf("Op() = %d, want %d (SetInt)", got, SetInt)
			}
			if got := rec.TxNumber(); got != tt.txNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.txNum)
			}
			if got := rec.Block(); !got.Equals(blk) {
				t.Errorf("Block() = %v, want %v", got, blk)
			}
		})
	}

	// The file name is stored inline, so how much of the record has to be
	// present depends on the record itself. Every prefix short of the whole
	// thing must be rejected rather than read past the end of the slice.
	full := newSetIntRecordBytes(t, 1, filemanager.NewBlockId("test.tbl", 2), 80, 99)

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
			name: "given a record missing the last byte of the old value, it refuses it",
			size: len(full) - 1,
		},
	}

	for _, tt := range shortRecordTests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := NewSetIntRecord(full[:tt.size])

			if rec != nil {
				t.Errorf("NewSetIntRecord() = %v, want nil", rec)
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
func TestSetIntRecordUndo(t *testing.T) {
	tests := []struct {
		name     string
		offset   int
		oldVal   int32
		overwith int32
	}{
		{
			name:     "given a record for the start of the block, the old value is put back there",
			offset:   0,
			oldVal:   99,
			overwith: 1234,
		},
		{
			name:     "given a record for an offset further into the block, the old value is put back there",
			offset:   80,
			oldVal:   -5,
			overwith: 1234,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := filemanager.NewBlockId("test.tbl", 2)
			rec, err := NewSetIntRecord(newSetIntRecordBytes(t, 1, blk, tt.offset, tt.oldVal))
			if err != nil {
				t.Fatalf("NewSetIntRecord() error = %v", err)
			}

			// Stand in for the block as it looks after the change the record
			// describes: the offset holds the new value, not the old one.
			p := filemanager.NewPageByBlockSize(testBlockSize)
			if err := p.SetInt(tt.offset, tt.overwith); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}

			if err := rec.Undo(p); err != nil {
				t.Fatalf("Undo() error = %v", err)
			}

			if got := p.GetInt(tt.offset); got != tt.oldVal {
				t.Errorf("GetInt(%d) = %d, want %d", tt.offset, got, tt.oldVal)
			}
		})
	}
}

func TestSetIntRecordString(t *testing.T) {
	tests := []struct {
		name   string
		txNum  int
		offset int
		oldVal int32
		want   string
	}{
		{
			name:   "it reads as SETINT alongside the transaction, block, offset and old value",
			txNum:  1,
			offset: 80,
			oldVal: 99,
			want:   "<SETINT 1 [file test.tbl, block 2] 80 99>",
		},
		{
			name:   "given a negative old value, the sign is shown rather than lost",
			txNum:  42,
			offset: 0,
			oldVal: -5,
			want:   "<SETINT 42 [file test.tbl, block 2] 0 -5>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blk := filemanager.NewBlockId("test.tbl", 2)
			rec, err := NewSetIntRecord(newSetIntRecordBytes(t, tt.txNum, blk, tt.offset, tt.oldVal))
			if err != nil {
				t.Fatalf("NewSetIntRecord() error = %v", err)
			}

			if got := rec.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteSetIntRecordToLog(t *testing.T) {
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
				lsn, err = WriteSetIntRecordToLog(lm, txNum, blk, 80, 99)
				if err != nil {
					t.Fatalf("WriteSetIntRecordToLog() error = %v", err)
				}
			}

			if lsn != tt.wantLSN {
				t.Errorf("WriteSetIntRecordToLog() = %d, want %d", lsn, tt.wantLSN)
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

			rec, err := NewSetIntRecord(record)
			if err != nil {
				t.Fatalf("NewSetIntRecord() error = %v", err)
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
