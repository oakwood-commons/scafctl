// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
)

// stateContextKey is the context key for storing StateData.
type stateContextKey struct{}

// WithState returns a new context with the StateData stored in it.
func WithState(ctx context.Context, data *Data) context.Context {
	return context.WithValue(ctx, stateContextKey{}, data)
}

// FromContext retrieves the StateData from the context.
// Returns the StateData and true if found, or nil and false if not present.
func FromContext(ctx context.Context) (*Data, bool) {
	data, ok := ctx.Value(stateContextKey{}).(*Data)
	return data, ok
}
