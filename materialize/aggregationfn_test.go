package materialize

import (
	"errors"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

func TestAggregationFnAddFieldTo(t *testing.T) {
	tests := []struct {
		name       string
		fn         AggregationFn
		wantField  string
		wantType   recordmanager.FieldType
		wantLength int
	}{
		{
			name:      "given records of an int id and a varchar name, when a count of name adds its field, then it is countofname of type int",
			fn:        NewCountFn("name"),
			wantField: "countofname",
			wantType:  recordmanager.FieldTypeInt,
		},
		{
			name:       "given records of an int id and a varchar name, when a max of name adds its field, then it is maxofname, a varchar as long as name",
			fn:         NewMaxFn("name"),
			wantField:  "maxofname",
			wantType:   recordmanager.FieldTypeVarchar,
			wantLength: testStringFieldLength,
		},
		{
			name:      "given records of an int id and a varchar name, when a max of id adds its field, then it is maxofid of type int",
			fn:        NewMaxFn("id"),
			wantField: "maxofid",
			wantType:  recordmanager.FieldTypeInt,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := recordmanager.NewSchema()
			if err := tt.fn.AddFieldTo(schema, newTestSchema(t)); err != nil {
				t.Fatalf("AddFieldTo() error = %v", err)
			}

			gotType, err := schema.Type(tt.wantField)
			if err != nil {
				t.Fatalf("Type(%q) error = %v", tt.wantField, err)
			}
			if gotType != tt.wantType {
				t.Errorf("Type(%q) = %v, want %v", tt.wantField, gotType, tt.wantType)
			}
			gotLength, err := schema.Length(tt.wantField)
			if err != nil {
				t.Fatalf("Length(%q) error = %v", tt.wantField, err)
			}
			if gotLength != tt.wantLength {
				t.Errorf("Length(%q) = %d, want %d", tt.wantField, gotLength, tt.wantLength)
			}
		})
	}

	t.Run("given records of an int id and a varchar name, when a max of a field they do not have adds its field, then it reports ErrFieldNotFound", func(t *testing.T) {
		err := NewMaxFn("missing").AddFieldTo(recordmanager.NewSchema(), newTestSchema(t))
		if !errors.Is(err, recordmanager.ErrFieldNotFound) {
			t.Errorf("AddFieldTo() error = %v, want %v", err, recordmanager.ErrFieldNotFound)
		}
	})
}
