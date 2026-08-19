package recoverymanager

import (
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
