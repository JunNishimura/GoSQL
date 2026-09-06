package recoverymanager

import "testing"

var _ LogRecord = (*RollbackRecord)(nil)

func TestNewRollbackRecord(t *testing.T) {
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
			rec, err := NewRollbackRecord(newTxRecordBytes(Rollback, tt.txNum))
			if err != nil {
				t.Fatalf("NewRollbackRecord() error = %v", err)
			}

			if got := rec.TxNumber(); got != tt.wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, tt.wantTxNum)
			}
			if got := rec.Op(); got != Rollback {
				t.Errorf("Op() = %d, want %d (Rollback)", got, Rollback)
			}
		})
	}
}

func TestRollbackRecordString(t *testing.T) {
	tests := []struct {
		name  string
		txNum int32
		want  string
	}{
		{
			name:  "it reads as ROLLBACK alongside the transaction number",
			txNum: 1,
			want:  "<ROLLBACK 1>",
		},
		{
			name:  "given a transaction number of more than one digit, it shows the whole of it",
			txNum: 42,
			want:  "<ROLLBACK 42>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := NewRollbackRecord(newTxRecordBytes(Rollback, tt.txNum))
			if err != nil {
				t.Fatalf("NewRollbackRecord() error = %v", err)
			}

			if got := rec.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteRollbackRecordToLog(t *testing.T) {
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
				lsn, err = WriteRollbackRecordToLog(lm, txNum)
				if err != nil {
					t.Fatalf("WriteRollbackRecordToLog() error = %v", err)
				}
			}

			if lsn != tt.wantLSN {
				t.Errorf("WriteRollbackRecordToLog() = %d, want %d", lsn, tt.wantLSN)
			}

			// The most recent record must be readable back as an equivalent
			// RollbackRecord, since recovery reads the log backwards.
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

			rec, err := NewRollbackRecord(bytes)
			if err != nil {
				t.Fatalf("NewRollbackRecord() error = %v", err)
			}
			wantTxNum := tt.txNums[len(tt.txNums)-1]
			if got := rec.TxNumber(); got != wantTxNum {
				t.Errorf("TxNumber() = %d, want %d", got, wantTxNum)
			}
			if got := rec.Op(); got != Rollback {
				t.Errorf("Op() = %d, want %d (Rollback)", got, Rollback)
			}
		})
	}
}
