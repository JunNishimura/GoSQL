package filemanager

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func TestNewPageByBlockSize(t *testing.T) {
	p := NewPageByBlockSize(400)
	if len(p.buf) != 400 {
		t.Errorf("buf length = %d, want %d", len(p.buf), 400)
	}
	for _, b := range p.buf {
		if b != 0 {
			t.Error("buf should be zero-initialized")
			break
		}
	}
}

func TestNewPageByBytes(t *testing.T) {
	src := []byte{1, 2, 3}
	p := NewPageByBytes(src)
	if !bytes.Equal(p.buf, src) {
		t.Errorf("buf = %v, want %v", p.buf, src)
	}
}

func TestSetGetInt(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  int32
	}{
		{"positive value", 0, 42},
		{"zero", 0, 0},
		{"negative value", 0, -1},
		{"non-zero offset", 4, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			p.SetInt(tt.offset, tt.value)
			if got := p.GetInt(tt.offset); got != tt.value {
				t.Errorf("GetInt() = %d, want %d", got, tt.value)
			}
		})
	}
}

func TestSetGetBytes(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  []byte
	}{
		{"non-empty bytes", 0, []byte{1, 2, 3}},
		{"empty bytes", 0, []byte{}},
		{"non-zero offset", 8, []byte{9, 8, 7}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			p.SetBytes(tt.offset, tt.value)
			if got := p.GetBytes(tt.offset); !bytes.Equal(got, tt.value) {
				t.Errorf("GetBytes() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestGetBytesReturnsIndependentCopy(t *testing.T) {
	p := NewPageByBlockSize(64)
	p.SetBytes(0, []byte{1, 2, 3})

	got := p.GetBytes(0)
	got[0] = 99

	if p.GetBytes(0)[0] == 99 {
		t.Error("GetBytes() returned a slice sharing the internal buffer")
	}
}

func TestSetGetString(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  string
	}{
		{"ascii string", 0, "hello"},
		{"empty string", 0, ""},
		{"multibyte string", 0, "日本語"},
		{"non-zero offset", 16, "world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(128)
			p.SetString(tt.offset, tt.value)
			if got := p.GetString(tt.offset); got != tt.value {
				t.Errorf("GetString() = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestMaxLength(t *testing.T) {
	p := NewPageByBlockSize(64)
	tests := []struct {
		strlen int
		want   int
	}{
		{0, 4},
		{1, 4 + utf8.UTFMax},
		{10, 4 + 10*utf8.UTFMax},
	}

	for _, tt := range tests {
		if got := p.MaxLength(tt.strlen); got != tt.want {
			t.Errorf("MaxLength(%d) = %d, want %d", tt.strlen, got, tt.want)
		}
	}
}
