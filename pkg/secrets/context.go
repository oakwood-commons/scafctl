// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import "context"

type storeContextKey struct{}

// WithStore returns a new context carrying store.
func WithStore(ctx context.Context, store Store) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, storeContextKey{}, store)
}

// FromContext retrieves the secret Store attached to ctx, or nil if none is set.
func FromContext(ctx context.Context) Store {
	if ctx == nil {
		return nil
	}
	store, _ := ctx.Value(storeContextKey{}).(Store)
	return store
}
