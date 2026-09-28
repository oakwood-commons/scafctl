// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package input

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
	// ioctlFlushQueue drives the TIOCFLUSH ioctl; the arg below selects
	// flushing of received-but-unread input only.
	ioctlFlushQueue   = unix.TIOCFLUSH
	flushDiscardInput = unix.TCIFLUSH
)
