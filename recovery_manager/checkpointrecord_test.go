package recoverymanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
)

var _ LogRecord = (*CheckpointRecord)(nil)

func TestNewCheckpointRecord(t *testing.T) {
	rec := NewCheckpointRecord()

	if got := rec.Op(); got != Checkpoint {
		t.Errorf("Op() = %d, want %d (Checkpoint)", got, Checkpoint)
	}
	// A checkpoint belongs to no transaction, so it reports the same "no
	// transaction" marker that Buffer uses for an unassigned buffer.
	if got := rec.TxNumber(); got != noTxNum {
		t.Errorf("TxNumber() = %d, want %d", got, noTxNum)
	}
}

func TestCheckpointRecordString(t *testing.T) {
	rec := NewCheckpointRecord()

	const want = "<CHECKPOINT>"
	if got := rec.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestWriteCheckpointRecordToLog(t *testing.T) {
	tests := []struct {
		name    string
		writes  int
		wantLSN int
	}{
		{
			name:    "returns LSN 1 for the first record appended to an empty log",
			writes:  1,
			wantLSN: 1,
		},
		{
			name:    "returns an increasing LSN for each appended record",
			writes:  3,
			wantLSN: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lm := newTestLogManager(t)

			var lsn int
			var err error
			for i := 0; i < tt.writes; i++ {
				lsn, err = WriteCheckpointRecordToLog(lm)
				if err != nil {
					t.Fatalf("WriteCheckpointRecordToLog() error = %v", err)
				}
			}

			if lsn != tt.wantLSN {
				t.Errorf("WriteCheckpointRecordToLog() = %d, want %d", lsn, tt.wantLSN)
			}

			it, err := lm.Iterator()
			if err != nil {
				t.Fatalf("Iterator() error = %v", err)
			}
			if !it.HasNext() {
				t.Fatal("HasNext() = false, want true")
			}
			bytes, err := it.Next()
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}

			// A checkpoint record carries no txNum, so only the op code is
			// written and the record is one int wide.
			if len(bytes) != checkpointRecordSize {
				t.Errorf("record size = %d, want %d", len(bytes), checkpointRecordSize)
			}
			p := filemanager.NewPageByBytes(bytes)
			if got := Op(p.GetInt(opOffset)); got != Checkpoint {
				t.Errorf("op in log = %d, want %d (Checkpoint)", got, Checkpoint)
			}
		})
	}
}
