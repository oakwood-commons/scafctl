// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRemoteSolutionResolver(t *testing.T) {
	t.Parallel()

	t.Run("sets all fields from config", func(t *testing.T) {
		t.Parallel()
		handlerFunc := func(registry string) scafctlauth.Handler { return nil }
		resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
			CredentialStore: nil,
			AuthHandlerFunc: handlerFunc,
			Insecure:        true,
			Logger:          logr.Discard(),
		})

		require.NotNil(t, resolver)
		assert.True(t, resolver.insecure)
		assert.Nil(t, resolver.credStore)
		assert.NotNil(t, resolver.authHandlerFunc)
	})

	t.Run("nil auth handler func is accepted", func(t *testing.T) {
		t.Parallel()
		resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
			Logger: logr.Discard(),
		})

		require.NotNil(t, resolver)
		assert.Nil(t, resolver.authHandlerFunc)
	})
}

func TestRemoteSolutionResolver_FetchRemoteSolution_InvalidRef(t *testing.T) {
	t.Parallel()

	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Logger: logr.Discard(),
	})

	tests := []struct {
		name string
		ref  string
	}{
		{"empty ref", ""},
		{"whitespace only", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, bundleData, err := resolver.FetchRemoteSolution(t.Context(), tt.ref)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid remote reference")
			assert.Nil(t, content)
			assert.Nil(t, bundleData)
		})
	}
}

func TestRemoteSolutionResolver_FetchRemoteSolution_DefaultsToSolutionKind(t *testing.T) {
	t.Parallel()

	// We cannot easily test the full fetch path without a real registry.
	// Instead, verify the function parses the ref correctly by using a
	// reference with an explicit kind path segment so it reaches
	// NewRemoteCatalog. We test with a canceled context to avoid network calls.
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Insecure: true,
		Logger:   logr.Discard(),
	})

	// Cancel immediately so no network I/O occurs.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err)
	// Error should NOT be about parsing — it should be a fetch/context error
	assert.NotContains(t, err.Error(), "invalid remote reference")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_WithAuthHandlerFunc(t *testing.T) {
	t.Parallel()

	called := false
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		AuthHandlerFunc: func(registry string) scafctlauth.Handler {
			called = true
			assert.Equal(t, "localhost:9999", registry)
			return nil
		},
		Insecure: true,
		Logger:   logr.Discard(),
	})

	// Cancel immediately so no network I/O occurs.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err)
	assert.True(t, called, "auth handler func should have been called")
}
