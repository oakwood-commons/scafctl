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

// MustRegistryFromContext retrieves the auth registry from the context.
// Panics if no registry is attached.
func MustRegistryFromContext(ctx context.Context) *Registry {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		panic("auth registry not found in context")
	}
	return registry
}

// GetHandler gets an auth handler from the context's registry.
func GetHandler(ctx context.Context, name string) (Handler, error) {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return nil, fmt.Errorf("%w: no auth registry in context", ErrHandlerNotFound)
	}
	return registry.Get(name)
}

// HasHandler checks if an auth handler exists in the context's registry.
func HasHandler(ctx context.Context, name string) bool {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return false
	}
	return registry.Has(name)
}

// ListHandlers lists all auth handlers in the context's registry.
func ListHandlers(ctx context.Context) []string {
	registry := RegistryFromContext(ctx)
	if registry == nil {
		return nil
	}
	return registry.List()
}
