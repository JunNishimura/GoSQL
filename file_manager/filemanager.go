package filemanager

import (
	"io"
	"os"
	"path/filepath"
)

type FileManager struct {
	dbDirectory string
	blockSize   int
	openFiles   map[string]*os.File
}

func NewFileManager(dbDir string, blockSize int) (*FileManager, error) {
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
			return nil, err
		}
	}
	return &FileManager{
		dbDirectory: dbDir,
		blockSize:   blockSize,
		openFiles:   make(map[string]*os.File),
	}, nil
}

func (f *FileManager) Read(blockId *BlockId, p *Page) error {
	file, err := f.getFile(blockId.filename)
	if err != nil {
		return err
	}

	offset := int64(blockId.blkNum) * int64(f.blockSize)
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	_, err = io.ReadFull(file, p.contents())
	return err
}

func (f *FileManager) getFile(fileName string) (*os.File, error) {
	file, ok := f.openFiles[fileName]
	if !ok {
		var err error
		file, err = os.OpenFile(filepath.Join(f.dbDirectory, fileName), os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			return nil, err
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
		return err
	}

	return nil
}

func (f *FileManager) Append(fileName string) (*BlockId, error) {
	file, err := f.getFile(fileName)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	newBlockNum := int(info.Size()) / f.blockSize

	newBytes := make([]byte, f.blockSize)
	offset := int64(newBlockNum) * int64(f.blockSize)
	if _, err := file.WriteAt(newBytes, offset); err != nil {
		return nil, err
	}

	return NewBlockId(fileName, newBlockNum), nil
}

func (f *FileManager) Length(fileName string) (int, error) {
	file, err := f.getFile(fileName)
	if err != nil {
		return 0, err
	}

	info, err := file.Stat()
	if err != nil {
		return 0, err
	}

	return int(info.Size()) / f.blockSize, nil
}
