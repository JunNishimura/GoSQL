package index

import (
	"slices"
	"testing"

	"github.com/JunNishimura/GoSQL/query"
	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

func TestHashIndexMoveToNextMatch(t *testing.T) {
	tests := []struct {
		name      string
		entries   []entry
		searchKey query.Constant
		want      []string
	}{
		{
			name: "given entries filed under 7 and under 8, when 7 is searched for, then the record ids filed under 7 come back in the order they were filed",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(8), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(1, 2)},
			},
			searchKey: query.NewIntConstant(7),
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(1, 2).String(),
			},
		},
		{
			name: "given entries filed under 7 and under 107, which share a bucket, when 7 is searched for, then only the record ids filed under 7 come back",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(7 + numBuckets), rid: recordmanager.NewRecordID(0, 1)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 2)},
			},
			searchKey: query.NewIntConstant(7),
			want: []string{
				recordmanager.NewRecordID(0, 0).String(),
				recordmanager.NewRecordID(0, 2).String(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewHashIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
			defer idx.Close()

			insertEntries(t, idx, tt.entries)

			if got := searchRecordIDs(t, idx, tt.searchKey); !slices.Equal(got, tt.want) {
				t.Errorf("record ids filed under %s = %v, want %v", tt.searchKey, got, tt.want)
			}
		})
	}
}

func TestBucketOf(t *testing.T) {
	tests := []struct {
		name  string
		value query.Constant
		want  int
	}{
		{
			name:  "when an int smaller than the number of buckets is bucketed, then it goes in the bucket of that number",
			value: query.NewIntConstant(7),
			want:  7,
		},
		{
			name:  "when an int the number of buckets past another is bucketed, then it goes in the same bucket as that one",
			value: query.NewIntConstant(7 + numBuckets),
			want:  7,
		},
		{
			name:  "when a negative int is bucketed, then it goes in the bucket counted back from the last",
			value: query.NewIntConstant(-1),
			want:  numBuckets - 1,
		},
		{
			// The number is written out rather than worked out from hash/fnv, so
			// that the case fails if the hash ever changes. Buckets are files on
			// disk, and an index written under one hash cannot be read under
			// another.
			name:  "when a string is bucketed, then it goes in the bucket of its 32-bit FNV-1a hash",
			value: query.NewStringConstant("abc"),
			want:  31,
		},
		{
			name:  "when another string is bucketed, then it goes in the bucket of its own 32-bit FNV-1a hash",
			value: query.NewStringConstant("alice"),
			want:  79,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bucketOf(tt.value); got != tt.want {
				t.Errorf("bucketOf(%s) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
