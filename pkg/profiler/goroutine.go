package profiler

import (
	"fmt"
	"os"
	"runtime/pprof"
)

func (g *goroutineProfiler) Start() error {
	f, err := g.writeProfile()
	if err != nil {
		return fmt.Errorf("error starting goroutine profiler: %w", err)
	}

	g.fileManager.addFile(f)

	return nil
}

func (g *goroutineProfiler) writeProfile() (*os.File, error) {
	f, err := g.fileManager.createTemp()
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for goroutine profile: %w", err)
	}

	if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to write goroutine profile: %w", err)
	}

	return f, nil
}

func (g *goroutineProfiler) cleanUp() {
	g.fileManager.cleanUp()
}

func (g *goroutineProfiler) getFiles() []*os.File {
	return g.fileManager.getFiles()
}

func (g *goroutineProfiler) Stop() error {
	return nil
}
