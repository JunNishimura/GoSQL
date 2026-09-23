package query

import (
	"errors"
	"testing"

	recordmanager "github.com/JunNishimura/GoSQL/record_manager"
)

func TestNewIntConstant(t *testing.T) {
	t.Run("it carries the value it was made from, and is an int", func(t *testing.T) {
		const val = 42

		c := NewIntConstant(val)

		if got := c.Type(); got != recordmanager.FieldTypeInt {
			t.Errorf("Type() = %s, want %s", got, recordmanager.FieldTypeInt)
		}

		got, err := c.AsInt()
		if err != nil {
			t.Fatalf("AsInt() error = %v", err)
		}
		if got != val {
			t.Errorf("AsInt() = %d, want %d", got, val)
		}
	})
}

func TestNewStringConstant(t *testing.T) {
	t.Run("it carries the text it was made from, and is a varchar", func(t *testing.T) {
		const val = "alice"

		c := NewStringConstant(val)

		if got := c.Type(); got != recordmanager.FieldTypeVarchar {
			t.Errorf("Type() = %s, want %s", got, recordmanager.FieldTypeVarchar)
		}

		got, err := c.AsString()
		if err != nil {
			t.Fatalf("AsString() error = %v", err)
		}
		if got != val {
			t.Errorf("AsString() = %q, want %q", got, val)
		}
	})
}

// The values here are the zero value of either kind, so what the cases turn on
// is the kind the constant was made as and nothing about what it holds.
func TestConstantType(t *testing.T) {
	tests := []struct {
		name string
		c    Constant
		want recordmanager.FieldType
	}{
		{
			name: "given a constant made from an int, it reports the int field type",
			c:    NewIntConstant(0),
			want: recordmanager.FieldTypeInt,
		},
		{
			name: "given a constant made from a string, it reports the varchar field type",
			c:    NewStringConstant(""),
			want: recordmanager.FieldTypeVarchar,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Type(); got != tt.want {
				t.Errorf("Type() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestConstantAsInt(t *testing.T) {
	tests := []struct {
		name    string
		c       Constant
		want    int32
		wantErr error
	}{
		{
			name: "given an int constant, it returns the value it holds",
			c:    NewIntConstant(42),
			want: 42,
		},
		{
			name: "given a negative int constant, it returns the value it holds",
			c:    NewIntConstant(-1),
			want: -1,
		},
		// The text is the digits of a number, so a constant that read its
		// varchar as an int would come back with 42 rather than refuse.
		{
			name:    "given a varchar constant whose text is digits, it reports ErrConstantTypeMismatch rather than reading the text as a number",
			c:       NewStringConstant("42"),
			wantErr: ErrConstantTypeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.AsInt()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("AsInt() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("AsInt() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("AsInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConstantAsString(t *testing.T) {
	tests := []struct {
		name    string
		c       Constant
		want    string
		wantErr error
	}{
		{
			name: "given a varchar constant, it returns the text it holds",
			c:    NewStringConstant("alice"),
			want: "alice",
		},
		{
			name: "given a varchar constant of the empty string, it returns the empty string",
			c:    NewStringConstant(""),
			want: "",
		},
		{
			name:    "given an int constant, it reports ErrConstantTypeMismatch rather than writing the number out as text",
			c:       NewIntConstant(42),
			wantErr: ErrConstantTypeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.c.AsString()

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("AsString() error = %v, want %v", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("AsString() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("AsString() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A constant is compared with ==, which is why it is a value rather than a
// pointer. A term of a predicate is two of these put side by side, so this is
// the operation the whole of query processing rests on.
//
// The last two cases are what the kind a constant carries is for. Without it a
// constant would be its two values alone, and the unused one of the two is the
// zero value of its type, so an int and a varchar that both hold nothing else
// would read as the same value.
func TestConstantEquality(t *testing.T) {
	tests := []struct {
		name  string
		left  Constant
		right Constant
		want  bool
	}{
		{
			name:  "given two int constants of the same value, they are equal",
			left:  NewIntConstant(42),
			right: NewIntConstant(42),
			want:  true,
		},
		{
			name:  "given two int constants of different values, they are not equal",
			left:  NewIntConstant(42),
			right: NewIntConstant(7),
			want:  false,
		},
		{
			name:  "given two varchar constants of the same text, they are equal",
			left:  NewStringConstant("alice"),
			right: NewStringConstant("alice"),
			want:  true,
		},
		{
			name:  "given two varchar constants of different text, they are not equal",
			left:  NewStringConstant("alice"),
			right: NewStringConstant("bob"),
			want:  false,
		},
		{
			name:  "given an int constant and a varchar constant whose text is the same digits, they are not equal",
			left:  NewIntConstant(42),
			right: NewStringConstant("42"),
			want:  false,
		},
		{
			name:  "given the int constant zero and the varchar constant of the empty string, they are not equal",
			left:  NewIntConstant(0),
			right: NewStringConstant(""),
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.left == tt.right; got != tt.want {
				t.Errorf("%s == %s is %t, want %t", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestConstantString(t *testing.T) {
	tests := []struct {
		name string
		c    Constant
		want string
	}{
		{
			name: "given an int constant, it is the number on its own",
			c:    NewIntConstant(42),
			want: "42",
		},
		// An error message naming both kinds is the reason there is a
		// difference to make: quoted, the two cases above read as one value
		// each rather than as the same one twice.
		{
			name: "given a varchar constant whose text is digits, it is quoted, so it does not read as the number",
			c:    NewStringConstant("42"),
			want: `"42"`,
		},
		{
			name: "given a varchar constant of the empty string, it is a pair of quotes rather than nothing",
			c:    NewStringConstant(""),
			want: `""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		})
	}
}
