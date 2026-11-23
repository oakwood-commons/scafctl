// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/plugin/plugintest"
)

// State files are written best effort: a failure is reported on stderr (which
// the host forwards to its plugin log) but never changes the plugin's
// behavior, so a broken sandbox cannot mask the behavior under test.
const (
	stateDirMode  = 0o750
	stateFileMode = 0o600
)

// stateDir is where this process records its state. It lives under the
// process temp directory, which the tests point at a per-test sandbox.
func stateDir() string {
	return filepath.Join(os.TempDir(), plugintest.StateDir)
}

func writePIDFile(dir string, pid int) {
	body := fmt.Sprintf("%d\n", pid)
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".pid"), []byte(body), stateFileMode); err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: write pid file: %v\n", err)
	}
}

func recordExit(dir string, pid int, reason string) {
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".exit"), []byte(reason+"\n"), stateFileMode); err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: write exit record: %v\n", err)
	}
}

// startWatchdog ends the process once its maximum lifetime has passed.
func startWatchdog(dir string, pid int, limit time.Duration) {
	time.AfterFunc(limit, func() {
		recordExit(dir, pid, plugintest.ExitWatchdog)
		os.Exit(0)
	})
}

// callLog appends one line per served RPC to <pid>.calls.
type callLog struct {
	mu   sync.Mutex
	path string
}

func (l *callLog) record(rpc, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, stateFileMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: open call log: %v\n", err)
		return
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, plugintest.FormatCall(plugintest.Call{RPC: rpc, Detail: detail})); err != nil {
		fmt.Fprintf(os.Stderr, "lifecycleplugin: write call log: %v\n", err)
	}
}
