package config

import (
	"context"
)

// configContextKey is the context key for storing Config.
type configContextKey struct{}

// WithConfig returns a new context with the Config stored in it.
func WithConfig(ctx context.Context, cfg *Config) context.Context {
	return context.WithValue(ctx, configContextKey{}, cfg)
}

// FromContext retrieves the Config from the context.
// Returns nil if no Config is stored in the context.
func FromContext(ctx context.Context) *Config {
	cfg, _ := ctx.Value(configContextKey{}).(*Config)
	return cfg
}

// MustFromContext retrieves the Config from the context.
// Panics if no Config is stored in the context.
func MustFromContext(ctx context.Context) *Config {
	cfg := FromContext(ctx)
	if cfg == nil {
		panic("config not found in context")
	}
	return cfg
}
