// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithExecutionMode_AndFromContext(t *testing.T) {
	ctx := context.Background()

	capabilities := []Capability{
		CapabilityFrom,
		CapabilityTransform,
		CapabilityValidation,
		CapabilityAuthentication,
		CapabilityAction,
	}

	for _, capability := range capabilities {
		t.Run(string(capability), func(t *testing.T) {
			ctx = WithExecutionMode(ctx, capability)
			mode, ok := ExecutionModeFromContext(ctx)
			assert.True(t, ok)
			assert.Equal(t, capability, mode)
		})
	}
}

func TestExecutionModeFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	mode, ok := ExecutionModeFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, Capability(""), mode)
}

func TestExecutionModeFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, executionModeKey, "not-a-capability")

	mode, ok := ExecutionModeFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, Capability(""), mode)
}

func TestWithDryRun_AndFromContext(t *testing.T) {
	ctx := context.Background()

	ctx = WithDryRun(ctx, true)
	assert.True(t, DryRunFromContext(ctx))

	ctx = WithDryRun(ctx, false)
	assert.False(t, DryRunFromContext(ctx))
}

func TestDryRunFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	assert.False(t, DryRunFromContext(ctx))
}

func TestDryRunFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, dryRunKey, "not-a-bool")
	assert.False(t, DryRunFromContext(ctx))
}

func TestWithResolverContext_AndFromContext(t *testing.T) {
	ctx := context.Background()

	resolverContext := map[string]any{
		"environmentName": "production",
		"region":          "us-west-2",
		"accountId":       "123456789",
		"tags":            []string{"tag1", "tag2"},
	}

	ctx = WithResolverContext(ctx, resolverContext)

	retrievedCtx, ok := ResolverContextFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, resolverContext, retrievedCtx)
	assert.Equal(t, "production", retrievedCtx["environmentName"])
	assert.Equal(t, "us-west-2", retrievedCtx["region"])
}

func TestResolverContextFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	resolverCtx, ok := ResolverContextFromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, resolverCtx)
}

func TestResolverContextFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, resolverContextKey, "not-a-map")

	resolverCtx, ok := ResolverContextFromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, resolverCtx)
}

func TestResolverContextFromContext_EmptyMap(t *testing.T) {
	ctx := context.Background()
	emptyMap := make(map[string]any)
	ctx = WithResolverContext(ctx, emptyMap)

	retrievedCtx, ok := ResolverContextFromContext(ctx)
	assert.True(t, ok)
	assert.NotNil(t, retrievedCtx)
	assert.Len(t, retrievedCtx, 0)
}

func TestContextChaining(t *testing.T) {
	ctx := context.Background()

	resolverContext := map[string]any{
		"env":    "dev",
		"region": "us-east-1",
	}

	ctx = WithExecutionMode(ctx, CapabilityFrom)
	ctx = WithDryRun(ctx, true)
	ctx = WithResolverContext(ctx, resolverContext)

	mode, ok := ExecutionModeFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, CapabilityFrom, mode)

	assert.True(t, DryRunFromContext(ctx))

	retrievedCtx, ok := ResolverContextFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, resolverContext, retrievedCtx)
}

func TestContextIsolation(t *testing.T) {
	ctx := context.Background()

	ctx1 := WithExecutionMode(ctx, CapabilityFrom)
	ctx2 := WithExecutionMode(ctx, CapabilityAction)

	mode1, ok1 := ExecutionModeFromContext(ctx1)
	mode2, ok2 := ExecutionModeFromContext(ctx2)

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, CapabilityFrom, mode1)
	assert.Equal(t, CapabilityAction, mode2)

	mode, ok := ExecutionModeFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, Capability(""), mode)
}

// Benchmarks

func BenchmarkWithExecutionMode(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_ = WithExecutionMode(ctx, CapabilityFrom)
	}
}

func BenchmarkExecutionModeFromContext(b *testing.B) {
	ctx := WithExecutionMode(context.Background(), CapabilityFrom)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ExecutionModeFromContext(ctx)
	}
}

func BenchmarkWithDryRun(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_ = WithDryRun(ctx, true)
	}
}

func BenchmarkDryRunFromContext(b *testing.B) {
	ctx := WithDryRun(context.Background(), true)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DryRunFromContext(ctx)
	}
}

func BenchmarkWithResolverContext(b *testing.B) {
	ctx := context.Background()
	resolverCtx := map[string]any{"env": "prod", "region": "us-west-2"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithResolverContext(ctx, resolverCtx)
	}
}

func BenchmarkResolverContextFromContext(b *testing.B) {
	resolverCtx := map[string]any{"env": "prod", "region": "us-west-2"}
	ctx := WithResolverContext(context.Background(), resolverCtx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolverContextFromContext(ctx)
	}
}

