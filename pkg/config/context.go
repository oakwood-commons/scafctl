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

// baseDefaultsContextKey is the context key for storing embedder defaults bytes.
type baseDefaultsContextKey struct{}

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

// WithBaseDefaults stores the raw embedder defaults YAML bytes in the context.
// Commands that need to write defaults to disk (e.g. config reset) use these
// bytes so that the on-disk file matches the embedder's runtime defaults.
func WithBaseDefaults(ctx context.Context, data []byte) context.Context {
	return context.WithValue(ctx, baseDefaultsContextKey{}, data)
}

// BaseDefaultsFromContext retrieves the embedder defaults YAML bytes from the
// context. Returns nil when no embedder defaults were stored (i.e. plain
// scafctl usage).
func BaseDefaultsFromContext(ctx context.Context) []byte {
	data, _ := ctx.Value(baseDefaultsContextKey{}).([]byte)
	return data
}
