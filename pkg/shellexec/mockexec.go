// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package shellexec

import "context"

// RunFunc is a function signature matching Run, allowing mock implementations.
type RunFunc func(ctx context.Context, opts *RunOptions) (*RunResult, error)

type contextKey struct{}

// WithRunFunc returns a context with a custom RunFunc injected.
// When present, RunWithContext will use this function instead of the real Run.
func WithRunFunc(ctx context.Context, fn RunFunc) context.Context {
	return context.WithValue(ctx, contextKey{}, fn)
}

// RunFuncFromContext returns a RunFunc from the context, if one was injected.
func RunFuncFromContext(ctx context.Context) (RunFunc, bool) {
	fn, ok := ctx.Value(contextKey{}).(RunFunc)
	return fn, ok
}

// RunWithContext executes a command, but first checks if a mock RunFunc
// was injected via WithRunFunc. This enables exec mocking in tests.
func RunWithContext(ctx context.Context, opts *RunOptions) (*RunResult, error) {
	if fn, ok := RunFuncFromContext(ctx); ok {
		return fn(ctx, opts)
	}
	return Run(ctx, opts)
}
