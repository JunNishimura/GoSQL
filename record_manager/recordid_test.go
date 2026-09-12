package recordmanager

import "testing"

func TestNewRecordID(t *testing.T) {
	tests := []struct {
		name   string
		blkNum int
		slot   int
	}{
		{
			name:   "it carries the block number and the slot it was made from",
			blkNum: 3,
			slot:   2,
		},
		{
			name:   "given block zero and slot zero, it keeps them rather than reading them as unset",
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
			name:  "given two record ids of the same block, when their slots are the same, then they are equal",
			other: NewRecordID(3, 2),
			want:  true,
		},
		{
			name:  "given two record ids of the same slot, when their block numbers differ, then they are not equal",
			other: NewRecordID(4, 2),
			want:  false,
		},
		{
			name:  "given two record ids of the same block, when their slots differ, then they are not equal",
			other: NewRecordID(3, 1),
			want:  false,
		},
		{
			name:  "given a record id whose block number and slot are the other's the other way round, then they are not equal",
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

func TestRecordIDBlockNumber(t *testing.T) {
	tests := []struct {
		name   string
		blkNum int
		want   int
	}{
		{
			name:   "given a record id of block three, when its block is asked for, then it is that three",
			blkNum: 3,
			want:   3,
		},
		{
			name:   "given a record id of block zero, when its block is asked for, then it is that zero rather than nothing",
			blkNum: 0,
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewRecordID(tt.blkNum, 2).BlockNumber(); got != tt.want {
				t.Errorf("BlockNumber() = %d, want %d", got, tt.want)
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
			name:   "it formats as its block number and slot",
			blkNum: 3,
			slot:   2,
			want:   "[block 3, slot 2]",
		},
		{
			name:   "given block zero and slot zero, it shows the zeroes rather than leaving them out",
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
