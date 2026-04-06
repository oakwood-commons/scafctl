// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"context"
)

type contextKey string

const (
	settingsContextKey contextKey = "settings"
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
