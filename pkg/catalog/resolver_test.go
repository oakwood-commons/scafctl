// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNameVersion(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "name only",
			input:           "my-solution",
			expectedName:    "my-solution",
			expectedVersion: "",
		},
		{
			name:            "name with version",
			input:           "my-solution@1.2.3",
			expectedName:    "my-solution",
			expectedVersion: "1.2.3",
		},
		{
			name:            "name with prerelease version",
			input:           "my-app@2.0.0-beta.1",
			expectedName:    "my-app",
			expectedVersion: "2.0.0-beta.1",
		},
		{
			name:            "name with digest",
			input:           "my-solution@sha256:abc123def456",
			expectedName:    "my-solution",
			expectedVersion: "sha256:abc123def456",
		},
		{
			name:            "simple name",
			input:           "app",
			expectedName:    "app",
			expectedVersion: "",
		},
		{
			name:            "name with hyphens",
			input:           "my-complex-app-name@1.0.0",
			expectedName:    "my-complex-app-name",
			expectedVersion: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version := parseNameVersion(tt.input)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

func TestNewSolutionResolver(t *testing.T) {
	tmpDir := t.TempDir()
	catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	resolver := NewSolutionResolver(catalog, logr.Discard())

	assert.NotNil(t, resolver)
	assert.Equal(t, catalog, resolver.catalog)
}

func TestSolutionResolver_FetchSolution(t *testing.T) {
	t.Run("fetches existing solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		// Store a solution
		content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers: {}
`)
		ref := MustParseReference(ArtifactKindSolution, "test-solution@1.0.0")
		_, err = catalog.Store(context.Background(), ref, content, nil, nil, false)
		require.NoError(t, err)

		// Fetch via resolver
		resolver := NewSolutionResolver(catalog, logr.Discard())
		fetchedContent, err := resolver.FetchSolution(context.Background(), "test-solution@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, content, fetchedContent)
	})

	t.Run("fetches latest version when no version specified", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		// Store multiple versions
		content1 := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-version
  version: 1.0.0
spec:
  resolvers: {}
`)
		content2 := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-version
  version: 2.0.0
spec:
  resolvers: {}
`)
		ref1 := MustParseReference(ArtifactKindSolution, "multi-version@1.0.0")
		ref2 := MustParseReference(ArtifactKindSolution, "multi-version@2.0.0")
		_, err = catalog.Store(context.Background(), ref1, content1, nil, nil, false)
		require.NoError(t, err)
		_, err = catalog.Store(context.Background(), ref2, content2, nil, nil, false)
		require.NoError(t, err)

		// Fetch without version should get latest (2.0.0)
		resolver := NewSolutionResolver(catalog, logr.Discard())
		fetchedContent, err := resolver.FetchSolution(context.Background(), "multi-version")
		require.NoError(t, err)
		assert.Equal(t, content2, fetchedContent)
	})

	t.Run("returns error for non-existent solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(catalog, logr.Discard())
		_, err = resolver.FetchSolution(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("returns error for invalid reference", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(catalog, logr.Discard())
		_, err = resolver.FetchSolution(context.Background(), "Invalid-Name@1.0.0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid solution reference")
	})
}
