package recoverymanager

import (
	"errors"
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// newTxRecordPage builds the log representation shared by the record types
// whose only payload is a transaction number: the op code, then the txNum.
func newTxRecordPage(t *testing.T, op Op, txNum int32) *filemanager.Page {
	t.Helper()
	p := filemanager.NewPageByBlockSize(txRecordSize)
	if err := p.SetInt(opOffset, int32(op)); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	if err := p.SetInt(txNumOffset, txNum); err != nil {
		t.Fatalf("SetInt() error = %v", err)
	}
	return p
}

// Op codes are written into the log file, so their numeric values are part of
// the on-disk format and must not be reordered once logs exist.
func TestOpValues(t *testing.T) {
	tests := []struct {
		name string
		op   Op
		want int
	}{
		{
			name: "Checkpoint is encoded as 0",
			op:   Checkpoint,
			want: 0,
		},
		{
			name: "Start is encoded as 1",
			op:   Start,
			want: 1,
		},
		{
			name: "Commit is encoded as 2",
			op:   Commit,
			want: 2,
		},
		{
			name: "Rollback is encoded as 3",
			op:   Rollback,
			want: 3,
		},
		{
			name: "SetInt is encoded as 4",
			op:   SetInt,
			want: 4,
		},
		{
			name: "SetString is encoded as 5",
			op:   SetString,
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := int(tt.op); got != tt.want {
				t.Errorf("Op = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCreateLogRecord(t *testing.T) {
	tests := []struct {
		name      string
		record    []byte
		wantOp    Op
		wantTxNum int
		wantType  string
	}{
		{
			name:      "builds a CheckpointRecord from a checkpoint op",
			record:    newCheckpointRecordBytes(),
			wantOp:    Checkpoint,
			wantTxNum: noTxNum,
			wantType:  "<CHECKPOINT>",
		},
		{
			name:      "builds a StartRecord from a start op",
			record:    newTxRecordBytes(Start, 1),
			wantOp:    Start,
			wantTxNum: 1,
			wantType:  "<START 1>",
		},
		{
			name:      "builds a CommitRecord from a commit op",
			record:    newTxRecordBytes(Commit, 2),
			wantOp:    Commit,
			wantTxNum: 2,
			wantType:  "<COMMIT 2>",
		},
		{
			name:      "builds a RollbackRecord from a rollback op",
			record:    newTxRecordBytes(Rollback, 3),
			wantOp:    Rollback,
			wantTxNum: 3,
			wantType:  "<ROLLBACK 3>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := CreateLogRecord(tt.record)
			if err != nil {
				t.Fatalf("CreateLogRecord() error = %v, want nil", err)
			}

			if got := rec.Op(); got != tt.wantOp {
				t.Errorf("Op() = %d, want %d", got, tt.wantOp)
			}
			if got := rec.TxNumber(); got != tt.wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.wantTxNum)
			}
			// String identifies which concrete type was built, which Op alone
			// cannot do if the factory returned the wrong record.
			if got, ok := rec.(interface{ String() string }); !ok {
				t.Errorf("record does not implement String()")
			} else if s := got.String(); s != tt.wantType {
				t.Errorf("String() = %q, want %q", s, tt.wantType)
			}
		})
	}
}

func TestCreateLogRecordUnimplementedOp(t *testing.T) {
	// SetInt and SetString have op codes reserved but no record type yet.
	// They must be distinguishable from a corrupted log.
	tests := []struct {
		name string
		op   Op
	}{
		{
			name: "reports SetInt as unimplemented",
			op:   SetInt,
		},
		{
			name: "reports SetString as unimplemented",
			op:   SetString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := CreateLogRecord(newTxRecordBytes(tt.op, 1))

			if rec != nil {
				t.Errorf("CreateLogRecord() = %v, want nil", rec)
			}
			if !errors.Is(err, ErrUnimplementedRecord) {
				t.Errorf("error = %v, want it to wrap ErrUnimplementedRecord", err)
			}
		})
	}
}

func TestCreateLogRecordUnknownOp(t *testing.T) {
	tests := []struct {
		name string
		op   Op
	}{
		{
			name: "rejects an op code past the last defined one",
			op:   SetString + 1,
		},
		{
			name: "rejects a negative op code",
			op:   -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := CreateLogRecord(newTxRecordBytes(tt.op, 1))

			if rec != nil {
				t.Errorf("CreateLogRecord() = %v, want nil", rec)
			}
			if err == nil {
				t.Fatal("error = nil, want an error")
			}
			if errors.Is(err, ErrUnimplementedRecord) {
				t.Errorf("error = %v, want it not to wrap ErrUnimplementedRecord", err)
			}
		})
	}
}

func TestCreateLogRecordShortRecord(t *testing.T) {
	tests := []struct {
		name   string
		record []byte
	}{
		{
			name:   "rejects an empty record",
			record: []byte{},
		},
		{
			name:   "rejects a record too short to hold an op code",
			record: make([]byte, filemanager.IntBytes-1),
		},
		{
			name:   "rejects a start record without its txNum",
			record: newCheckpointRecordBytes(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The third case carries a valid checkpoint op, so overwrite it with
			// an op whose layout demands a txNum that is not there.
			record := tt.record
			if len(record) == checkpointRecordSize {
				p := filemanager.NewPageByBytes(record)
				if err := p.SetInt(opOffset, int32(Start)); err != nil {
					t.Fatalf("SetInt() error = %v", err)
				}
			}

			rec, err := CreateLogRecord(record)

			if rec != nil {
				t.Errorf("CreateLogRecord() = %v, want nil", rec)
			}
			if err == nil {
				t.Fatal("error = nil, want an error")
			}
		})
	}
}

// newTxRecordBytes builds the on-disk bytes of a record whose payload is a
// transaction number, without going through the log manager.
func newTxRecordBytes(op Op, txNum int32) []byte {
	record := make([]byte, txRecordSize)
	p := filemanager.NewPageByBytes(record)
	_ = p.SetInt(opOffset, int32(op))
	_ = p.SetInt(txNumOffset, txNum)
	return record
}

// newCheckpointRecordBytes builds the on-disk bytes of a checkpoint record.
func newCheckpointRecordBytes() []byte {
	record := make([]byte, checkpointRecordSize)
	p := filemanager.NewPageByBytes(record)
	_ = p.SetInt(opOffset, int32(Checkpoint))
	return record
}
