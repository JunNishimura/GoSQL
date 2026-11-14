package filemanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// FileManager is safe for concurrent use. The buffer manager and the log
// manager each call it under their own lock, so mu is the only lock that
// protects openFiles, unsynced and stats from concurrent access.
type FileManager struct {
	dbDirectory string
	blockSize   int
	openFiles   map[string]*os.File
	// unsynced holds the names of the files written since their last sync. A
	// write only reaches the OS page cache, so an OS crash can still lose the
	// latest blocks of these files.
	unsynced map[string]struct{}
	// dirUnsynced reports whether a file has been created in the database
	// directory since the directory was last synced. Syncing a new file makes
	// its contents durable, but not the directory entry that names it.
	dirUnsynced bool
	stats       Stats
	isNew       bool
	mu          sync.Mutex
}

// TempFilePrefix is the prefix of the name of every temp table file.
//
// It begins with '#', which the SQL tokenizer never accepts in a word. No
// statement can therefore name a table that starts with it. A user table
// cannot share a file with a temp table, and only temp tables have such names.
//
// A temp table does not outlive the query that created it. A temp file found
// when a file manager is created was therefore left by a database that
// stopped before it could remove the file.
const TempFilePrefix = "#temp"

// dirName is the name that IsSynced accepts for the database directory
// itself: the path of the directory relative to itself.
const dirName = "."

func NewFileManager(dbDir string, blockSize int) (*FileManager, error) {
	if err := os.MkdirAll(dbDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dbDir, err)
	}
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", dbDir, err)
	}

	// Temp files are removed before the check for a new directory. A
	// directory that held only temp files then counts as empty.
	kept := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), TempFilePrefix) {
			kept++
			continue
		}
		path := filepath.Join(dbDir, entry.Name())
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove temp file %s: %w", path, err)
		}
	}

	return &FileManager{
		dbDirectory: dbDir,
		blockSize:   blockSize,
		openFiles:   make(map[string]*os.File),
		unsynced:    make(map[string]struct{}),
		isNew:       kept == 0,
	}, nil
}

// IsNew reports whether the database directory was empty when the file
// manager was created, either because it did not exist or because it held no
// files. A database that starts up uses this to decide whether to recover its
// catalogs or to create them.
//
// Every file counts, including hidden ones. Distinguishing database files
// from other files would require knowing their names, and this layer does not
// know them.
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

// getFile returns the handle for fileName, opening it the first time and
// creating it if it does not exist. The caller must hold mu.
func (f *FileManager) getFile(fileName string) (*os.File, error) {
	if file, ok := f.openFiles[fileName]; ok {
		return file, nil
	}

	path := filepath.Join(f.dbDirectory, fileName)
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat file %s: %w", path, err)
	}
	created := os.IsNotExist(err)

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, fmt.Errorf("open file %s: %w", path, err)
	}
	f.openFiles[fileName] = file
	if created {
		f.dirUnsynced = true
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

// Archive moves fileName out of the database directory to destPath, and
// creates the parent directories of destPath as needed. The name is then
// free: the next read or write of it starts an empty file.
//
// The file is synced first, so that the archive is complete on disk. Its
// handle is then closed and dropped from openFiles. A handle refers to the
// file it was opened on, not to the path. A handle kept in openFiles would
// therefore still refer to the moved file.
//
// After the move, every directory that the move changed is synced. These are
// the database directory, which no longer names the file, and each directory
// from the destination up to the first one that already existed.
//
// Archive returns an error if destPath already exists. The purpose of an
// archive is to keep the old file, so it must not overwrite an earlier one.
func (f *FileManager) Archive(fileName, destPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.syncFile(fileName); err != nil {
		return err
	}
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

	destDir := filepath.Dir(destPath)
	existing, err := existingAncestor(destDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, os.ModePerm); err != nil {
		return fmt.Errorf("create directory %s: %w", destDir, err)
	}

	if err := os.Rename(filepath.Join(f.dbDirectory, fileName), destPath); err != nil {
		return fmt.Errorf("move %s to %s: %w", fileName, destPath, err)
	}

	if err := syncDir(f.dbDirectory); err != nil {
		return err
	}
	f.dirUnsynced = false
	for dir := destDir; ; dir = filepath.Dir(dir) {
		if err := syncDir(dir); err != nil {
			return err
		}
		if dir == existing {
			break
		}
	}

	return nil
}

// existingAncestor returns dir if it exists, or else its closest ancestor
// that exists.
func existingAncestor(dir string) (string, error) {
	for {
		_, err := os.Stat(dir)
		if err == nil {
			return dir, nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", dir, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, nil
		}
		dir = parent
	}
}

// syncDir syncs the directory at path, which makes its entries durable.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s: %w", path, err)
	}
	defer dir.Close()

	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}

// Close syncs and closes every open file and drops its handle. A later use of
// the manager then opens each file again instead of using a closed handle.
//
// Each file is synced before it is closed, because the manager cannot sync a
// file without its handle. The database directory is synced after the files.
//
// Every handle is closed and dropped even if a sync or a close fails, because
// a handle that failed to close is not usable either. The errors are returned
// together.
func (f *FileManager) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var errs []error
	for fileName, file := range f.openFiles {
		if err := f.syncFile(fileName); err != nil {
			errs = append(errs, err)
		}
		if err := file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close file %s: %w", fileName, err))
		}
		delete(f.openFiles, fileName)
	}
	if err := f.syncDBDir(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// IsSynced reports whether everything written to fileName has been synced to
// disk. A file that has never been written counts as synced, because a crash
// cannot lose anything from it.
//
// For ".", it reports on the database directory instead: whether the entry of
// every file created in it is on disk.
func (f *FileManager) IsSynced(fileName string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if fileName == dirName {
		return !f.dirUnsynced
	}
	_, ok := f.unsynced[fileName]
	return !ok
}

// Sync makes everything written to fileName durable, so that it survives a
// crash of the OS and not only of the process. It does nothing for a file
// with no unsynced writes.
//
// The database directory is also synced if a file has been created in it. A
// new file whose directory entry is not on disk cannot be found after a
// crash.
func (f *FileManager) Sync(fileName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.syncFile(fileName); err != nil {
		return err
	}
	return f.syncDBDir()
}

// SyncAll syncs every file with unsynced writes, and the database directory
// if a file has been created in it.
//
// It covers the files of all transactions, not only the caller's. The buffer
// manager can write a modified block early to free its buffer, and nothing
// records which transaction modified that block.
func (f *FileManager) SyncAll() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for fileName := range f.unsynced {
		if err := f.syncFile(fileName); err != nil {
			return err
		}
	}
	return f.syncDBDir()
}

// syncDBDir syncs the database directory if a file has been created in it
// since it was last synced. The caller must hold mu.
func (f *FileManager) syncDBDir() error {
	if !f.dirUnsynced {
		return nil
	}
	if err := syncDir(f.dbDirectory); err != nil {
		return err
	}
	f.dirUnsynced = false
	return nil
}

// syncFile syncs fileName if it has unsynced writes. The caller must hold mu.
func (f *FileManager) syncFile(fileName string) error {
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
