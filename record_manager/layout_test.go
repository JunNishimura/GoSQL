package recordmanager

import (
	"errors"
	"maps"
	"testing"
)

// The widths the expected numbers below are built from, spelled out once:
//
//	an int          4 bytes
//	a varchar(n)    4 + 4n bytes, a length prefix plus the widest encoding
//	                of each character
//	the in-use flag 4 bytes, at the front of every slot
//
// They are written as literals rather than computed, so that a change to any of
// those widths shows up here as a failing test: it is a change to the format
// records are stored in.

func TestNewLayoutSlotSize(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *Schema
		want  int
	}{
		{
			name: "reserves the in-use flag for a schema with no fields",
			build: func(_ *testing.T) *Schema {
				return NewSchema()
			},
			want: 4,
		},
		{
			name: "adds the width of an int field to the flag",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				return s
			},
			want: 4 + 4,
		},
		{
			name: "sizes a varchar field by its character limit, not by any value",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddStringField(t, s, "name", 20)
				return s
			},
			want: 4 + (4 + 20*4),
		},
		{
			name: "adds up every field of a mixed schema",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				mustAddStringField(t, s, "name", 20)
				mustAddIntField(t, s, "age")
				return s
			},
			want: 4 + 4 + (4 + 20*4) + 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewLayout(tt.build(t)).SlotSize(); got != tt.want {
				t.Errorf("SlotSize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewLayoutOffsets(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T) *Schema
		want  map[string]int
	}{
		{
			name: "puts the only field after the in-use flag",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				return s
			},
			want: map[string]int{
				"id": 4,
			},
		},
		{
			name: "places the fields one after another in the order the schema lists them",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "id")
				mustAddStringField(t, s, "name", 20)
				mustAddIntField(t, s, "age")
				return s
			},
			want: map[string]int{
				"id":   4,
				"name": 4 + 4,
				"age":  4 + 4 + (4 + 20*4),
			},
		},
		{
			name: "follows the schema's order rather than the field names",
			build: func(t *testing.T) *Schema {
				s := NewSchema()
				mustAddIntField(t, s, "age")
				mustAddIntField(t, s, "id")
				return s
			},
			want: map[string]int{
				"age": 4,
				"id":  4 + 4,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := NewLayout(tt.build(t))

			got := map[string]int{}
			for fieldName := range tt.want {
				offset, err := l.Offset(fieldName)
				if err != nil {
					t.Fatalf("Offset(%q) returned error: %v", fieldName, err)
				}
				got[fieldName] = offset
			}

			if !maps.Equal(got, tt.want) {
				t.Errorf("offsets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLayoutOffsetRejectsAnUnknownField(t *testing.T) {
	s := NewSchema()
	mustAddIntField(t, s, "id")

	l := NewLayout(s)

	if _, err := l.Offset("missing"); !errors.Is(err, ErrFieldNotFound) {
		t.Errorf("Offset(\"missing\") error = %v, want %v", err, ErrFieldNotFound)
	}
}

func TestLayoutSchemaReturnsTheSchemaItWasBuiltFrom(t *testing.T) {
	s := NewSchema()
	mustAddIntField(t, s, "id")

	if got := NewLayout(s).Schema(); got != s {
		t.Errorf("Schema() = %p, want %p", got, s)
	}
}
