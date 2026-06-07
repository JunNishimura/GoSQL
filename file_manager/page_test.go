package filemanager

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func TestNewPageByBlockSize(t *testing.T) {
	tests := []struct {
		name      string
		blockSize int
	}{
		{
			name:      "allocates buffer with given block size",
			blockSize: 400,
		},
		{
			name:      "allocates zero-initialized buffer",
			blockSize: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.blockSize)
			if len(p.buf) != tt.blockSize {
				t.Errorf("buf length = %d, want %d", len(p.buf), tt.blockSize)
			}
			for _, b := range p.buf {
				if b != 0 {
					t.Error("buf should be zero-initialized")
					break
				}
			}
		})
	}
}

func TestNewPageByBytes(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
	}{
		{
			name: "wraps given byte slice",
			src:  []byte{1, 2, 3},
		},
		{
			name: "wraps empty byte slice",
			src:  []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBytes(tt.src)
			if !bytes.Equal(p.buf, tt.src) {
				t.Errorf("buf = %v, want %v", p.buf, tt.src)
			}
		})
	}
}

func TestSetGetInt(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  int32
	}{
		{
			name:   "stores and retrieves a positive int32 value",
			offset: 0,
			value:  42,
		},
		{
			name:   "stores and retrieves zero",
			offset: 0,
			value:  0,
		},
		{
			name:   "stores and retrieves a negative int32 value",
			offset: 0,
			value:  -1,
		},
		{
			name:   "stores and retrieves value at non-zero offset",
			offset: 4,
			value:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			if err := p.SetInt(tt.offset, tt.value); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
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
		{
			name:   "stores and retrieves non-empty byte slice",
			offset: 0,
			value:  []byte{1, 2, 3},
		},
		{
			name:   "stores and retrieves empty byte slice",
			offset: 0,
			value:  []byte{},
		},
		{
			name:   "stores and retrieves bytes at non-zero offset",
			offset: 8,
			value:  []byte{9, 8, 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			if err := p.SetBytes(tt.offset, tt.value); err != nil {
				t.Fatalf("SetBytes() error = %v", err)
			}
			if got := p.GetBytes(tt.offset); !bytes.Equal(got, tt.value) {
				t.Errorf("GetBytes() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestGetBytesReturnsIndependentCopy(t *testing.T) {
	p := NewPageByBlockSize(64)
	if err := p.SetBytes(0, []byte{1, 2, 3}); err != nil {
		t.Fatalf("SetBytes() error = %v", err)
	}

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
		{
			name:   "stores and retrieves ASCII string",
			offset: 0,
			value:  "hello",
		},
		{
			name:   "stores and retrieves empty string",
			offset: 0,
			value:  "",
		},
		{
			name:   "stores and retrieves multibyte UTF-8 string",
			offset: 0,
			value:  "日本語",
		},
		{
			name:   "stores and retrieves string at non-zero offset",
			offset: 16,
			value:  "world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(128)
			if err := p.SetString(tt.offset, tt.value); err != nil {
				t.Fatalf("SetString() error = %v", err)
			}
			if got := p.GetString(tt.offset); got != tt.value {
				t.Errorf("GetString() = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestSetIntBoundsCheck(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		wantErr bool
	}{
		{
			name:    "returns no error when int fits exactly in buffer",
			bufSize: 4,
			offset:  0,
			wantErr: false,
		},
		{
			name:    "returns error when offset plus 4 bytes exceeds buffer size",
			bufSize: 4,
			offset:  1,
			wantErr: true,
		},
		{
			name:    "returns error when offset is equal to buffer size",
			bufSize: 4,
			offset:  4,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetInt(tt.offset, 42)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetInt() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetBytesBoundsCheck(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		value   []byte
		wantErr bool
	}{
		{
			name:    "returns no error when bytes fit exactly in buffer",
			bufSize: 7,
			offset:  0,
			value:   []byte{1, 2, 3},
			wantErr: false,
		},
		{
			name:    "returns error when data exceeds buffer size",
			bufSize: 6,
			offset:  0,
			value:   []byte{1, 2, 3},
			wantErr: true,
		},
		{
			name:    "returns error when offset plus data exceeds buffer size",
			bufSize: 8,
			offset:  2,
			value:   []byte{1, 2, 3},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetBytes(tt.offset, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetBytes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetStringBoundsCheck(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		value   string
		wantErr bool
	}{
		{
			name:    "returns no error when string fits exactly in buffer",
			bufSize: 9,
			offset:  0,
			value:   "hello",
			wantErr: false,
		},
		{
			name:    "returns error when string exceeds buffer size",
			bufSize: 8,
			offset:  0,
			value:   "hello",
			wantErr: true,
		},
		{
			name:    "returns error when offset plus string exceeds buffer size",
			bufSize: 10,
			offset:  2,
			value:   "hello",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetString(tt.offset, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMaxLength(t *testing.T) {
	p := NewPageByBlockSize(64)
	tests := []struct {
		name   string
		strlen int
		want   int
	}{
		{
			name:   "returns 4 bytes for empty string (length prefix only)",
			strlen: 0,
			want:   4,
		},
		{
			name:   "returns 4 plus UTFMax for single character string",
			strlen: 1,
			want:   4 + utf8.UTFMax,
		},
		{
			name:   "returns 4 plus UTFMax times strlen for multi-character string",
			strlen: 10,
			want:   4 + 10*utf8.UTFMax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.MaxLength(tt.strlen); got != tt.want {
				t.Errorf("MaxLength(%d) = %d, want %d", tt.strlen, got, tt.want)
			}
		})
	}
}
