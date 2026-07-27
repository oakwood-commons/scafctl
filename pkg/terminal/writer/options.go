// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package writer

// Option is a functional option for configuring a Writer.
type Option func(*Writer)

// WithExitFunc sets a custom exit function.
// Useful for testing to capture exit calls instead of actually exiting.
func WithExitFunc(fn func(code int)) Option {
	return func(w *Writer) {
		w.exitFunc = fn
	}
}

// WithHumanToStderr routes human-facing messages (success, info, warning,
// section header, debug, and plain output) to stderr instead of stdout.
//
// Use this for commands that emit structured data on stdout (e.g., -o json/yaml)
// so progress and status noise never corrupts the machine-readable stream.
// Error output always goes to stderr regardless of this option.
func WithHumanToStderr() Option {
	return func(w *Writer) {
		w.humanToStderr = true
	}
}
