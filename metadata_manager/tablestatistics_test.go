package metadatamanager

import "testing"

func TestNewTableStatistics(t *testing.T) {
	t.Run("it carries the block and record counts it was made from", func(t *testing.T) {
		stats := NewTableStatistics(7, 100)

		if stats.numBlocks != 7 {
			t.Errorf("numBlocks = %d, want 7", stats.numBlocks)
		}
		if stats.numRecords != 100 {
			t.Errorf("numRecords = %d, want 100", stats.numRecords)
		}
	})
}

func TestTableStatisticsBlocksAccessed(t *testing.T) {
	t.Run("given a table of seven blocks, when the blocks a scan of it would touch are asked for, then it is those seven", func(t *testing.T) {
		stats := NewTableStatistics(7, 100)

		if got := stats.BlocksAccessed(); got != 7 {
			t.Errorf("BlocksAccessed() = %d, want 7", got)
		}
	})
}

func TestTableStatisticsRecordsOutput(t *testing.T) {
	t.Run("given a table of a hundred records, when the records a scan of it would give back are asked for, then it is those hundred", func(t *testing.T) {
		stats := NewTableStatistics(7, 100)

		if got := stats.RecordsOutput(); got != 100 {
			t.Errorf("RecordsOutput() = %d, want 100", got)
		}
	})
}

// The count is a guess made from the number of records alone, on the reasoning
// that a field of a table tends to repeat each of its values a few times. It
// never reaches zero, because a field of a table with no records still has the
// one value a query could ask for and find nothing under.
func TestTableStatisticsDistinctValues(t *testing.T) {
	tests := []struct {
		name       string
		numRecords int
		want       int
	}{
		{
			name:       "given a table with no records, when a field's distinct values are asked for, then the guess is one",
			numRecords: 0,
			want:       1,
		},
		{
			name:       "given a table with fewer records than the guess divides by, when a field's distinct values are asked for, then the guess is still one",
			numRecords: 2,
			want:       1,
		},
		{
			name:       "given a table with as many records as the guess divides by, when a field's distinct values are asked for, then the guess goes up to two",
			numRecords: 3,
			want:       2,
		},
		{
			name:       "given a table with a hundred records, when a field's distinct values are asked for, then the guess is a third of them and one more",
			numRecords: 100,
			want:       34,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := NewTableStatistics(7, tt.numRecords)

			if got := stats.DistinctValues("id"); got != tt.want {
				t.Errorf("DistinctValues() = %d, want %d", got, tt.want)
			}
		})
	}

	// The field is named in the call but not looked at, and this holds that
	// down rather than leaving it to be found. A caller reading the guess for
	// two fields of one table gets the same number twice, however unlike the
	// two fields are, and a caller that wants better has to give this layer
	// something it does not have today: what is actually in the field.
	t.Run("given two fields of one table, when each one's distinct values are asked for, then the guess is the same for both", func(t *testing.T) {
		stats := NewTableStatistics(7, 100)

		id := stats.DistinctValues("id")
		name := stats.DistinctValues("name")

		if id != name {
			t.Errorf("DistinctValues(\"id\") = %d and DistinctValues(\"name\") = %d, want the same guess for both", id, name)
		}
	})
}
