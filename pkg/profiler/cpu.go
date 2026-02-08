// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package profiler

import (
	"fmt"
	"os"
	"runtime/pprof"
)

func (c *cpuProfiler) Start() error {
	if c.active {
		return nil
	}

	f, err := c.writeProfile()
	if err != nil {
		return err
	}

	c.fileManager.addFile(f)
	c.active = true

	return nil
}

func (c *cpuProfiler) getFiles() []*os.File {
	return c.fileManager.getFiles()
}

func (c *cpuProfiler) Stop() error {
	pprof.StopCPUProfile()
	return nil
}

func (c *cpuProfiler) writeProfile() (*os.File, error) {
	f, err := c.fileManager.createTemp()
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file for cpu profile: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to start cpu profile: %w", err)
	}

	return f, nil
}

func (c *cpuProfiler) cleanUp() {
	c.fileManager.cleanUp()
	c.active = false
}
