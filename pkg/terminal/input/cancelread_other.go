// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build !(linux || darwin)

package input

import (
	"context"
	"errors"
	"io"
)

// interactiveLineSupported reports whether this platform implements
// cancellation-safe terminal reads.
const interactiveLineSupported = false

// ReadInteractiveLine is unavailable on this platform; callers must check
// InteractiveLineSupported before installing a prompt that relies on it.
//
//nolint:revive // ctx/echo params keep the signature uniform across platforms
func ReadInteractiveLine(_ context.Context, _ io.Reader, _ io.Writer) (string, error) {
	return "", errors.New("interactive paste-back input is not supported on this platform")
}
