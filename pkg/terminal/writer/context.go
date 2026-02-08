// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package writer

import "context"

type contextKey struct{}

// WithWriter returns a new context with the Writer attached.
func WithWriter(ctx context.Context, w *Writer) context.Context {
	return context.WithValue(ctx, contextKey{}, w)
}

// FromContext retrieves the Writer from the context.
// Returns nil if no Writer is present.
func FromContext(ctx context.Context) *Writer {
	w, _ := ctx.Value(contextKey{}).(*Writer)
	return w
}

// MustFromContext retrieves the Writer from the context.
// Panics if no Writer is present.
func MustFromContext(ctx context.Context) *Writer {
	w := FromContext(ctx)
	if w == nil {
		panic("writer: no Writer in context")
	}
	return w
}
