// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── WhatIf function tests ─────────────────────────────────────────────────────

func TestWhatIf_Clone(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/user/repo.git",
		"path":       "/tmp/repo",
		"branch":     "main",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "clone")
	assert.Contains(t, msg, "https://github.com/user/repo.git")
	assert.Contains(t, msg, "/tmp/repo")
	assert.Contains(t, msg, "main")
}

func TestWhatIf_CloneMinimal(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/user/repo.git",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "clone")
}

func TestWhatIf_Commit(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "commit",
		"path":      "/tmp/repo",
		"message":   "fix: something",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "commit")
	assert.Contains(t, msg, "fix: something")
}

func TestWhatIf_Push(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "push",
		"path":      "/tmp/repo",
		"branch":    "develop",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "push")
	assert.Contains(t, msg, "develop")
}

func TestWhatIf_Checkout(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "checkout",
		"path":      "/tmp/repo",
		"branch":    "feature",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "checkout")
	assert.Contains(t, msg, "feature")
}

func TestWhatIf_Tag(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "tag",
		"path":      "/tmp/repo",
		"tag":       "v1.0.0",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "tag")
	assert.Contains(t, msg, "v1.0.0")
}

func TestWhatIf_DefaultOperation(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "status",
		"path":      "/tmp/repo",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "status")
	assert.Contains(t, msg, "/tmp/repo")
}

func TestWhatIf_DefaultOperationNoPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), map[string]any{
		"operation": "log",
	})
	require.NoError(t, err)
	assert.Contains(t, msg, "log")
}

func TestWhatIf_InvalidInput(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	msg, err := p.Descriptor().WhatIf(t.Context(), "not-a-map")
	require.NoError(t, err)
	assert.Empty(t, msg)
}

// ── Validation tests for uncovered operations ─────────────────────────────────

func TestGitProvider_Execute_Push_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "push",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestGitProvider_Execute_Log_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "log",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestGitProvider_Execute_Tag_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "tag",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestGitProvider_Execute_Add_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "add",
		"files":     []any{"README.md"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestGitProvider_Execute_Commit_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "commit",
		"message":   "test commit",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

func TestGitProvider_Execute_Pull_MissingPath(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "pull",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path")
}

// ── DryRun tests for additional operations ────────────────────────────────────

func TestGitProvider_Execute_DryRun_Push(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "push",
		"path":      "/tmp/repo",
		"branch":    "main",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "push")
	assert.Contains(t, data["_message"].(string), "main")
}

func TestGitProvider_Execute_DryRun_Log(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "log",
		"path":      "/tmp/repo",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "log")
}

func TestGitProvider_Execute_DryRun_Tag(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "tag",
		"path":      "/tmp/repo",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "tag")
}

func TestGitProvider_Execute_DryRun_Commit(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "commit",
		"path":      "/tmp/repo",
		"message":   "fix: dry run test",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
}

func TestGitProvider_Execute_DryRun_Checkout(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "checkout",
		"path":      "/tmp/repo",
		"branch":    "feature",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "checkout")
}

func TestGitProvider_Execute_DryRun_Pull(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "pull",
		"path":      "/tmp/repo",
		"branch":    "develop",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "pull")
}

func TestGitProvider_Execute_DryRun_Add(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "add",
		"path":      "/tmp/repo",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "add")
}

func TestGitProvider_Execute_DryRun_Branch(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation": "branch",
		"path":      "/tmp/repo",
		"branch":    "new-branch",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "branch")
	assert.Contains(t, data["_message"].(string), "new-branch")
}

// ── Add operation edge cases ──────────────────────────────────────────────────

func TestGitProvider_Execute_Add_StringSlice(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	// string slice will fail on non-existent dir
	_, err := p.Execute(t.Context(), map[string]any{
		"operation": "add",
		"path":      "/nonexistent-dir-for-test",
		"files":     []string{"file1.txt", "file2.txt"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "directory does not exist")
}

// ── Invalid input type ────────────────────────────────────────────────────────

func TestGitProvider_Execute_InvalidInputType(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	_, err := p.Execute(t.Context(), "not-a-map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

// ── Clone with depth (float64) ────────────────────────────────────────────────

func TestGitProvider_Execute_Clone_DepthFloat(t *testing.T) {
	t.Parallel()
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	out, err := p.Execute(ctx, map[string]any{
		"operation":  "clone",
		"repository": "https://example.com/nonexistent.git",
		"path":       t.TempDir() + "/clone-target",
		"depth":      float64(1),
		"branch":     "main",
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "clone")
}

// ── applyEnvOverrides additional edges ────────────────────────────────────────

func TestApplyEnvOverrides_Empty(t *testing.T) {
	t.Parallel()
	result := applyEnvOverrides(nil, nil)
	assert.Empty(t, result)
}

func TestApplyEnvOverrides_BaseOnly(t *testing.T) {
	t.Parallel()
	result := applyEnvOverrides([]string{"FOO=bar", "BAZ=qux"}, nil)
	assert.Equal(t, []string{"FOO=bar", "BAZ=qux"}, result)
}

func TestApplyEnvOverrides_OverridesOnly(t *testing.T) {
	t.Parallel()
	result := applyEnvOverrides(nil, map[string]string{"NEW": "val"})
	assert.Contains(t, result, "NEW=val")
}

func TestApplyEnvOverrides_BaseEntryWithoutEquals(t *testing.T) {
	t.Parallel()
	// An env entry without "=" should use the whole string as key
	result := applyEnvOverrides([]string{"ORPHAN"}, map[string]string{"orphan": "replaced"})
	assert.Contains(t, result, "orphan=replaced")
	assert.NotContains(t, result, "ORPHAN")
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkWhatIf(b *testing.B) {
	p := NewGitProvider()
	input := map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/user/repo.git",
		"path":       "/tmp/repo",
		"branch":     "main",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Descriptor().WhatIf(b.Context(), input)
	}
}

func BenchmarkExecuteDryRun(b *testing.B) {
	p := NewGitProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	input := map[string]any{
		"operation": "push",
		"path":      "/tmp/repo",
		"branch":    "main",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, input)
	}
}
