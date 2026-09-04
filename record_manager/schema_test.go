package recordmanager

import (
	"errors"
	"slices"
	"testing"
)

func TestSchemaAddField(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		fieldType  FieldType
		length     int
		wantType   FieldType
		wantLength int
	}{
		{
			name:       "adds an int field whose length is zero",
			fieldName:  "id",
			fieldType:  FieldTypeInt,
			length:     0,
			wantType:   FieldTypeInt,
			wantLength: 0,
		},
		{
			name:       "adds a varchar field keeping its character limit",
			fieldName:  "name",
			fieldType:  FieldTypeVarchar,
			length:     20,
			wantType:   FieldTypeVarchar,
			wantLength: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddField(tt.fieldName, tt.fieldType, tt.length)

			if !s.HasField(tt.fieldName) {
				t.Fatalf("HasField(%q) = false, want true", tt.fieldName)
			}

			gotType, err := s.Type(tt.fieldName)
			if err != nil {
				t.Fatalf("Type(%q) returned error: %v", tt.fieldName, err)
			}
			if gotType != tt.wantType {
				t.Errorf("Type(%q) = %d, want %d", tt.fieldName, gotType, tt.wantType)
			}

			gotLength, err := s.Length(tt.fieldName)
			if err != nil {
				t.Fatalf("Length(%q) returned error: %v", tt.fieldName, err)
			}
			if gotLength != tt.wantLength {
				t.Errorf("Length(%q) = %d, want %d", tt.fieldName, gotLength, tt.wantLength)
			}
		})
	}
}

func TestSchemaAddIntField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
	}{
		{
			name:      "adds an int field under the given name",
			fieldName: "id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddIntField(tt.fieldName)

			gotType, err := s.Type(tt.fieldName)
			if err != nil {
				t.Fatalf("Type(%q) returned error: %v", tt.fieldName, err)
			}
			if gotType != FieldTypeInt {
				t.Errorf("Type(%q) = %d, want %d", tt.fieldName, gotType, FieldTypeInt)
			}

			gotLength, err := s.Length(tt.fieldName)
			if err != nil {
				t.Fatalf("Length(%q) returned error: %v", tt.fieldName, err)
			}
			if gotLength != 0 {
				t.Errorf("Length(%q) = %d, want 0", tt.fieldName, gotLength)
			}
		})
	}
}

func TestSchemaAddStringField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		length    int
	}{
		{
			name:      "adds a varchar field with the given character limit",
			fieldName: "name",
			length:    20,
		},
		{
			name:      "adds a varchar field whose character limit is zero",
			fieldName: "empty",
			length:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddStringField(tt.fieldName, tt.length)

			gotType, err := s.Type(tt.fieldName)
			if err != nil {
				t.Fatalf("Type(%q) returned error: %v", tt.fieldName, err)
			}
			if gotType != FieldTypeVarchar {
				t.Errorf("Type(%q) = %d, want %d", tt.fieldName, gotType, FieldTypeVarchar)
			}

			gotLength, err := s.Length(tt.fieldName)
			if err != nil {
				t.Fatalf("Length(%q) returned error: %v", tt.fieldName, err)
			}
			if gotLength != tt.length {
				t.Errorf("Length(%q) = %d, want %d", tt.fieldName, gotLength, tt.length)
			}
		})
	}
}

