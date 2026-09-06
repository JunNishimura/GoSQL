package recoverymanager

import (
	"testing"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

const (
	testBlockSize = 400
	testLogFile   = "test.log"
)

// CommitRecord must satisfy LogRecord so that the recovery manager can treat it
// uniformly with the other record types.
var _ LogRecord = (*CommitRecord)(nil)

func newTestLogManager(t *testing.T) *logmanager.LogManager {
	t.Helper()
	fm, err := filemanager.NewFileManager(t.TempDir(), testBlockSize)
	if err != nil {
		t.Fatalf("NewFileManager() error = %v", err)
	}
	lm, err := logmanager.NewLogManager(fm, testLogFile)
	if err != nil {
		t.Fatalf("NewLogManager() error = %v", err)
	}
	return lm
}

func TestNewCommitRecord(t *testing.T) {
	tests := []struct {
		name      string
		txNum     int32
		wantTxNum int
	}{
		{
			name:      "given transaction number zero, it reads the zero rather than taking it as unset",
			txNum:     0,
			wantTxNum: 0,
		},
		{
			name:      "it reads the transaction number from the bytes after the op code",
			txNum:     1,
			wantTxNum: 1,
		},
		{
			name:      "given a transaction number that fills more than one byte, it reads the whole of it",
			txNum:     123456,
			wantTxNum: 123456,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := NewCommitRecord(newTxRecordBytes(Commit, tt.txNum))
			if err != nil {
				t.Fatalf("NewCommitRecord() error = %v", err)
			}

			if got := rec.TxNumber(); got != tt.wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.wantTxNum)
			}
			if got := rec.Op(); got != Commit {
				t.Errorf("Op() = %d, want %d (Commit)", got, Commit)
			}
		})
	}
}

func TestCommitRecordString(t *testing.T) {
	tests := []struct {
		name  string
		txNum int32
		want  string
	}{
		{
			name:  "it reads as COMMIT alongside the transaction number",
			txNum: 1,
			want:  "<COMMIT 1>",
		},
		{
			name:  "given a transaction number of more than one digit, it shows the whole of it",
			txNum: 42,
			want:  "<COMMIT 42>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := NewCommitRecord(newTxRecordBytes(Commit, tt.txNum))
			if err != nil {
				t.Fatalf("NewCommitRecord() error = %v", err)
			}

			if got := rec.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteCommitRecordToLog(t *testing.T) {
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

			var lsn int
			var err error
			for _, txNum := range tt.txNums {
				lsn, err = WriteCommitRecordToLog(lm, txNum)
				if err != nil {
					t.Fatalf("WriteCommitRecordToLog() error = %v", err)
				}
			}

			if lsn != tt.wantLSN {
				t.Errorf("WriteCommitRecordToLog() = %d, want %d", lsn, tt.wantLSN)
			}

			// The most recent record must be readable back as an equivalent
			// CommitRecord, since recovery reads the log backwards.
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

			rec, err := NewCommitRecord(bytes)
			if err != nil {
				t.Fatalf("NewCommitRecord() error = %v", err)
			}
			wantTxNum := tt.txNums[len(tt.txNums)-1]
			if got := rec.TxNumber(); got != wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, wantTxNum)
			}
			if got := rec.Op(); got != Commit {
				t.Errorf("Op() = %d, want %d (Commit)", got, Commit)
			}
		})
	}
}
