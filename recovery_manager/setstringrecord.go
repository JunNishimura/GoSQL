package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// SetStringRecord records the value a string held before a transaction
// overwrote it. It shares the header of a set int record and differs only in
// the trailing value, which is length-prefixed rather than a fixed four bytes.
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

// Undo puts the overwritten value back. Pinning the block that p belongs to and
// marking the buffer modified are the caller's job.
func (r *SetStringRecord) Undo(p *filemanager.Page) error {
	if err := p.SetString(r.offset, r.oldVal); err != nil {
		return fmt.Errorf("restore string at offset %d of %s: %w", r.offset, r.blk, err)
	}
	return nil
}

// String quotes the value so that an empty one does not read as a missing field.
func (r *SetStringRecord) String() string {
	return fmt.Sprintf("<SETSTRING %d %s %d %q>", r.txNum, r.blk, r.offset, r.oldVal)
}

// WriteSetStringRecordToLog appends a record of the value at offset in blk, as
// it stood before txNum overwrote it, and returns its LSN.
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
