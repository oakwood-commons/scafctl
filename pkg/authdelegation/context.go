// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import "context"

type registryKey struct{}

// WithRegistry returns a context that carries the given DelegatorRegistry.
func WithRegistry(ctx context.Context, reg *DelegatorRegistry) context.Context {
	return context.WithValue(ctx, registryKey{}, reg)
}

// RegistryFromContext retrieves the DelegatorRegistry from ctx.
// Returns nil if no registry has been stored.
func RegistryFromContext(ctx context.Context) *DelegatorRegistry {
	reg, _ := ctx.Value(registryKey{}).(*DelegatorRegistry)
	return reg
}
