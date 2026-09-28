// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package input

// InteractiveLineSupported reports whether this platform can read a full
// terminal line in a cancellation-safe way (no uninterruptible blocking
// read, no leftover reader after cancellation). Interactive logins check
// this before offering the paste-back prompt.
func InteractiveLineSupported() bool {
	return interactiveLineSupported
}
