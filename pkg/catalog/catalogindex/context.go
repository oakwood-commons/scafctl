// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// indexContextKey is the context key under which the shared Index is stored.
type indexContextKey struct{}

// WithIndex stores a shared Index in the context so downstream consumers (the
// plugin fetcher, dependency scoping, FQN resolution) reuse a single instance
// built once from config rather than rebuilding the topology per call. Mirrors
// config.WithConfig: attach it once at process/server startup, alongside the
// config.
func WithIndex(ctx context.Context, idx *Index) context.Context {
	return context.WithValue(ctx, indexContextKey{}, idx)
}

// FromContext returns the shared Index previously stored with WithIndex. When
// none is present it falls back to building one from the context's config
// (config.FromContext), so callers always receive a usable, non-nil Index --
// this keeps tests and code paths that never called WithIndex working. The
// fallback build carries no allowlist gate; apply one per consumer with
// WithAllowed (which copies, never mutating the shared instance).
func FromContext(ctx context.Context) *Index {
	if idx, ok := ctx.Value(indexContextKey{}).(*Index); ok && idx != nil {
		return idx
	}
	return FromConfig(config.FromContext(ctx))
}
