// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

func TestWithIndex_RoundTrip(t *testing.T) {
	idx := FromConfig(testConfig())
	ctx := WithIndex(context.Background(), idx)

	// FromContext returns the exact shared instance that was stored.
	got := FromContext(ctx)
	assert.Same(t, idx, got)
}

func TestFromContext_FallsBackToConfig(t *testing.T) {
	// No Index stored, but a config is present -> build from config.
	ctx := config.WithConfig(context.Background(), testConfig())
	got := FromContext(ctx)
	assert.NotNil(t, got)

	alias, ok := got.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
	assert.Equal(t, "Prod", alias)
}

func TestFromContext_EmptyContext(t *testing.T) {
	// Neither Index nor config -> non-nil empty Index (nil-safe lookups).
	got := FromContext(context.Background())
	assert.NotNil(t, got)
	_, ok := got.AliasForRegistry("ghcr.io/acme/plugins")
	assert.False(t, ok)
}

func TestFromContext_NilStoredFallsBack(t *testing.T) {
	// A nil *Index stored explicitly must not shadow the config fallback.
	var nilIdx *Index
	ctx := WithIndex(config.WithConfig(context.Background(), testConfig()), nilIdx)
	got := FromContext(ctx)
	assert.NotNil(t, got)
	_, ok := got.AliasForRegistry("ghcr.io/acme/plugins")
	assert.True(t, ok)
}
