package filemanager

import (
	"fmt"
	"hash/fnv"
)

type BlockId struct {
	filename string
	blkNum   int
}

func NewBlockId(fileName string, blkNum int) *BlockId {
	return &BlockId{
		filename: fileName,
		blkNum:   blkNum,
	}
}

func (b *BlockId) FileName() string {
	return b.filename
}

func (b *BlockId) Number() int {
	return b.blkNum
}

func (b *BlockId) Equals(other *BlockId) bool {
	return b.filename == other.filename && b.blkNum == other.blkNum
}

func (b *BlockId) String() string {
	return fmt.Sprintf("[file %s, block %d]", b.filename, b.blkNum)
}

func (b *BlockId) HashCode() int {
	h := fnv.New32a()
	h.Write([]byte(b.String()))
	return int(h.Sum32())
}
