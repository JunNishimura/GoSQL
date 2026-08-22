package recoverymanager

import (
	"fmt"

	filemanager "github.com/JunNishimura/GoSQL/file_manager"
	logmanager "github.com/JunNishimura/GoSQL/log_manager"
)

// A set int record is laid out as the op code, the transaction number, the name
// of the file, the block number within it, the offset of the value inside that
// block, and finally the value that was overwritten.
//
// The file name is stored inline as a length-prefixed string, so every field
// after it sits at an offset that is only known once the name has been read.
const fileNameOffset = 2 * filemanager.IntBytes

// SetIntRecord records the value an int held before a transaction overwrote it.
type SetIntRecord struct {
	txNum  int
	blk    *filemanager.BlockId
	offset int
	oldVal int32
}

func NewSetIntRecord(record []byte) (*SetIntRecord, error) {
	txNum, blk, rest, err := readSetRecordHeader(record, SetInt)
	if err != nil {
		return nil, err
	}

	if len(record) < rest+2*filemanager.IntBytes {
		return nil, fmt.Errorf("set int record is %d bytes, want at least %d", len(record), rest+2*filemanager.IntBytes)
	}

	p := filemanager.NewPageByBytes(record)
	return &SetIntRecord{
		txNum:  txNum,
		blk:    blk,
		offset: int(p.GetInt(rest)),
		oldVal: p.GetInt(rest + filemanager.IntBytes),
	}, nil
}

func (r *SetIntRecord) Op() Op {
	return SetInt
}

func (r *SetIntRecord) TxNumber() int {
	return r.txNum
}

func (r *SetIntRecord) Block() *filemanager.BlockId {
	return r.blk
}

// Undo puts the overwritten value back. Pinning the block that p belongs to and
// marking the buffer modified are the caller's job.
func (r *SetIntRecord) Undo(p *filemanager.Page) error {
	if err := p.SetInt(r.offset, r.oldVal); err != nil {
		return fmt.Errorf("restore int at offset %d of %s: %w", r.offset, r.blk, err)
	}
	return nil
}

func (r *SetIntRecord) String() string {
	return fmt.Sprintf("<SETINT %d %s %d %d>", r.txNum, r.blk, r.offset, r.oldVal)
}

// WriteSetIntRecordToLog appends a record of the value at offset in blk, as it
// stood before txNum overwrote it, and returns its LSN.
func WriteSetIntRecordToLog(lm *logmanager.LogManager, txNum int, blk *filemanager.BlockId, offset int, oldVal int32) (int, error) {
	record, err := newSetIntRecord(txNum, blk, offset, oldVal)
	if err != nil {
		return 0, err
	}
	return lm.Append(record)
}

// newSetIntRecord encodes a set int record.
func newSetIntRecord(txNum int, blk *filemanager.BlockId, offset int, oldVal int32) ([]byte, error) {
	blkNumOffset := fileNameOffset + filemanager.IntBytes + len(blk.FileName())
	offsetOffset := blkNumOffset + filemanager.IntBytes
	oldValOffset := offsetOffset + filemanager.IntBytes

	record := make([]byte, oldValOffset+filemanager.IntBytes)
	p := filemanager.NewPageByBytes(record)

	writes := []struct {
		what string
		set  func() error
	}{
		{"op", func() error { return p.SetInt(opOffset, int32(SetInt)) }},
		{"txNum", func() error { return p.SetInt(txNumOffset, int32(txNum)) }},
		{"file name", func() error { return p.SetString(fileNameOffset, blk.FileName()) }},
		{"block number", func() error { return p.SetInt(blkNumOffset, int32(blk.Number())) }},
		{"offset", func() error { return p.SetInt(offsetOffset, int32(offset)) }},
		{"old value", func() error { return p.SetInt(oldValOffset, oldVal) }},
	}
	for _, w := range writes {
		if err := w.set(); err != nil {
			return nil, fmt.Errorf("set %s of set int record: %w", w.what, err)
		}
	}

	return record, nil
}

// readSetRecordHeader decodes the part shared by the records that overwrote a
// value: which transaction, and which block. It returns the offset just past
// the header, where the record's own fields begin.
//
// Every read is bounds checked first. Page.GetInt reads past the end of a short
// slice, and Page.GetString trusts the length prefix it finds, so a truncated
// record would otherwise be decoded into a plausible-looking one.
func readSetRecordHeader(record []byte, op Op) (txNum int, blk *filemanager.BlockId, rest int, err error) {
	if len(record) < fileNameOffset+filemanager.IntBytes {
		return 0, nil, 0, fmt.Errorf("log record for op %d is %d bytes, too short to hold a file name", op, len(record))
	}

	p := filemanager.NewPageByBytes(record)
	nameLength := int(p.GetInt(fileNameOffset))
	if nameLength < 0 || len(record) < fileNameOffset+filemanager.IntBytes+nameLength {
		return 0, nil, 0, fmt.Errorf("log record for op %d claims a file name of %d bytes but holds %d bytes in total", op, nameLength, len(record))
	}

	blkNumOffset := fileNameOffset + filemanager.IntBytes + nameLength
	if len(record) < blkNumOffset+filemanager.IntBytes {
		return 0, nil, 0, fmt.Errorf("log record for op %d is %d bytes, too short to hold a block number", op, len(record))
	}

	blk = filemanager.NewBlockId(p.GetString(fileNameOffset), int(p.GetInt(blkNumOffset)))
	return int(p.GetInt(txNumOffset)), blk, blkNumOffset + filemanager.IntBytes, nil
}
