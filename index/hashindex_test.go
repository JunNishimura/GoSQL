package index

import (
	"errors"
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

	t.Run("given a hash index that has not been searched, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := NewHashIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
		defer idx.Close()

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})

	t.Run("given a hash index that was searched and then closed, when it is moved, then it reports ErrNoSearch", func(t *testing.T) {
		idx := NewHashIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
		insertEntries(t, idx, []entry{
			{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
		})
		if err := idx.MoveBeforeFirstMatch(query.NewIntConstant(7)); err != nil {
			t.Fatalf("MoveBeforeFirstMatch(7) error = %v", err)
		}
		idx.Close()

		if _, err := idx.MoveToNextMatch(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("MoveToNextMatch() error = %v, want %v", err, ErrNoSearch)
		}
	})
}

func TestHashIndexDataRecordID(t *testing.T) {
	t.Run("given a hash index that has not been searched, when the record id is asked for, then it reports ErrNoSearch", func(t *testing.T) {
		idx := NewHashIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
		defer idx.Close()

		if _, err := idx.DataRecordID(); !errors.Is(err, ErrNoSearch) {
			t.Errorf("DataRecordID() error = %v, want %v", err, ErrNoSearch)
		}
	})
}

func TestHashIndexDelete(t *testing.T) {
	tests := []struct {
		name    string
		entries []entry
		deleted entry
		wantErr error
		// want is the record ids each value is searched for afterwards, and
		// should give back.
		want map[query.Constant][]string
	}{
		{
			name: "given two record ids filed under 7, when one of them is deleted, then the other is still filed under 7",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			want: map[query.Constant][]string{
				query.NewIntConstant(7): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
		{
			name: "given one record id filed under both 7 and 107, which share a bucket, when it is deleted from under 107, then it is filed under 7 and no longer under 107",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
				{value: query.NewIntConstant(7 + numBuckets), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(7 + numBuckets), rid: recordmanager.NewRecordID(0, 0)},
			want: map[query.Constant][]string{
				query.NewIntConstant(7):              {recordmanager.NewRecordID(0, 0).String()},
				query.NewIntConstant(7 + numBuckets): nil,
			},
		},
		{
			name: "given a record id filed under 7, when another record id is deleted from under 7, then it reports ErrEntryNotFound and the one filed is still there",
			entries: []entry{
				{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 1)},
			wantErr: ErrEntryNotFound,
			want: map[query.Constant][]string{
				query.NewIntConstant(7): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
		{
			name: "given a record id filed under 107, when it is deleted from under 7, which shares the bucket, then it reports ErrEntryNotFound and it is still filed under 107",
			entries: []entry{
				{value: query.NewIntConstant(7 + numBuckets), rid: recordmanager.NewRecordID(0, 0)},
			},
			deleted: entry{value: query.NewIntConstant(7), rid: recordmanager.NewRecordID(0, 0)},
			wantErr: ErrEntryNotFound,
			want: map[query.Constant][]string{
				query.NewIntConstant(7 + numBuckets): {recordmanager.NewRecordID(0, 0).String()},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := NewHashIndex(newTestTransaction(t), testIndexName, newTestIndexLayout(t))
			defer idx.Close()

			insertEntries(t, idx, tt.entries)

			if err := idx.Delete(tt.deleted.value, tt.deleted.rid); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Delete(%s, %s) error = %v, want %v", tt.deleted.value, tt.deleted.rid, err, tt.wantErr)
			}

			for searchKey, want := range tt.want {
				if got := searchRecordIDs(t, idx, searchKey); !slices.Equal(got, want) {
					t.Errorf("record ids filed under %s = %v, want %v", searchKey, got, want)
				}
			}
		})
	}
}

func TestHashIndexSearchCost(t *testing.T) {
	tests := []struct {
		name            string
		numBlocks       int
		recordsPerBlock int
		want            int
	}{
		{
			name:            "when an index whose blocks divide evenly among the buckets is costed, then a search reads one bucket's share of them",
			numBlocks:       10 * numBuckets,
			recordsPerBlock: 4,
			want:            10,
		},
		{
			name:            "when an index whose blocks do not divide evenly among the buckets is costed, then a search reads one bucket's share rounded up",
			numBlocks:       2*numBuckets + 50,
			recordsPerBlock: 4,
			want:            3,
		},
		{
			name:            "when an index of fewer blocks than buckets is costed, then a search reads one block",
			numBlocks:       numBuckets / 2,
			recordsPerBlock: 4,
			want:            1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HashIndexSearchCost(tt.numBlocks, tt.recordsPerBlock); got != tt.want {
				t.Errorf("HashIndexSearchCost(%d, %d) = %d, want %d", tt.numBlocks, tt.recordsPerBlock, got, tt.want)
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
