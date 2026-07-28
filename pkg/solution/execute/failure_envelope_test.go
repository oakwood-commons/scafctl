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
		assert.Nil(t, BuildFailureEnvelope(nil))
	})

	t.Run("builds status and diagnostics", func(t *testing.T) {
		got := BuildFailureEnvelope(errors.New("kaboom"))
		require.NotNil(t, got)
		assert.Equal(t, StatusFailed, got[StatusFieldKey])
		diags, ok := got[DiagnosticsFieldKey].([]ResolverDiagnostic)
		require.True(t, ok)
		require.Len(t, diags, 1)
		assert.Equal(t, "kaboom", diags[0].Message)
	})
}