func TestWithParameters_AndFromContext(t *testing.T) {
	ctx := context.Background()

	params := map[string]any{
		"env":     "prod",
		"regions": []string{"us-east1", "us-west1"},
		"count":   int64(42),
	}

	ctx = WithParameters(ctx, params)
	retrieved, ok := ParametersFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, params, retrieved)
}

func TestParametersFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	params, ok := ParametersFromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, params)
}

func TestParametersFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, parametersKey, "not-a-map")

	params, ok := ParametersFromContext(ctx)
	assert.False(t, ok)
	assert.Nil(t, params)
}

func BenchmarkContextChaining(b *testing.B) {
	ctx := context.Background()
	resolverCtx := map[string]any{"env": "prod"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx = WithExecutionMode(ctx, CapabilityFrom)
		ctx = WithDryRun(ctx, true)
		ctx = WithResolverContext(ctx, resolverCtx)
	}
}

func TestWithOutputDirectory_AndFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithOutputDirectory(ctx, "/tmp/output")

	dir, ok := OutputDirectoryFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/output", dir)
}

func TestOutputDirectoryFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	dir, ok := OutputDirectoryFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, "", dir)
}

func TestOutputDirectoryFromContext_WrongType(t *testing.T) {
	ctx := context.Background()
	ctx = context.WithValue(ctx, outputDirectoryKey, 12345)

	dir, ok := OutputDirectoryFromContext(ctx)
	assert.False(t, ok)
	assert.Equal(t, "", dir)
}

func TestOutputDirectoryFromContext_EmptyString(t *testing.T) {
	ctx := context.Background()
	ctx = WithOutputDirectory(ctx, "")

	dir, ok := OutputDirectoryFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "", dir)
}

func TestContextChaining_WithOutputDirectory(t *testing.T) {
	ctx := context.Background()

	ctx = WithExecutionMode(ctx, CapabilityAction)
	ctx = WithDryRun(ctx, true)
	ctx = WithOutputDirectory(ctx, "/tmp/output")

	mode, ok := ExecutionModeFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, CapabilityAction, mode)

	assert.True(t, DryRunFromContext(ctx))

	dir, ok := OutputDirectoryFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "/tmp/output", dir)
}

func BenchmarkWithOutputDirectory(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		_ = WithOutputDirectory(ctx, "/tmp/output")
	}
}

func BenchmarkOutputDirectoryFromContext(b *testing.B) {
	ctx := WithOutputDirectory(context.Background(), "/tmp/output")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = OutputDirectoryFromContext(ctx)
	}
}

func TestWithSolutionMetadata_AndFromContext(t *testing.T) {
	ctx := context.Background()
	meta := &SolutionMeta{
		Name:    "my-solution",
		Version: "1.0.0",
	}
	ctx = WithSolutionMetadata(ctx, meta)

	got, ok := SolutionMetadataFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, meta, got)
}

func TestSolutionMetadataFromContext_NotSet(t *testing.T) {
	_, ok := SolutionMetadataFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithIOStreams_AndFromContext(t *testing.T) {
	ctx := context.Background()
	streams := &IOStreams{}
	ctx = WithIOStreams(ctx, streams)

	got, ok := IOStreamsFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, streams, got)
}

func TestIOStreamsFromContext_NotSet(t *testing.T) {
	_, ok := IOStreamsFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithConflictStrategy_AndFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithConflictStrategy(ctx, "overwrite")

	got, ok := ConflictStrategyFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "overwrite", got)
}

func TestConflictStrategyFromContext_NotSet(t *testing.T) {
	_, ok := ConflictStrategyFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithBackup_AndFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithBackup(ctx, true)

	got, ok := BackupFromContext(ctx)
	assert.True(t, ok)
	assert.True(t, got)
}

func TestBackupFromContext_NotSet(t *testing.T) {
	_, ok := BackupFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithIterationContext_AndFromContext(t *testing.T) {
	ctx := context.Background()
	iterCtx := &IterationContext{Item: "hello", Index: 2}
	ctx = WithIterationContext(ctx, iterCtx)

	got, ok := IterationContextFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, iterCtx, got)
}

func TestIterationContextFromContext_NotSet(t *testing.T) {
	_, ok := IterationContextFromContext(context.Background())
	assert.False(t, ok)
}

func TestWithSolutionDirectory_AndFromContext(t *testing.T) {
	ctx := context.Background()
	ctx = WithSolutionDirectory(ctx, "/projects/my-solution")

	got, ok := SolutionDirectoryFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "/projects/my-solution", got)
}

func TestSolutionDirectoryFromContext_NotSet(t *testing.T) {
	_, ok := SolutionDirectoryFromContext(context.Background())
	assert.False(t, ok)
}
