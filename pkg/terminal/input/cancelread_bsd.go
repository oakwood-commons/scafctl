// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package input

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)

// flushInput discards received-but-unread terminal input. TIOCFLUSH takes
// its queue selector by pointer (unlike Linux TCFLSH, which takes it by
// value), so IoctlSetInt would fail silently here.
func flushInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH)
}
