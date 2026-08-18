package recoverymanager

import "testing"

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
