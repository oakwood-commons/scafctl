// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import "context"

type contextKey string

const resolverKey contextKey = "kube.resolver"

// WithResolver returns a new context with the cluster resolver attached.
func WithResolver(ctx context.Context, resolver ClusterResolver) context.Context {
	return context.WithValue(ctx, resolverKey, resolver)
}

// ResolverFromContext retrieves the cluster resolver from the context. It
// returns nil when no resolver is configured, in which case callers fall back
// to explicit --server flags and auto-detection.
func ResolverFromContext(ctx context.Context) ClusterResolver {
	resolver, _ := ctx.Value(resolverKey).(ClusterResolver)
	return resolver
}
