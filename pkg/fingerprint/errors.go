// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import "errors"

var (
	// ErrNoMatches is returned when a glob pattern matches zero files.
	ErrNoMatches = errors.New("glob pattern matched no files")

	// ErrPatternInvalid is returned for a malformed glob pattern.
	ErrPatternInvalid = errors.New("invalid glob pattern")
)
