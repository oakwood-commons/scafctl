// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package tokenprovider

import (
	"context"
)

type contextKey struct{}

// WithRegistry returns a context carrying the token provider registry.
func WithRegistry(ctx context.Context, reg *Registry) context.Context {
	return context.WithValue(ctx, contextKey{}, reg)
}

// RegistryFromContext retrieves the token provider registry from ctx.
// Returns nil if no registry has been stored.
func RegistryFromContext(ctx context.Context) *Registry {
	reg, _ := ctx.Value(contextKey{}).(*Registry)
	return reg
}
