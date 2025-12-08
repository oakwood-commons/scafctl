package profiler

import (
	"os"
)

type runtimeProfiler interface {
	Start() error
	Stop() error
	writeProfile() (*os.File, error)
	getFiles() []*os.File
	cleanUp()
}

type Proxy struct {
	profiler    runtimeProfiler
	profileType string
	path        string
	stopCh      chan struct{}
	finalPath   string
}

type memoryProfiler struct {
	fileManager *fileManager
}

type cpuProfiler struct {
	fileManager *fileManager
	active      bool
}

type goroutineProfiler struct {
	fileManager *fileManager
}
