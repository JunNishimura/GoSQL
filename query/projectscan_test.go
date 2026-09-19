package query

import (
	"errors"
	"slices"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
	"github.com/JunNishimura/GoSQL/transaction"
)

func TestNewProjectScan(t *testing.T) {
	// A scan that named a field it could not deliver would have to answer
	// HasField with one thing and the read of that field with another. Settling
	// it here is what lets HasField be trusted everywhere else.
	t.Run("given a field the scan underneath does not have, when a projection over it is opened, then it reports ErrFieldNotFound", func(t *testing.T) {
		_, err := NewProjectScan(newTestTableOfRecords(t, 10), "id", "missing")

		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("NewProjectScan() error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}
	})
}

// newTestProjectionOnARecord opens a projection keeping the named fields over a
// table of one record, id 20, and moves it onto that record.
func newTestProjectionOnARecord(t *testing.T, fieldNames ...string) *ProjectScan {
	t.Helper()

	ps, err := NewProjectScan(newTestTableOfRecords(t, 20), fieldNames...)
	if err != nil {
		t.Fatalf("NewProjectScan(%v) error = %v", fieldNames, err)
	}

	onRecord, err := ps.MoveToNextRecord()
	if err != nil {
		t.Fatalf("MoveToNextRecord() error = %v", err)
	}
	if !onRecord {
		t.Fatal("the projection returned no records, want the one the table holds")
	}

	return ps
}

// A projection is the one scan whose fields are not the fields of what it reads
// through, so what it answers here is its own list rather than a question
// passed down.
func TestProjectScanHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "given a field the projection keeps, when it is asked for, then it reports the field is there",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a field the table underneath has but the projection drops, when it is asked for, then it reports the field is not there",
			fieldName: "name",
			want:      false,
		},
		{
			name:      "given a field no table in the query has, when it is asked for, then it reports the field is not there",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newTestProjectionOnARecord(t, "id")

			if got := ps.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestProjectScanGetInt(t *testing.T) {
	t.Run("given a field the projection keeps, when it is read, then it is what the record underneath holds", func(t *testing.T) {
		got, err := newTestProjectionOnARecord(t, "id").GetInt("id")
		if err != nil {
			t.Fatalf("GetInt() error = %v", err)
		}
		if got != 20 {
			t.Errorf("GetInt(%q) = %d, want 20", "id", got)
		}
	})

	// The field is there in the record underneath, so a projection that passed
	// the name straight down would hand it over.
	t.Run("given a field the projection drops, when it is read, then it reports ErrFieldNotFound rather than reading it from underneath", func(t *testing.T) {
		ps := newTestProjectionOnARecord(t, "name")

		if _, err := ps.GetInt("id"); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("GetInt(%q) error = %v, want %v", "id", err, recordmanager.ErrFieldNotFound)
		}
	})
}

func TestProjectScanGetString(t *testing.T) {
	t.Run("given a field the projection keeps, when it is read, then it is what the record underneath holds", func(t *testing.T) {
		got, err := newTestProjectionOnARecord(t, "name").GetString("name")
		if err != nil {
			t.Fatalf("GetString() error = %v", err)
		}
		if want := testNameOf(20); got != want {
			t.Errorf("GetString(%q) = %q, want %q", "name", got, want)
		}
	})

	t.Run("given a field the projection drops, when it is read, then it reports ErrFieldNotFound rather than reading it from underneath", func(t *testing.T) {
		ps := newTestProjectionOnARecord(t, "id")

		if _, err := ps.GetString("name"); !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("GetString(%q) error = %v, want %v", "name", err, recordmanager.ErrFieldNotFound)
		}
	})
}

func TestProjectScanGetValue(t *testing.T) {
	tests := []struct {
		name       string
		keptFields []string
		fieldName  string
		want       Constant
		wantErr    error
	}{
		{
			name:       "given an int field the projection keeps, when it is read as a value, then it is that field as an int constant",
			keptFields: []string{"id", "name"},
			fieldName:  "id",
			want:       NewIntConstant(20),
		},
		{
			name:       "given a varchar field the projection keeps, when it is read as a value, then it is that field as a varchar constant",
			keptFields: []string{"id", "name"},
			fieldName:  "name",
			want:       NewStringConstant(testNameOf(20)),
		},
		{
			name:       "given a field the projection drops, when it is read as a value, then it reports ErrFieldNotFound",
			keptFields: []string{"id"},
			fieldName:  "name",
			wantErr:    recordmanager.ErrFieldNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := newTestProjectionOnARecord(t, tt.keptFields...)

			got, err := ps.GetValue(tt.fieldName)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("GetValue(%q) error = %v, want %v", tt.fieldName, err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("GetValue(%q) error = %v", tt.fieldName, err)
			}
			if got != tt.want {
				t.Errorf("GetValue(%q) = %s, want %s", tt.fieldName, got, tt.want)
			}
		})
	}
}

// A projection drops fields and keeps every record, which is the other half of
// what a select does.
func TestProjectScanMoveToNextRecord(t *testing.T) {
	t.Run("given a table of records, when the projection is walked, then it returns every one of them in order", func(t *testing.T) {
		ps, err := NewProjectScan(newTestTableOfRecords(t, 10, 20, 30), "id")
		if err != nil {
			t.Fatalf("NewProjectScan() error = %v", err)
		}

		want := []int32{10, 20, 30}
		if got := walkTestRecords(t, ps); !slices.Equal(got, want) {
			t.Errorf("the ids read = %v, want %v", got, want)
		}
	})
}

func TestProjectScanMoveBeforeFirstRecord(t *testing.T) {
	t.Run("given a projection that has been read to the end, when it is put back to the start, then it returns the same records again", func(t *testing.T) {
		ps, err := NewProjectScan(newTestTableOfRecords(t, 10, 20), "id")
		if err != nil {
			t.Fatalf("NewProjectScan() error = %v", err)
		}

		want := []int32{10, 20}
		if got := walkTestRecords(t, ps); !slices.Equal(got, want) {
			t.Fatalf("the ids read = %v, want %v", got, want)
		}

		if err := ps.MoveBeforeFirstRecord(); err != nil {
			t.Fatalf("MoveBeforeFirstRecord() error = %v", err)
		}

		if got := walkTestRecords(t, ps); !slices.Equal(got, want) {
			t.Errorf("the ids read the second time = %v, want %v", got, want)
		}
	})
}

func TestProjectScanClose(t *testing.T) {
	t.Run("when a projection is closed, then the scan it reads through is closed too, so the block that one held is given back", func(t *testing.T) {
		ts := newTestTableOfRecords(t, 10)

		held := ts.rp.BlockID()

		ps, err := NewProjectScan(ts, "id")
		if err != nil {
			t.Fatalf("NewProjectScan() error = %v", err)
		}
		ps.Close()

		if _, err := ts.tx.GetInt(held, 0); !errors.Is(err, transaction.ErrBlockNotPinned) {
			t.Errorf("GetInt() on the block the table scan held error = %v, want %v", err, transaction.ErrBlockNotPinned)
		}
	})
}
