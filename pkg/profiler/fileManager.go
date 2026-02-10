// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package profiler

import "os"

type fileManager struct {
	files []*os.File
}

func (fm *fileManager) addFile(f *os.File) {
	fm.files = append(fm.files, f)
}

func (fm *fileManager) getFiles() []*os.File {
	return fm.files
}

func (fm *fileManager) createTemp() (*os.File, error) {
	return os.CreateTemp("", "*.pprof")
}

func (fm *fileManager) cleanUp() {
	for _, f := range fm.files {
		if f != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}
	fm.files = nil
}
