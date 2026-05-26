// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
)

type contextKey string

const registryKey contextKey = "auth.registry"

// WithRegistry returns a new context with the auth registry attached.
func WithRegistry(ctx context.Context, registry *Registry) context.Context {
	return context.WithValue(ctx, registryKey, registry)
}

// RegistryFromContext retrieves the auth registry from the context.
func RegistryFromContext(ctx context.Context) *Registry {
	registry, _ := ctx.Value(registryKey).(*Registry)
	return registry
}

// GetHandler gets an auth handler from the context's registry.
// The name can include a profile suffix using "handler@profile" syntax.
// The profile portion is stripped for registry lookup; use ParseProfileKey
// to extract the profile for passing via context to handler methods.
func GetHandler(ctx context.Context, name string) (Handler, error) {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("%w: no auth registry in context", ErrHandlerNotFound)
	}
	handlerName, _ := ParseProfileKey(name)
	return registry.Get(handlerName)
}

// HasHandler checks if an auth handler exists in the context's registry.
// The name can include a profile suffix using "handler@profile" syntax.
func HasHandler(ctx context.Context, name string) bool {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return false
	}
	handlerName, _ := ParseProfileKey(name)
	return registry.Has(handlerName)
}

// ListHandlers lists all auth handlers in the context's registry.
func ListHandlers(ctx context.Context) []string {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return nil
	}
	return registry.List()
}
