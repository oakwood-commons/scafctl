// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
)

// configContextKey is the context key for storing Config.
type configContextKey struct{}

// managerOptsContextKey is the context key for storing ManagerOptions.
type managerOptsContextKey struct{}

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

// WithManagerOptions stores ManagerOption values in the context so that
// subcommands which create their own Manager can apply the same embedder
// options (e.g., WithBaseConfig, WithEnvPrefix).
func WithManagerOptions(ctx context.Context, opts []ManagerOption) context.Context {
	return context.WithValue(ctx, managerOptsContextKey{}, opts)
}

// ManagerOptionsFromContext retrieves the ManagerOption slice from the context.
// Returns nil when no options were stored.
func ManagerOptionsFromContext(ctx context.Context) []ManagerOption {
	opts, _ := ctx.Value(managerOptsContextKey{}).([]ManagerOption)
	return opts
}
