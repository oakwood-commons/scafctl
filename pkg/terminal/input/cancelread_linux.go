// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package input

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TCGETS
	ioctlWriteTermios = unix.TCSETS
	// ioctlFlushQueue drives the TCFLSH ioctl; keep the flush constant here
	// rather than in the shared unix file because its value is also
	// per-platform.
	ioctlFlushQueue = unix.TCFLSH
	// flushDiscardInput flushes received-but-unread input only.
	flushDiscardInput = unix.TCIFLUSH
)
