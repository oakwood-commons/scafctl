// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package diskcache

import (
	"time"

	"golang.org/x/sys/windows"
)

const (
	// maxRenameRetries is the number of retry attempts for transient Windows errors.
	maxRenameRetries = 3
	// renameRetryDelay is the base delay between retries (doubled each attempt).
	renameRetryDelay = 5 * time.Millisecond
)

// atomicRename atomically replaces dst with src using MoveFileEx.
// MOVEFILE_REPLACE_EXISTING allows overwriting an existing target.
// MOVEFILE_WRITE_THROUGH ensures the operation is flushed to disk.
// Retries on transient errors (sharing violations, access denied) that
// occur when antivirus or indexing services hold brief locks on files.
func atomicRename(src, dst string) error {
	srcW, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstW, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}

	var lastErr error
	delay := renameRetryDelay
	for attempt := range maxRenameRetries {
		lastErr = windows.MoveFileEx(srcW, dstW, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if lastErr == nil {
			return nil
		}
		if !isTransientError(lastErr) {
			return lastErr
		}
		if attempt < maxRenameRetries-1 {
			time.Sleep(delay)
			delay *= 2
		}
	}
	return lastErr
}

// isTransientError returns true for Windows errors that are typically
// caused by brief file locks from antivirus, search indexers, or
// backup software and are likely to resolve on retry.
func isTransientError(err error) bool {
	switch err {
	case windows.ERROR_SHARING_VIOLATION,
		windows.ERROR_LOCK_VIOLATION,
		windows.ERROR_ACCESS_DENIED:
		return true
	default:
		return false
	}
}
