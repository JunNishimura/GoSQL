package recordmanager

import "testing"

func TestNewRecordID(t *testing.T) {
	tests := []struct {
		name   string
		blkNum int
		slot   int
	}{
		{
			name:   "keeps the block number and the slot it is given",
			blkNum: 3,
			slot:   2,
		},
		{
			name:   "keeps the first slot of the first block",
			blkNum: 0,
			slot:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRecordID(tt.blkNum, tt.slot)

			if r.blkNum != tt.blkNum {
				t.Errorf("blkNum = %d, want %d", r.blkNum, tt.blkNum)
			}
			if r.slot != tt.slot {
				t.Errorf("slot = %d, want %d", r.slot, tt.slot)
			}
		})
	}
}

func TestRecordIDEquals(t *testing.T) {
	tests := []struct {
		name  string
		other *RecordID
		want  bool
	}{
		{
			name:  "returns true for the same block and slot",
			other: NewRecordID(3, 2),
			want:  true,
		},
		{
			name:  "returns false when the block number differs",
			other: NewRecordID(4, 2),
			want:  false,
		},
		{
			name:  "returns false when the slot differs",
			other: NewRecordID(3, 1),
			want:  false,
		},
		{
			name:  "returns false when the block number and the slot are swapped",
			other: NewRecordID(2, 3),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRecordID(3, 2)

			if got := r.Equals(tt.other); got != tt.want {
				t.Errorf("Equals(%s) = %t, want %t", tt.other, got, tt.want)
			}
		})
	}
}

func TestRecordIDString(t *testing.T) {
	tests := []struct {
		name   string
		blkNum int
		slot   int
		want   string
	}{
		{
			name:   "formats the block number and the slot",
			blkNum: 3,
			slot:   2,
			want:   "[block 3, slot 2]",
		},
		{
			name:   "formats the first slot of the first block",
			blkNum: 0,
			slot:   0,
			want:   "[block 0, slot 0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewRecordID(tt.blkNum, tt.slot).String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
