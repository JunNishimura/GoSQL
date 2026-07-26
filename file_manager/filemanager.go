package filemanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Stats struct {
	blocksRead    int
	blocksWritten int
}

func (s *Stats) BlocksRead() int {
	return s.blocksRead
}

func (s *Stats) BlocksWritten() int {
	return s.blocksWritten
}

type FileManager struct {
	dbDirectory string
	blockSize   int
	openFiles   map[string]*os.File
	stats       Stats
}

func NewFileManager(dbDir string, blockSize int) (*FileManager, error) {
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", dbDir, err)
		}
	}
	return &FileManager{
		dbDirectory: dbDir,
		blockSize:   blockSize,
		openFiles:   make(map[string]*os.File),
	}, nil
}

func (f *FileManager) BlockSize() int {
	return f.blockSize
}

func (f *FileManager) Read(blockId *BlockId, p *Page) error {
	file, err := f.getFile(blockId.filename)
	if err != nil {
		return err
	}

	offset := int64(blockId.blkNum) * int64(f.blockSize)
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return fmt.Errorf("seek block %s: %w", blockId, err)
	}

	if _, err = io.ReadFull(file, p.contents()); err != nil {
		return fmt.Errorf("read block %s: %w", blockId, err)
	}
	f.stats.blocksRead++
	return nil
}

func (f *FileManager) getFile(fileName string) (*os.File, error) {
	file, ok := f.openFiles[fileName]
	if !ok {
		var err error
		file, err = os.OpenFile(filepath.Join(f.dbDirectory, fileName), os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			return nil, fmt.Errorf("open file %s: %w", filepath.Join(f.dbDirectory, fileName), err)
		}
		f.openFiles[fileName] = file
	}
	return file, nil
}

func (f *FileManager) Write(blockId *BlockId, p *Page) error {
	file, err := f.getFile(blockId.filename)
	if err != nil {
		return err
	}

	offset := int64(blockId.blkNum) * int64(f.blockSize)
	if _, err := file.WriteAt(p.contents(), offset); err != nil {
		return fmt.Errorf("write block %s: %w", blockId, err)
	}
	f.stats.blocksWritten++
	return nil
}

func (f *FileManager) Append(fileName string) (*BlockId, error) {
	file, err := f.getFile(fileName)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", fileName, err)
	}
	newBlockNum := int(info.Size()) / f.blockSize

	newBytes := make([]byte, f.blockSize)
	offset := int64(newBlockNum) * int64(f.blockSize)
	if _, err := file.WriteAt(newBytes, offset); err != nil {
		return nil, fmt.Errorf("append block to %s: %w", fileName, err)
	}

	return NewBlockId(fileName, newBlockNum), nil
}

func (f *FileManager) GetStats() Stats {
	return f.stats
}

func (f *FileManager) Length(fileName string) (int, error) {
	file, err := f.getFile(fileName)
	if err != nil {
		return 0, err
	}

	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat file %s: %w", fileName, err)
	}

	return int(info.Size()) / f.blockSize, nil
}
