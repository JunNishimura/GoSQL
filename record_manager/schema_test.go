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

func TestFieldTypeString(t *testing.T) {
	tests := []struct {
		name      string
		fieldType FieldType
		want      string
	}{
		{
			name:      "given the int field type, it reads as int",
			fieldType: FieldTypeInt,
			want:      "int",
		},
		{
			name:      "given the varchar field type, it reads as varchar",
			fieldType: FieldTypeVarchar,
			want:      "varchar",
		},
		{
			name:      "given a type it does not know, it shows the number rather than nothing",
			fieldType: FieldType(7),
			want:      "FieldType(7)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fieldType.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
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
			name:       "when an int field is added, then it is kept with a length of zero",
			fieldName:  "id",
			fieldType:  FieldTypeInt,
			length:     0,
			wantType:   FieldTypeInt,
			wantLength: 0,
		},
		{
			name:       "when a varchar field is added, then its character limit is kept",
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
	t.Run("it adds a field of the int type, with no character limit of its own", func(t *testing.T) {
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
	})
}

func TestSchemaAddStringField(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		length    int
	}{
		{
			name:      "when a varchar field is added, then it is a varchar of the character limit given",
			fieldName: "name",
			length:    20,
		},
		{
			name:      "given a character limit of zero, it keeps the zero rather than reading it as unset",
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
			name: "given a schema that already has the name, when AddField is called with it, then it reports ErrDuplicateField",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddField("id", FieldTypeVarchar, 20)
			},
		},
		{
			name: "given a schema that already has the name, when AddIntField is called with it, then it reports ErrDuplicateField",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddIntField("id")
			},
		},
		{
			name: "given a schema that already has the name, when AddStringField is called with it, then it reports ErrDuplicateField",
			add: func(_ *testing.T, s *Schema) error {
				return s.AddStringField("id", 20)
			},
		},
		{
			name: "given a schema that already has the name, when Add copies it from another schema, then it reports ErrDuplicateField",
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
			name: "given a schema with no fields, it returns an empty slice rather than nothing",
			build: func(_ *testing.T) *Schema {
				return NewSchema()
			},
			want: []string{},
		},
		{
			name: "it returns the field names in the order they were added, not in the order of the names",
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

	// The slice is a copy, so writing to it has to leave the schema alone.
	aliasTests := []struct {
		name   string
		mutate func(fields []string)
		want   []string
	}{
		{
			name: "when an element of the slice it returned is overwritten, then the schema keeps its field names",
			mutate: func(fields []string) {
				fields[0] = "overwritten"
			},
			want: []string{"id", "name", "age"},
		},
		{
			name: "when the slice it returned is sorted in place, then the schema keeps the order fields were added in",
			mutate: func(fields []string) {
				slices.Sort(fields)
			},
			want: []string{"id", "name", "age"},
		},
	}

	for _, tt := range aliasTests {
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
			name:      "given a field that was added, it reports the schema has it",
			fieldName: "id",
			want:      true,
		},
		{
			name:      "given a field that was never added, it reports the schema does not have it",
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

func TestSchemaType(t *testing.T) {
	t.Run("given a field the schema does not have, it reports ErrFieldNotFound", func(t *testing.T) {
		s := NewSchema()
		mustAddIntField(t, s, "id")

		if _, err := s.Type("missing"); !errors.Is(err, ErrFieldNotFound) {
			t.Errorf("Type(\"missing\") error = %v, want %v", err, ErrFieldNotFound)
		}
	})
}

func TestSchemaLength(t *testing.T) {
	t.Run("given a field the schema does not have, it reports ErrFieldNotFound", func(t *testing.T) {
		s := NewSchema()
		mustAddIntField(t, s, "id")

		if _, err := s.Length("missing"); !errors.Is(err, ErrFieldNotFound) {
			t.Errorf("Length(\"missing\") error = %v, want %v", err, ErrFieldNotFound)
		}
	})
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
			name:       "when an int field is copied from another schema, then its type comes from there",
			fieldName:  "id",
			wantFields: []string{"id"},
			wantType:   FieldTypeInt,
			wantLength: 0,
		},
		{
			name:       "when a varchar field is copied from another schema, then its character limit comes from there too",
			fieldName:  "name",
			wantFields: []string{"name"},
			wantType:   FieldTypeVarchar,
			wantLength: 20,
		},
		{
			name:       "given another schema that lacks the field, it reports ErrFieldNotFound and copies nothing",
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
			name: "given a schema with no fields, it copies every field of the other one, keeping that one's order",
			build: func(_ *testing.T) *Schema {
				return NewSchema()
			},
			want: []string{"id", "name"},
		},
		{
			name: "given a schema that already has fields, the copied ones go after them",
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

	// Nothing is copied unless all of it can be, so a clash has to leave the
	// schema as it was rather than half combined.
	duplicateTests := []struct {
		name  string
		build func(t *testing.T) *Schema
		want  []string
	}{
		{
			name: "given a schema that already has the other's first field, it reports ErrDuplicateField and copies nothing",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				return s
			},
			want: []string{"id"},
		},
		{
			name: "given a schema that clashes on a field after the first, it still copies nothing, not even the ones before it",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddStringField(t, s, "name", 20)
				return s
			},
			want: []string{"name"},
		},
	}

	for _, tt := range duplicateTests {
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
