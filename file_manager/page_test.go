package filemanager

import (
	"bytes"
	"testing"
	"time"
	"unicode/utf8"
)

func TestNewPageByBlockSize(t *testing.T) {
	tests := []struct {
		name      string
		blockSize int
	}{
		{
			name:      "it allocates a buffer of the block size it was given",
			blockSize: 400,
		},
		{
			name:      "it allocates a buffer of zeroes, so an unwritten page reads as zero",
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
			name: "it wraps the bytes it was given rather than copying them",
			src:  []byte{1, 2, 3},
		},
		{
			name: "given no bytes, it wraps an empty buffer rather than failing",
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
			name:   "when a positive int is written and read back, then it is unchanged",
			offset: 0,
			value:  42,
		},
		{
			name:   "when zero is written and read back, then it is unchanged",
			offset: 0,
			value:  0,
		},
		{
			name:   "when a negative int is written and read back, then the sign survives",
			offset: 0,
			value:  -1,
		},
		{
			name:   "when an int is written past the start of the page, then it is read back from there",
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
			name:   "when bytes are written and read back, then they are unchanged",
			offset: 0,
			value:  []byte{1, 2, 3},
		},
		{
			name:   "when no bytes are written and read back, then the result is empty rather than nil",
			offset: 0,
			value:  []byte{},
		},
		{
			name:   "when bytes are written past the start of the page, then they are read back from there",
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

func TestGetBytes(t *testing.T) {
	t.Run("when the bytes it returned are written to, then the page is unchanged", func(t *testing.T) {
		p := NewPageByBlockSize(64)
		if err := p.SetBytes(0, []byte{1, 2, 3}); err != nil {
			t.Fatalf("SetBytes() error = %v", err)
		}

		got := p.GetBytes(0)
		got[0] = 99

		if p.GetBytes(0)[0] == 99 {
			t.Error("GetBytes() returned a slice sharing the internal buffer")
		}
	})
}

func TestSetGetString(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  string
	}{
		{
			name:   "when a string is written and read back, then it is unchanged",
			offset: 0,
			value:  "hello",
		},
		{
			name:   "when an empty string is written and read back, then it is still empty",
			offset: 0,
			value:  "",
		},
		{
			name:   "when a string of multi-byte characters is written and read back, then it is unchanged",
			offset: 0,
			value:  "日本語",
		},
		{
			name:   "when a string is written past the start of the page, then it is read back from there",
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

func TestSetInt(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		wantErr bool
	}{
		{
			name:    "given a page with room for exactly one int, it accepts a write at the start",
			bufSize: 4,
			offset:  0,
			wantErr: false,
		},
		{
			name:    "when an int would run past the end of the page, then the write is refused",
			bufSize: 4,
			offset:  1,
			wantErr: true,
		},
		{
			name:    "when the offset is the end of the page, then the write is refused",
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

func TestSetBytes(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		value   []byte
		wantErr bool
	}{
		{
			name:    "given a page with room for exactly the length prefix and the bytes, it accepts the write",
			bufSize: 7,
			offset:  0,
			value:   []byte{1, 2, 3},
			wantErr: false,
		},
		{
			name:    "when the bytes and their length prefix do not fit the page, then the write is refused",
			bufSize: 6,
			offset:  0,
			value:   []byte{1, 2, 3},
			wantErr: true,
		},
		{
			name:    "when the bytes fit the page but not from the offset given, then the write is refused",
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

func TestSetString(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		value   string
		wantErr bool
	}{
		{
			name:    "given a page with room for exactly the length prefix and the string, it accepts the write",
			bufSize: 9,
			offset:  0,
			value:   "hello",
			wantErr: false,
		},
		{
			name:    "when the string and its length prefix do not fit the page, then the write is refused",
			bufSize: 8,
			offset:  0,
			value:   "hello",
			wantErr: true,
		},
		{
			name:    "when the string fits the page but not from the offset given, then the write is refused",
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

func TestSetGetShort(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  int16
	}{
		{
			name:   "when a positive short is written and read back, then it is unchanged",
			offset: 0,
			value:  100,
		},
		{
			name:   "when zero is written and read back, then it is unchanged",
			offset: 0,
			value:  0,
		},
		{
			name:   "when a negative short is written and read back, then the sign survives",
			offset: 0,
			value:  -1,
		},
		{
			name:   "when an int is written past the start of the page, then it is read back from there",
			offset: 2,
			value:  42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			if err := p.SetShort(tt.offset, tt.value); err != nil {
				t.Fatalf("SetShort() error = %v", err)
			}
			if got := p.GetShort(tt.offset); got != tt.value {
				t.Errorf("GetShort() = %d, want %d", got, tt.value)
			}
		})
	}
}

func TestSetShort(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		wantErr bool
	}{
		{
			name:    "given a page with room for exactly one short, it accepts a write at the start",
			bufSize: 2,
			offset:  0,
			wantErr: false,
		},
		{
			name:    "when a short would run past the end of the page, then the write is refused",
			bufSize: 2,
			offset:  1,
			wantErr: true,
		},
		{
			name:    "when the offset is the end of the page, then the write is refused",
			bufSize: 2,
			offset:  2,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetShort(tt.offset, 42)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetShort() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetGetBool(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  bool
	}{
		{
			name:   "when true is written and read back, then it is still true",
			offset: 0,
			value:  true,
		},
		{
			name:   "when false is written and read back, then it is still false",
			offset: 0,
			value:  false,
		},
		{
			name:   "when a bool is written past the start of the page, then it is read back from there",
			offset: 1,
			value:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			if err := p.SetBool(tt.offset, tt.value); err != nil {
				t.Fatalf("SetBool() error = %v", err)
			}
			if got := p.GetBool(tt.offset); got != tt.value {
				t.Errorf("GetBool() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestSetBool(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		wantErr bool
	}{
		{
			name:    "given a page of one byte, it accepts a bool at the start",
			bufSize: 1,
			offset:  0,
			wantErr: false,
		},
		{
			name:    "when the offset is the end of the page, then the write is refused",
			bufSize: 1,
			offset:  1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetBool(tt.offset, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetBool() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetGetDate(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		value  time.Time
	}{
		{
			name:   "when a date is written and read back, then it names the same instant",
			offset: 0,
			value:  time.Unix(1000000, 0),
		},
		{
			name:   "when the Unix epoch is written and read back, then it is kept rather than read as unset",
			offset: 0,
			value:  time.Unix(0, 0),
		},
		{
			name:   "when a date is written past the start of the page, then it is read back from there",
			offset: 8,
			value:  time.Unix(1700000000, 0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(64)
			if err := p.SetDate(tt.offset, tt.value); err != nil {
				t.Fatalf("SetDate() error = %v", err)
			}
			if got := p.GetDate(tt.offset); !got.Equal(tt.value) {
				t.Errorf("GetDate() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestSetDate(t *testing.T) {
	tests := []struct {
		name    string
		bufSize int
		offset  int
		wantErr bool
	}{
		{
			name:    "given a page with room for exactly one date, it accepts a write at the start",
			bufSize: 8,
			offset:  0,
			wantErr: false,
		},
		{
			name:    "when a date would run past the end of the page, then the write is refused",
			bufSize: 8,
			offset:  1,
			wantErr: true,
		},
		{
			name:    "when the offset is the end of the page, then the write is refused",
			bufSize: 8,
			offset:  8,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPageByBlockSize(tt.bufSize)
			err := p.SetDate(tt.offset, time.Unix(0, 0))
			if (err != nil) != tt.wantErr {
				t.Errorf("SetDate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMaxLength(t *testing.T) {
	tests := []struct {
		name   string
		strlen int
		want   int
	}{
		{
			name:   "given a limit of no characters, it is the length prefix alone",
			strlen: 0,
			want:   4,
		},
		{
			name:   "given a limit of one character, it is the length prefix plus the widest encoding of one",
			strlen: 1,
			want:   4 + utf8.UTFMax,
		},
		{
			name:   "given a limit of ten characters, it is the length prefix plus ten times the widest encoding",
			strlen: 10,
			want:   4 + 10*utf8.UTFMax,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxLength(tt.strlen); got != tt.want {
				t.Errorf("MaxLength(%d) = %d, want %d", tt.strlen, got, tt.want)
			}
		})
	}
}
