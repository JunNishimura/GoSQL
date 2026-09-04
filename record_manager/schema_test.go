package recordmanager

import (
	"errors"
	"slices"
	"testing"
)

// mustAddIntField adds an int field as part of a test's setup. A schema that
// refuses it means the test itself is broken, not that the assertion failed, so
// it stops the test rather than reporting a difference.
func mustAddIntField(t *testing.T, s *Schema, fieldName string) {
	t.Helper()

	if err := s.AddIntField(fieldName); err != nil {
		t.Fatalf("AddIntField(%q) returned error: %v", fieldName, err)
	}
}

// mustAddStringField adds a varchar field as part of a test's setup. See
// mustAddIntField for why it stops the test.
func mustAddStringField(t *testing.T, s *Schema, fieldName string, length int) {
	t.Helper()

	if err := s.AddStringField(fieldName, length); err != nil {
		t.Fatalf("AddStringField(%q, %d) returned error: %v", fieldName, length, err)
	}
}

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
			if err := s.AddField(tt.fieldName, tt.fieldType, tt.length); err != nil {
				t.Fatalf("AddField(%q, %d, %d) returned error: %v", tt.fieldName, tt.fieldType, tt.length, err)
			}

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
	s := NewSchema()
	mustAddIntField(t, s, "id")

	gotType, err := s.Type("id")
	if err != nil {
		t.Fatalf("Type(\"id\") returned error: %v", err)
	}
	if gotType != FieldTypeInt {
		t.Errorf("Type(\"id\") = %d, want %d", gotType, FieldTypeInt)
	}

	gotLength, err := s.Length("id")
	if err != nil {
		t.Fatalf("Length(\"id\") returned error: %v", err)
	}
	if gotLength != 0 {
		t.Errorf("Length(\"id\") = %d, want 0", gotLength)
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
			mustAddStringField(t, s, tt.fieldName, tt.length)

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

func TestSchemaAddDuplicateField(t *testing.T) {
	tests := []struct {
		name string
		add  func(t *testing.T, s *Schema) error
	}{
		{
			name: "AddField refuses a name the schema already has",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddField("id", FieldTypeVarchar, 20)
			},
		},
		{
			name: "AddIntField refuses a name the schema already has",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddIntField("id")
			},
		},
		{
			name: "AddStringField refuses a name the schema already has",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddStringField("id", 20)
			},
		},
		{
			name: "Add refuses a name the schema already has",
			add: func(t *testing.T, s *Schema) error {
				other := NewSchema()
				mustAddStringField(t, other, "id", 20)
				return s.Add("id", other)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSchema()
			mustAddIntField(t, s, "id")
			mustAddStringField(t, s, "name", 20)

			if err := tt.add(t, s); !errors.Is(err, ErrDuplicateField) {
				t.Fatalf("adding a duplicate field returned error %v, want %v", err, ErrDuplicateField)
			}

			want := []string{"id", "name"}
			if got := s.Fields(); !slices.Equal(got, want) {
				t.Errorf("Fields() = %v, want %v", got, want)
			}

			gotType, err := s.Type("id")
			if err != nil {
				t.Fatalf("Type(\"id\") returned error: %v", err)
			}
			if gotType != FieldTypeInt {
				t.Errorf("Type(\"id\") = %d, want %d: the refused add redefined the field", gotType, FieldTypeInt)
			}
		})
	}
}

func TestSchemaFields(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *Schema
		want  []string
	}{
		{
			name: "returns an empty slice for a schema with no fields",
			build: func(_ *testing.T) *Schema {
				return NewSchema()
			},
			want: []string{},
		},
		{
			name: "returns the field names in the order they were added",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				mustAddStringField(t, s, "name", 20)
				mustAddIntField(t, s, "age")
				return s
			},
			want: []string{"id", "name", "age"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build(t).Fields(); !slices.Equal(got, tt.want) {
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
			mustAddIntField(t, s, "id")
			mustAddStringField(t, s, "name", 20)
			mustAddIntField(t, s, "age")

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
			mustAddIntField(t, s, "id")
			mustAddStringField(t, s, "name", 20)

			if got := s.HasField(tt.fieldName); got != tt.want {
				t.Errorf("HasField(%q) = %t, want %t", tt.fieldName, got, tt.want)
			}
		})
	}
}

func TestSchemaTypeRejectsAnUnknownField(t *testing.T) {
	s := NewSchema()
	mustAddIntField(t, s, "id")

	if _, err := s.Type("missing"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("Type(\"missing\") error = %v, want %v", err, ErrFieldNotFound)
	}
}

func TestSchemaLengthRejectsAnUnknownField(t *testing.T) {
	s := NewSchema()
	mustAddIntField(t, s, "id")

	if _, err := s.Length("missing"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("Length(\"missing\") error = %v, want %v", err, ErrFieldNotFound)
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
			mustAddIntField(t, other, "id")
			mustAddStringField(t, other, "name", 20)

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
		build func(t *testing.T) *Schema
		want  []string
	}{
		{
			name: "copies every field of the other schema in its order",
			build: func(_ *testing.T) *Schema {
				return NewSchema()
			},
			want: []string{"id", "name"},
		},
		{
			name: "appends the other schema's fields after the existing ones",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "age")
				return s
			},
			want: []string{"age", "id", "name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := NewSchema()
			mustAddIntField(t, other, "id")
			mustAddStringField(t, other, "name", 20)

			s := tt.build(t)
			if err := s.AddAll(other); err != nil {
				t.Fatalf("AddAll(other) returned error: %v", err)
			}

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

func TestSchemaAddAllDuplicateField(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *Schema
		want  []string
	}{
		{
			name: "copies nothing when the other schema's first field is already present",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				return s
			},
			want: []string{"id"},
		},
		{
			name: "copies nothing when the clash is on a field after the first",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddStringField(t, s, "name", 20)
				return s
			},
			want: []string{"name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other := NewSchema()
			mustAddIntField(t, other, "id")
			mustAddStringField(t, other, "name", 20)

			s := tt.build(t)
			if err := s.AddAll(other); !errors.Is(err, ErrDuplicateField) {
				t.Fatalf("AddAll(other) error = %v, want %v", err, ErrDuplicateField)
			}

			if got := s.Fields(); !slices.Equal(got, tt.want) {
				t.Errorf("Fields() = %v, want %v: AddAll copied part of the other schema", got, tt.want)
			}
		})
	}
}
