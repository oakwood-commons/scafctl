// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"context"
)

type contextKey string

const (
	settingsContextKey contextKey = "settings"
	mockedResolversKey contextKey = "mocked_resolvers_file"
)

// IntoContext stores a Settings object in the context
func IntoContext(ctx context.Context, s *Run) context.Context {
	return context.WithValue(ctx, settingsContextKey, s)
}

// FromContext retrieves a Settings object from the context
func FromContext(ctx context.Context) (*Run, bool) {
	val := ctx.Value(settingsContextKey)
	s, ok := val.(*Run)
	return s, ok
}

// BinaryNameFromContext returns the configured binary name from context.
// Returns CliBinaryName when settings are absent or BinaryName is empty.
func BinaryNameFromContext(ctx context.Context) string {
	if s, ok := FromContext(ctx); ok && s.BinaryName != "" {
		return s.BinaryName
	}
	return CliBinaryName
}

// WithMockedResolversFile stores the path to a mocked resolvers JSON file in the context.
// Used by the test runner to pass mock data to in-process command execution without
// relying on process-global environment variables.
func WithMockedResolversFile(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, mockedResolversKey, path)
}

// MockedResolversFileFromContext returns the mocked resolvers file path from
// context, if set. Returns empty string and false when not present.
func MockedResolversFileFromContext(ctx context.Context) (string, bool) {
	val, ok := ctx.Value(mockedResolversKey).(string)
	return val, ok && val != ""
}
