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
