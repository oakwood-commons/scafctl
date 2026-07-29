// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectResolverFailureEnvelope(t *testing.T) {
	t.Run("nil error is a no-op", func(t *testing.T) {
		results := map[string]any{"a": 1}
		got := InjectResolverFailureEnvelope(results, nil)
		assert.Equal(t, map[string]any{"a": 1}, got)
		assert.NotContains(t, got, StatusKey)
		assert.NotContains(t, got, DiagnosticsKey)
	})

	t.Run("nil map is created", func(t *testing.T) {
		got := InjectResolverFailureEnvelope(nil, errors.New("boom"))
		require.NotNil(t, got)
		assert.Equal(t, StatusFailed, got[StatusKey])
		diags, ok := got[DiagnosticsKey].([]ResolverDiagnostic)
		require.True(t, ok)
		require.Len(t, diags, 1)
		assert.Equal(t, "boom", diags[0].Message)
	})

	t.Run("preserves existing values", func(t *testing.T) {
		results := map[string]any{"resolverA": "value", "resolverB": 42}
		got := InjectResolverFailureEnvelope(results, errors.New("failed"))
		assert.Equal(t, "value", got["resolverA"])
		assert.Equal(t, 42, got["resolverB"])
		assert.Equal(t, StatusFailed, got[StatusKey])
		assert.Contains(t, got, DiagnosticsKey)
	})
}

func TestBuildFailureEnvelope(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, BuildFailureEnvelope(nil, nil))
	})

	t.Run("builds status and diagnostics", func(t *testing.T) {
		got := BuildFailureEnvelope(errors.New("kaboom"), nil)
		require.NotNil(t, got)
		assert.Equal(t, StatusFailed, got[StatusFieldKey])
		diags, ok := got[DiagnosticsFieldKey].([]ResolverDiagnostic)
		require.True(t, ok)
		require.Len(t, diags, 1)
		assert.Equal(t, "kaboom", diags[0].Message)
		assert.NotContains(t, got, ResolversFieldKey,
			"resolvers key must be omitted when no resolver data is supplied")
	})

	t.Run("embeds resolvers when data supplied", func(t *testing.T) {
		got := BuildFailureEnvelope(errors.New("kaboom"), map[string]any{"good": "i-resolved"})
		require.NotNil(t, got)
		assert.Equal(t, StatusFailed, got[StatusFieldKey])
		resolvers, ok := got[ResolversFieldKey].(map[string]any)
		require.True(t, ok, "resolvers key must carry the resolved values")
		assert.Equal(t, "i-resolved", resolvers["good"])
	})
}

func TestInjectSolutionDiagnostics(t *testing.T) {
	t.Run("nil error is a no-op", func(t *testing.T) {
		envelope := map[string]any{"status": "succeeded"}
		got := InjectSolutionDiagnostics(envelope, nil, map[string]any{"good": "x"})
		assert.NotContains(t, got, DiagnosticsFieldKey)
		assert.NotContains(t, got, ResolversFieldKey)
	})

	t.Run("nil envelope is a no-op", func(t *testing.T) {
		assert.Nil(t, InjectSolutionDiagnostics(nil, errors.New("kaboom"), nil))
	})

	t.Run("injects diagnostics and resolvers into success envelope", func(t *testing.T) {
		envelope := map[string]any{"status": "succeeded"}
		got := InjectSolutionDiagnostics(envelope, errors.New("kaboom"), map[string]any{"good": "i-resolved"})
		assert.Equal(t, "succeeded", got["status"], "existing keys must be preserved")
		diags, ok := got[DiagnosticsFieldKey].([]ResolverDiagnostic)
		require.True(t, ok)
		require.Len(t, diags, 1)
		assert.Equal(t, "kaboom", diags[0].Message)
		resolvers, ok := got[ResolversFieldKey].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "i-resolved", resolvers["good"])
	})
}
