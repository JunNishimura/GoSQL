package filemanager

import (
	"encoding/binary"
	"fmt"
	"unicode/utf8"
)

type Page struct {
	buf []byte
}

func NewPageByBlockSize(blockSize int) *Page {
	return &Page{buf: make([]byte, blockSize)}
}

func NewPageByBytes(bytes []byte) *Page {
	return &Page{buf: bytes}
}

func (p *Page) GetInt(offset int) int32 {
	return int32(binary.BigEndian.Uint32(p.buf[offset:]))
}

func (p *Page) SetInt(offset int, value int32) error {
	if offset+4 > len(p.buf) {
		return fmt.Errorf("SetInt: offset %d out of bounds (buf size %d)", offset, len(p.buf))
	}
	binary.BigEndian.PutUint32(p.buf[offset:], uint32(value))
	return nil
}

func (p *Page) GetBytes(offset int) []byte {
	length := int(binary.BigEndian.Uint32(p.buf[offset:]))
	result := make([]byte, length)
	copy(result, p.buf[offset+4:])
	return result
}

func (p *Page) SetBytes(offset int, bytes []byte) error {
	if offset+4+len(bytes) > len(p.buf) {
		return fmt.Errorf("SetBytes: offset %d with %d bytes out of bounds (buf size %d)", offset, len(bytes), len(p.buf))
	}
	binary.BigEndian.PutUint32(p.buf[offset:], uint32(len(bytes)))
	copy(p.buf[offset+4:], bytes)
	return nil
}

func (p *Page) GetString(offset int) string {
	return string(p.GetBytes(offset))
}

func (p *Page) SetString(offset int, value string) error {
	return p.SetBytes(offset, []byte(value))
}

func (p *Page) MaxLength(strlen int) int {
	return 4 + strlen*utf8.UTFMax
}

func (p *Page) contents() []byte {
	return p.buf
}
