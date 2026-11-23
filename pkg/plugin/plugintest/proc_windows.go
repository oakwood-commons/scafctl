// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package plugintest

import "os"

// processAlive reports whether a process with the given ID exists. On Windows
// FindProcess opens a handle and fails when the process is gone.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}

func killProcess(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// processIs cannot cheaply inspect another process's command line on Windows,
// so it trusts the process ID.
func processIs(int, string) bool {
	return true
}
