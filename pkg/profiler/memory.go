package profiler

import (
	"fmt"
	"os"
	"runtime/pprof"
)

func (m *memoryProfiler) Start() error {
	f, err := m.writeProfile()
	if err != nil {
		return err
	}

	m.fileManager.addFile(f)

	return nil
}

func (m *memoryProfiler) Stop() error {
	return nil
}

func (m *memoryProfiler) writeProfile() (*os.File, error) {
	f, err := m.fileManager.createTemp()
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for memory profile: %w", err)
	}

	if err := pprof.WriteHeapProfile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to write memory profile: %w", err)
	}

	return f, nil
}

func (m *memoryProfiler) getFiles() []*os.File {
	return m.fileManager.getFiles()
}

func (m *memoryProfiler) cleanUp() {
	m.fileManager.cleanUp()
}
