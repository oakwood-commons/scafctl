// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package gitcredential

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockAccountFile(f *os.File) error {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
		return errAccountLockBusy
	}
	return err
}

func unlockAccountFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
