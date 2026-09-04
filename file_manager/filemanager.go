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

// Archive moves fileName out of the database directory to destPath, creating
// the directories leading up to it. The name is then free: reading or writing
// it again starts an empty file.
//
// The handle is closed and forgotten first. The manager keeps every file it has
// opened, and a handle follows the file it was opened on rather than the path,
// so a forgotten one would keep pointing at the file that was moved away.
//
// An existing destPath is refused rather than replaced. Archiving is done to
// keep the old file, so quietly writing over an earlier one would defeat it.
func (f *FileManager) Archive(fileName, destPath string) error {
	if file, ok := f.openFiles[fileName]; ok {
		if err := file.Close(); err != nil {
			return fmt.Errorf("close file %s: %w", fileName, err)
		}
		delete(f.openFiles, fileName)
	}

	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("archive %s: %s already exists", fileName, destPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", destPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), os.ModePerm); err != nil {
		return fmt.Errorf("create directory %s: %w", filepath.Dir(destPath), err)
	}

	if err := os.Rename(filepath.Join(f.dbDirectory, fileName), destPath); err != nil {
		return fmt.Errorf("move %s to %s: %w", fileName, destPath, err)
	}

	return nil
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
