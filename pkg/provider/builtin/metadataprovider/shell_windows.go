// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package metadataprovider

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ppidFunc returns the parent process ID. Overridable for testing.
var ppidFunc = os.Getppid

// parentProcessName returns the executable name of the parent process
// on Windows using the Toolhelp32 snapshot API.
// Returns an empty string if the parent process cannot be determined.
func parentProcessName() string {
	ppid := uint32(ppidFunc())
	if ppid == 0 {
		return ""
	}

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap) //nolint:errcheck

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	err = windows.Process32First(snap, &entry)
	for err == nil {
		if entry.ProcessID == ppid {
			name := windows.UTF16ToString(entry.ExeFile[:])
			return strings.TrimSpace(name)
		}
		err = windows.Process32Next(snap, &entry)
	}

	return ""
}
