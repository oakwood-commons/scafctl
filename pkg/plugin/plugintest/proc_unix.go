// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package plugintest

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// psTimeout bounds the identity check run before a leaked process is killed.
const psTimeout = 5 * time.Second

// processAlive reports whether a process with the given ID exists. Signal 0
// performs the existence and permission checks without delivering a signal.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func killProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// processIs reports whether the process's command line contains binary, the
// full path the fixture was started from. It guards the cleanup kill against a
// recycled process ID: only a process still running the fixture binary is
// killed. The full path, which lives in a directory made for this run, is
// matched rather than the file name, which another program could share.
func processIs(pid int, binary string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=").Output() //nolint:gosec // pid is an integer
	if err != nil {
		return false
	}
	return strings.Contains(string(out), binary)
}
