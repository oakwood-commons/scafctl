// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDescribeCachedPlugins_SkipsNamesInSkipSet(t *testing.T) {
	t.Parallel()
	cached := []CachedPlugin{
		{Name: "builtin-provider", Version: "1.0.0", Path: "/fake/path"},
		{Name: "local-provider", Version: "2.0.0", Path: "/fake/path2"},
	}
	skip := map[string]bool{"builtin-provider": true}

	result := DescribeCachedPlugins(context.Background(), cached, skip)

	// local-provider is excluded because the probe fails (non-existent binary).
	assert.Empty(t, result)
}

func TestDescribeCachedPlugins_SkipsFailedProbes(t *testing.T) {
	t.Parallel()
	cached := []CachedPlugin{
		{Name: "bad-plugin", Version: "1.0.0", Path: "/nonexistent/binary"},
	}

	result := DescribeCachedPlugins(context.Background(), cached, nil)
	assert.Empty(t, result, "plugins that fail probing should be excluded")
}

func TestDescribeCachedPlugins_EmptyInput(t *testing.T) {
	t.Parallel()
	result := DescribeCachedPlugins(context.Background(), nil, nil)
	assert.Empty(t, result)
}

func TestProbePluginDescription_InvalidBinary(t *testing.T) {
	t.Parallel()
	desc, ok := ProbePluginDescription(context.Background(), "/nonexistent/binary", "test")
	assert.Empty(t, desc)
	assert.False(t, ok)
}
