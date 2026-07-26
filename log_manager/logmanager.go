package logmanager

import (
	"github.com/JunNishimura/GoSQL/file_manager"
)

type LogManager struct {
	fileManager *filemanager.FileManager
	logFile     string
	logPage     *filemanager.Page
}

func NewLogManager(fm *filemanager.FileManager, logFile string) *LogManager {
	return &LogManager{
		fileManager: fm,
		logFile:     logFile,
		logPage:     filemanager.NewPageByBlockSize(fm.BlockSize()),
	}
}
