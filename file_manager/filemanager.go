package filemanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// FileManager is safe to use from several goroutines at once. The buffer
// manager and the log manager each reach it under their own lock, so mu is
// what keeps them from interleaving on openFiles, unsynced and stats.
type FileManager struct {
	dbDirectory string
	blockSize   int
	openFiles   map[string]*os.File
	// unsynced names the files written since they were last synced. A write
	// only reaches the OS's page cache, so these are the files whose latest
	// blocks a crash of the OS could still lose.
	unsynced map[string]struct{}
	stats    Stats
	isNew    bool
	mu       sync.Mutex
}

func NewFileManager(dbDir string, blockSize int) (*FileManager, error) {
	if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dbDir, err)
	}
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dbDir, err)
	}
	return &FileManager{
		dbDirectory: dbDir,
		blockSize:   blockSize,
		openFiles:   make(map[string]*os.File),
		unsynced:    make(map[string]struct{}),
		isNew:       len(entries) == 0,
	}, nil
}

// IsNew reports whether the database directory held nothing when the file
// manager was made on it, either because it did not exist or because it was
// empty. That is what tells a database starting up that it has no catalogs to
// recover and has to make them instead.
//
// Any file at all counts, hidden ones too, since telling a database's files
// from anything else would mean knowing their names, which is not this
// layer's business.
func (f *FileManager) IsNew() bool {
	return f.isNew
}

func (f *FileManager) BlockSize() int {
	return f.blockSize
}

func (f *FileManager) Read(blockId *BlockId, p *Page) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	file, err := f.getFile(blockId.filename)
	if err != nil {
		return err
	}

	offset := int64(blockId.blkNum) * int64(f.blockSize)
	if _, err := file.ReadAt(p.contents(), offset); err != nil {
		return fmt.Errorf("read block %s: %w", blockId, err)
	}
	f.stats.blocksRead++
	return nil
}

// getFile returns the handle for fileName, opening it the first time. The
// caller must hold mu.
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
	f.mu.Lock()
	defer f.mu.Unlock()

	file, err := f.getFile(blockId.filename)
	if err != nil {
		return err
	}

	offset := int64(blockId.blkNum) * int64(f.blockSize)
	if _, err := file.WriteAt(p.contents(), offset); err != nil {
		return fmt.Errorf("write block %s: %w", blockId, err)
	}
	f.unsynced[blockId.filename] = struct{}{}
	f.stats.blocksWritten++
	return nil
}

func (f *FileManager) Append(fileName string) (*BlockId, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

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
	f.unsynced[fileName] = struct{}{}

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
	f.mu.Lock()
	defer f.mu.Unlock()

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

// Close syncs and closes every file the manager has opened and forgets them, so
// that using the manager afterwards opens each file afresh rather than going
// through a closed handle.
//
// Each file is synced before it is closed, since once its handle is forgotten
// nothing would be left to sync it through.
//
// Every handle is closed and forgotten even when one fails to sync or close,
// since a handle that could not be closed is no more usable than one that was,
// and the failures are reported together.
func (f *FileManager) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var errs []error
	for fileName, file := range f.openFiles {
		if err := f.sync(fileName); err != nil {
			errs = append(errs, err)
		}
		if err := file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close file %s: %w", fileName, err))
		}
		delete(f.openFiles, fileName)
	}
	return errors.Join(errs...)
}

// IsSynced reports whether everything written to fileName has been synced to
// disk. A file that has never been written counts as synced, since there is
// nothing of it a crash could lose.
func (f *FileManager) IsSynced(fileName string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, ok := f.unsynced[fileName]
	return !ok
}

// Sync puts everything written to fileName on disk, so that it survives a crash
// of the OS and not only of the process. A file with nothing unsynced is left
// alone.
func (f *FileManager) Sync(fileName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.sync(fileName)
}

// SyncAll syncs every file written since it was last synced.
//
// It covers the files of every transaction, not only of the caller's: a block
// a transaction changed may have been written out early to free its buffer, and
// nothing remembers which transaction that block belonged to.
func (f *FileManager) SyncAll() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for fileName := range f.unsynced {
		if err := f.sync(fileName); err != nil {
			return err
		}
	}
	return nil
}

// sync syncs fileName if it has anything unsynced. The caller must hold mu.
func (f *FileManager) sync(fileName string) error {
	if _, ok := f.unsynced[fileName]; !ok {
		return nil
	}

	file, err := f.getFile(fileName)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync file %s: %w", fileName, err)
	}
	delete(f.unsynced, fileName)
	return nil
}

func (f *FileManager) GetStats() Stats {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.stats
}

func (f *FileManager) Length(fileName string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

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
