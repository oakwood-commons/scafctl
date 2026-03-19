// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractVersionFromRef(t *testing.T) {
	assert.Equal(t, "1.2.3", ExtractVersionFromRef("my-solution@1.2.3"))
	assert.Equal(t, "latest", ExtractVersionFromRef("my-solution"))
	assert.Equal(t, "2.0.0", ExtractVersionFromRef("registry.example.com/solutions/my-solution@2.0.0"))
}

func TestTruncateDigest(t *testing.T) {
	long := "sha256:abcdefghijklmnopqrstuvwxyz"
	truncated := TruncateDigest(long)
	assert.Equal(t, long[:19]+"...", truncated)

	short := "short"
	assert.Equal(t, short, TruncateDigest(short))
}

func TestFilterDependencies_Empty(t *testing.T) {
	result, err := FilterDependencies(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestFilterDependencies_WithFilter(t *testing.T) {
	deps := []LockDependency{
		{Ref: "sol-a@1.0.0"},
		{Ref: "sol-b@2.0.0"},
	}
	result, err := FilterDependencies(deps, []string{"sol-a@1.0.0"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "sol-a@1.0.0", result[0].Ref)
}

func TestFilterDependencies_NotFound(t *testing.T) {
	deps := []LockDependency{{Ref: "sol-a@1.0.0"}}
	_, err := FilterDependencies(deps, []string{"sol-missing@1.0.0"})
	assert.Error(t, err)
}

func TestCheckPluginUpdates_Nil(t *testing.T) {
	result := CheckPluginUpdates(nil)
	assert.Nil(t, result)
}

func TestCheckPluginUpdates_WithPlugins(t *testing.T) {
	lock := &LockFile{
		Plugins: []LockPlugin{
			{Name: "my-plugin", Kind: "builtin", Version: "1.0.0", Digest: "sha256:abcdefghijklmnopqrstuvwxyz"},
		},
	}
	msgs := CheckPluginUpdates(lock)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "my-plugin")
	assert.Contains(t, msgs[0], "1.0.0")
}
