package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// SetStringRecord holds the value of a string before a transaction overwrote
// it. It has the same header as a set int record. Only the trailing value
// differs: it is a length-prefixed string instead of a fixed four bytes.
type SetStringRecord struct {
	txNum  int
	blk    *filemanager.BlockId
	offset int
	oldVal string
}

func NewSetStringRecord(record []byte) (*SetStringRecord, error) {
	txNum, blk, rest, err := readSetRecordHeader(record, SetString)
	if err != nil {
		return nil, err
	}

	oldValOffset := rest + filemanager.IntBytes
	if len(record) < oldValOffset+filemanager.IntBytes {
		return nil, fmt.Errorf("set string record is %d bytes, too short to hold the length of the value", len(record))
	}

	p := filemanager.NewPageByBytes(record)
	valLength := int(p.GetInt(oldValOffset))
	if valLength < 0 || len(record) < oldValOffset+filemanager.IntBytes+valLength {
		return nil, fmt.Errorf("set string record claims a value of %d bytes but holds %d bytes in total", valLength, len(record))
	}

	return &SetStringRecord{
		txNum:  txNum,
		blk:    blk,
		offset: int(p.GetInt(rest)),
		oldVal: p.GetString(oldValOffset),
	}, nil
}

func (r *SetStringRecord) Op() Op {
	return SetString
}

func (r *SetStringRecord) TxNumber() int {
	return r.txNum
}

func (r *SetStringRecord) Block() *filemanager.BlockId {
	return r.blk
}

// Undo restores the overwritten value. The caller must pin the block that p
// belongs to and mark the buffer as modified.
func (r *SetStringRecord) Undo(p *filemanager.Page) error {
	if err := p.SetString(r.offset, r.oldVal); err != nil {
		return fmt.Errorf("restore string at offset %d of %s: %w", r.offset, r.blk, err)
	}
	return nil
}

// String quotes the value, so that an empty value is not mistaken for a
// missing field.
func (r *SetStringRecord) String() string {
	return fmt.Sprintf("<SETSTRING %d %s %d %q>", r.txNum, r.blk, r.offset, r.oldVal)
}

// WriteSetStringRecordToLog appends a record for the string at offset in blk
// and returns its LSN. oldVal is the value before txNum overwrote it.
func WriteSetStringRecordToLog(lm *logmanager.LogManager, txNum int, blk *filemanager.BlockId, offset int, oldVal string) (int, error) {
	record, err := newSetStringRecord(txNum, blk, offset, oldVal)
	if err != nil {
		return 0, err
	}
	return lm.Append(record)
}

// newSetStringRecord encodes a set string record.
func newSetStringRecord(txNum int, blk *filemanager.BlockId, offset int, oldVal string) ([]byte, error) {
	blkNumOffset := fileNameOffset + filemanager.IntBytes + len(blk.FileName())
	offsetOffset := blkNumOffset + filemanager.IntBytes
	oldValOffset := offsetOffset + filemanager.IntBytes

	record := make([]byte, oldValOffset+filemanager.IntBytes+len(oldVal))
	p := filemanager.NewPageByBytes(record)

	writes := []struct {
		what string
		set  func() error
	}{
		{"op", func() error { return p.SetInt(opOffset, int32(SetString)) }},
		{"txNum", func() error { return p.SetInt(txNumOffset, int32(txNum)) }},
		{"file name", func() error { return p.SetString(fileNameOffset, blk.FileName()) }},
		{"block number", func() error { return p.SetInt(blkNumOffset, int32(blk.Number())) }},
		{"offset", func() error { return p.SetInt(offsetOffset, int32(offset)) }},
		{"old value", func() error { return p.SetString(oldValOffset, oldVal) }},
	}
	for _, w := range writes {
		if err := w.set(); err != nil {
			return nil, fmt.Errorf("set %s of set string record: %w", w.what, err)
		}
	}

	return record, nil
}