func TestSchemaFields(t *testing.T) {
	tests := []struct {
		name  string
		build func() *Schema
		want  []string
	}{
		{
			name: "returns an empty slice for a schema with no fields",
			build: func() *Schema {
				return NewSchema()
			},
			want: []string{},
		},
		{
			name: "returns the field names in the order they were added",
			build: func() *Schema {
				s := NewSchema()
				s.AddIntField("id")
				s.AddStringField("name", 20)
				s.AddIntField("age")
				return s
			},
			want: []string{"id", "name", "age"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build().Fields(); !slices.Equal(got, tt.want) {
				t.Errorf("Fields() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchemaFieldsDoesNotAliasTheSchema(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fields []string)
		want   []string
	}{
		{
			name: "keeps the field names when an element of the returned slice is overwritten",
			mutate: func(fields []string) {
				fields[0] = "overwritten"
			},
			want: []string{"id", "name", "age"},
		},
		{
			name: "keeps the field order when the returned slice is sorted in place",
			mutate: func(fields []string) {
				slices.Sort(fields)
			},
			want: []string{"id", "name", "age"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddIntField("id")
			s.AddStringField("name", 20)
			s.AddIntField("age")

			tt.mutate(s.Fields())

			if got := s.Fields(); !slices.Equal(got, tt.want) {
				t.Errorf("Fields() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchemaHasField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		want      bool
	}{
		{
			name:      "returns true for a field that was added",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "returns false for a field that was never added",
			fieldName: "missing",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddIntField("id")
			s.AddStringField("name", 20)

			if got := s.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestSchemaTypeUnknownField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
	}{
		{
			name:      "reports ErrFieldNotFound for a field that was never added",
			fieldName: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddIntField("id")

			if _, err := s.Type(tt.fieldName); !errors.Is(err, ErrFieldNotFound) {
				t.Errorf("Type(%q) error = %v, want %v", tt.fieldName, err, ErrFieldNotFound)
			}
		})
	}
}

func TestSchemaLengthUnknownField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
	}{
		{
			name:      "reports ErrFieldNotFound for a field that was never added",
			fieldName: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			s.AddIntField("id")

			if _, err := s.Length(tt.fieldName); !errors.Is(err, ErrFieldNotFound) {
				t.Errorf("Length(%q) error = %v, want %v", tt.fieldName, err, ErrFieldNotFound)
			}
		})
	}
}

func TestSchemaAdd(t *testing.T) {
	tests := []struct {
		name       string
		fieldName  string
		wantErr    error
		wantFields []string
		wantType   FieldType
		wantLength int
	}{
		{
			name:       "copies an int field from the other schema",
			fieldName:  "id",
			wantFields: []string{"id"},
			wantType:   FieldTypeInt,
			wantLength: 0,
		},
		{
			name:       "copies a varchar field with its character limit",
			fieldName:  "name",
			wantFields: []string{"name"},
			wantType:   FieldTypeVarchar,
			wantLength: 20,
		},
		{
			name:       "reports ErrFieldNotFound when the other schema lacks the field",
			fieldName:  "missing",
			wantErr:    ErrFieldNotFound,
			wantFields: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := NewSchema()
			other.AddIntField("id")
			other.AddStringField("name", 20)

			s := NewSchema()
			err := s.Add(tt.fieldName, other)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Add(%q, other) error = %v, want %v", tt.fieldName, err, tt.wantErr)
			}
			if got := s.Fields(); !slices.Equal(got, tt.wantFields) {
				t.Fatalf("Fields() = %v, want %v", got, tt.wantFields)
			}
			if tt.wantErr != nil {
				return
			}

			gotType, err := s.Type(tt.fieldName)
			if err != nil {
				t.Fatalf("Type(%q) returned error: %v", tt.fieldName, err)
			}
			if gotType != tt.wantType {
				t.Errorf("Type(%q) = %d, want %d", tt.fieldName, gotType, tt.wantType)
			}

			gotLength, err := s.Length(tt.fieldName)
			if err != nil {
				t.Fatalf("Length(%q) returned error: %v", tt.fieldName, err)
			}
			if gotLength != tt.wantLength {
				t.Errorf("Length(%q) = %d, want %d", tt.fieldName, gotLength, tt.wantLength)
			}
		})
	}
}

func TestSchemaAddAll(t *testing.T) {
	tests := []struct {
		name  string
		build func() *Schema
		want  []string
	}{
		{
			name: "copies every field of the other schema in its order",
			build: func() *Schema {
				return NewSchema()
			},
			want: []string{"id", "name"},
		},
		{
			name: "appends the other schema's fields after the existing ones",
			build: func() *Schema {
				s := NewSchema()
				s.AddIntField("age")
				return s
			},
			want: []string{"age", "id", "name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := NewSchema()
			other.AddIntField("id")
			other.AddStringField("name", 20)

			s := tt.build()
			s.AddAll(other)

			if got := s.Fields(); !slices.Equal(got, tt.want) {
				t.Fatalf("Fields() = %v, want %v", got, tt.want)
			}

			gotLength, err := s.Length("name")
			if err != nil {
				t.Fatalf("Length(\"name\") returned error: %v", err)
			}
			if gotLength != 20 {
				t.Errorf("Length(\"name\") = %d, want 20", gotLength)
			}
		})
	}
}
