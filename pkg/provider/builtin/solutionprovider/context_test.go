// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAncestorStackFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	stack := AncestorStackFromContext(ctx)
	assert.Nil(t, stack)
}

func TestWithAncestorStack(t *testing.T) {
	ctx := context.Background()
	stack := []string{"solution-a", "solution-b"}

	ctx = WithAncestorStack(ctx, stack)

	got := AncestorStackFromContext(ctx)
	assert.Equal(t, stack, got)
}

func TestPushAncestor_EmptyContext(t *testing.T) {
	ctx := context.Background()

	ctx, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	stack := AncestorStackFromContext(ctx)
	assert.Equal(t, []string{"solution-a"}, stack)
}

func TestPushAncestor_AppendsToExisting(t *testing.T) {
	ctx := context.Background()

	ctx, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	ctx, err = PushAncestor(ctx, "solution-b")
	require.NoError(t, err)

	stack := AncestorStackFromContext(ctx)
	assert.Equal(t, []string{"solution-a", "solution-b"}, stack)
}

func TestPushAncestor_DirectCycle(t *testing.T) {
	ctx := context.Background()

	ctx, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	_, err = PushAncestor(ctx, "solution-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference detected")
	assert.Contains(t, err.Error(), "solution-a")
}

func TestPushAncestor_IndirectCycle(t *testing.T) {
	ctx := context.Background()

	ctx, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	ctx, err = PushAncestor(ctx, "solution-b")
	require.NoError(t, err)

	_, err = PushAncestor(ctx, "solution-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference detected")
	assert.Contains(t, err.Error(), "solution-a")
	assert.Contains(t, err.Error(), "solution-b")
}

func TestPushAncestor_DoesNotMutateOriginalStack(t *testing.T) {
	ctx := context.Background()

	ctx1, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	ctx2, err := PushAncestor(ctx1, "solution-b")
	require.NoError(t, err)

	stack1 := AncestorStackFromContext(ctx1)
	stack2 := AncestorStackFromContext(ctx2)

	assert.Equal(t, []string{"solution-a"}, stack1)
	assert.Equal(t, []string{"solution-a", "solution-b"}, stack2)
}

func TestCheckDepth_UnderLimit(t *testing.T) {
	ctx := context.Background()

	ctx, err := PushAncestor(ctx, "solution-a")
	require.NoError(t, err)

	err = CheckDepth(ctx, 5)
	assert.NoError(t, err)
}

func TestCheckDepth_AtLimit(t *testing.T) {
	ctx := context.Background()

	names := []string{"a", "b", "c"}
	for _, name := range names {
		var err error
		ctx, err = PushAncestor(ctx, name)
		require.NoError(t, err)
	}

	err := CheckDepth(ctx, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max nesting depth 3 exceeded")
}

func TestCheckDepth_EmptyStack(t *testing.T) {
	ctx := context.Background()

	err := CheckDepth(ctx, 10)
	assert.NoError(t, err)
}

func TestCanonicalize_URL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/solution.yaml", "https://example.com/solution.yaml"},
		{"http://internal.corp/path/to/sol.yaml", "http://internal.corp/path/to/sol.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Canonicalize(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCanonicalize_CatalogReference(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"deploy-to-k8s", "deploy-to-k8s"},
		{"deploy-to-k8s@2.0.0", "deploy-to-k8s@2.0.0"},
		{"my-solution@sha256:abc123", "my-solution@sha256:abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Canonicalize(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCanonicalize_AbsolutePath(t *testing.T) {
	got := Canonicalize("/home/user/infra.yaml")
	assert.Equal(t, "/home/user/infra.yaml", got)
}

func TestCanonicalize_RelativePath(t *testing.T) {
	got := Canonicalize("./child.yaml")
	// Should be resolved to an absolute path
	assert.True(t, len(got) > len("./child.yaml"), "expected absolute path, got: %s", got)
	assert.NotEqual(t, "./child.yaml", got)
}
