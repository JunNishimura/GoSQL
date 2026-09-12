package metadatamanager

import "testing"

// The index the tests below describe: one on the int field of the test table.
const (
	testIndexName      = "test_table_id_index"
	testIndexFieldName = "id"
)

func TestNewIndexInfo(t *testing.T) {
	t.Run("it carries the index name, the field it is on, the schema of the table it indexes and what is known about that table", func(t *testing.T) {
		tableSchema := newTestSchema(t)
		tableStatistics := NewTableStatistics(7, 100)

		info := NewIndexInfo(testIndexName, testIndexFieldName, tableSchema, tableStatistics)

		if info.indexName != testIndexName {
			t.Errorf("indexName = %q, want %q", info.indexName, testIndexName)
		}
		if info.fieldName != testIndexFieldName {
			t.Errorf("fieldName = %q, want %q", info.fieldName, testIndexFieldName)
		}
		if info.tableSchema != tableSchema {
			t.Errorf("tableSchema = %p, want %p", info.tableSchema, tableSchema)
		}
		if info.tableStatistics != tableStatistics {
			t.Errorf("tableStatistics = %p, want %p", info.tableStatistics, tableStatistics)
		}
	})
}
