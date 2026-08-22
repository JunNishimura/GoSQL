package recoverymanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

// mustCreateLogRecord rebuilds a record that the test knows to be well formed.
func mustCreateLogRecord(t *testing.T, record []byte) LogRecord {
	t.Helper()
	rec, err := CreateLogRecord(record)
	if err != nil {
		t.Fatalf("CreateLogRecord() error = %v", err)
	}
	return rec
}

// asLogRecord adapts a constructor's concrete result to LogRecord. Forwarding
// the two values directly would put a typed nil pointer into the interface,
// which does not compare equal to nil and would hide a failed construction.
func asLogRecord[T LogRecord](rec T, err error) (LogRecord, error) {
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// Each constructor validates the bytes it is handed, so that a truncated record
// is reported rather than read past the end of the slice.
func TestRecordConstructorsRejectShortRecords(t *testing.T) {
	tests := []struct {
		name string
		new  func([]byte) (LogRecord, error)
	}{
		{
			name: "NewStartRecord rejects a record without its txNum",
			new:  func(b []byte) (LogRecord, error) { return asLogRecord(NewStartRecord(b)) },
		},
		{
			name: "NewCommitRecord rejects a record without its txNum",
			new:  func(b []byte) (LogRecord, error) { return asLogRecord(NewCommitRecord(b)) },
		},
		{
			name: "NewRollbackRecord rejects a record without its txNum",
			new:  func(b []byte) (LogRecord, error) { return asLogRecord(NewRollbackRecord(b)) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// One int wide: enough for the op code, but not for the txNum.
			rec, err := tt.new(make([]byte, filemanager.IntBytes))

			if rec != nil {
				t.Errorf("record = %v, want nil", rec)
			}
			if err == nil {
				t.Fatal("error = nil, want an error")
			}
		})
	}
}

// minimalRecord implements only what a record with no data to restore should
// have to. The assertion below stops LogRecord from growing a method that such
// a record could satisfy only with an empty body.
type minimalRecord struct{}

func (minimalRecord) Op() Op        { return Checkpoint }
func (minimalRecord) TxNumber() int { return noTxNum }

var _ LogRecord = minimalRecord{}

// Only the records that changed data implement Undoable. Recovery decides
// whether to restore a record by that distinction alone, so a boundary record
// that started implementing it would be undone by mistake.
func TestUndoableRecords(t *testing.T) {
	tests := []struct {
		name         string
		record       LogRecord
		wantUndoable bool
	}{
		{
			name:         "CheckpointRecord changed no data and is not undoable",
			record:       mustCreateLogRecord(t, newCheckpointRecordBytes()),
			wantUndoable: false,
		},
		{
			name:         "StartRecord changed no data and is not undoable",
			record:       mustCreateLogRecord(t, newTxRecordBytes(Start, 1)),
			wantUndoable: false,
		},
		{
			name:         "CommitRecord changed no data and is not undoable",
			record:       mustCreateLogRecord(t, newTxRecordBytes(Commit, 1)),
			wantUndoable: false,
		},
		{
			name:         "RollbackRecord changed no data and is not undoable",
			record:       mustCreateLogRecord(t, newTxRecordBytes(Rollback, 1)),
			wantUndoable: false,
		},
		{
			name:         "SetIntRecord overwrote a value and is undoable",
			record:       mustCreateLogRecord(t, newSetIntRecordBytes(t, 1, filemanager.NewBlockId(testDataFile, 2), 80, 99)),
			wantUndoable: true,
		},
		{
			name:         "SetStringRecord overwrote a value and is undoable",
			record:       mustCreateLogRecord(t, newSetStringRecordBytes(t, 1, filemanager.NewBlockId(testDataFile, 2), 80, "hi")),
			wantUndoable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := tt.record.(Undoable)
			if ok != tt.wantUndoable {
				t.Errorf("implements Undoable = %v, want %v", ok, tt.wantUndoable)
			}
		})
	}
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
		{
			name:      "builds a SetIntRecord from a set int op",
			record:    newSetIntRecordBytes(t, 4, filemanager.NewBlockId(testDataFile, 2), 80, 99),
			wantOp:    SetInt,
			wantTxNum: 4,
			wantType:  "<SETINT 4 [file test.tbl, block 2] 80 99>",
		},
		{
			name:      "builds a SetStringRecord from a set string op",
			record:    newSetStringRecordBytes(t, 5, filemanager.NewBlockId(testDataFile, 2), 80, "hi"),
			wantOp:    SetString,
			wantTxNum: 5,
			wantType:  `<SETSTRING 5 [file test.tbl, block 2] 80 "hi">`,
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
