// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newProviderTestCtx creates context with writer and logger for provider tests.
func newProviderTestCtx(t testing.TB) context.Context {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logger.GetNoopLogger()
	ctx = logger.WithLogger(ctx, lgr)
	return ctx
}

// ── ProviderOptions.Run error paths ───────────────────────────────────────────

// TestProviderOptions_Run_ProviderNotFound verifies that an unknown provider
// name returns a "not found" error.
func TestProviderOptions_Run_ProviderNotFound(t *testing.T) {
	ctx := newProviderTestCtx(t)

	opts := &ProviderOptions{
		IOStreams:    terminal.NewIOStreams(nil, nil, nil, false),
		CliParams:    settings.NewCliParams(),
		ProviderName: "this-provider-does-not-exist-xyz123",
	}
	opts.Output = "json"

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestProviderOptions_Run_InvalidOnConflict verifies that an invalid --on-conflict
// value returns an error after finding a valid provider.
func TestProviderOptions_Run_InvalidOnConflict(t *testing.T) {
	ctx := newProviderTestCtx(t)

	opts := &ProviderOptions{
		IOStreams:    terminal.NewIOStreams(nil, nil, nil, false),
		CliParams:    settings.NewCliParams(),
		ProviderName: "message",
		OnConflict:   "not-a-valid-strategy",
		InputParams:  []string{"message=hello"},
	}
	opts.Output = "json"

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --on-conflict value")
}

// TestProviderOptions_Run_MessageProvider verifies the message provider runs successfully.
func TestProviderOptions_Run_MessageProvider(t *testing.T) {
	ctx := newProviderTestCtx(t)

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	opts := &ProviderOptions{
		IOStreams:    ioStreams,
		CliParams:    settings.NewCliParams(),
		ProviderName: "message",
		InputParams:  []string{"message=hello"},
	}
	opts.Output = "json"

	err := opts.Run(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, out.String(), "message provider should produce output")
}

// ── resolveCapability tests ────────────────────────────────────────────────────

// TestResolveCapability_Default returns first capability when Capability is empty.
func TestResolveCapability_Default(t *testing.T) {
	t.Parallel()

	opts := &ProviderOptions{}
	desc := &provider.Descriptor{
		Name:         "test-provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform},
	}

	capability, err := opts.resolveCapability(desc)
	require.NoError(t, err)
	assert.Equal(t, provider.CapabilityFrom, capability)
}

// TestResolveCapability_Explicit requests a specific valid capability.
func TestResolveCapability_Explicit(t *testing.T) {
	t.Parallel()

	opts := &ProviderOptions{
		Capability: "transform",
	}
	desc := &provider.Descriptor{
		Name:         "test-provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform},
	}

	capability, err := opts.resolveCapability(desc)
	require.NoError(t, err)
	assert.Equal(t, provider.CapabilityTransform, capability)
}

// TestResolveCapability_ExplicitNotSupported requests a capability the provider lacks.
func TestResolveCapability_ExplicitNotSupported(t *testing.T) {
	t.Parallel()

	opts := &ProviderOptions{
		Capability: "action",
	}
	desc := &provider.Descriptor{
		Name:         "test-provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom}, // no action
	}

	_, err := opts.resolveCapability(desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support capability")
}

// TestResolveCapability_Invalid requests a completely invalid capability name.
func TestResolveCapability_Invalid(t *testing.T) {
	t.Parallel()

	opts := &ProviderOptions{
		Capability: "not-a-real-capability",
	}
	desc := &provider.Descriptor{
		Name:         "test-provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom},
	}

	_, err := opts.resolveCapability(desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid capability")
}

// TestResolveCapability_NoCapabilities errors when no capabilities are declared.
func TestResolveCapability_NoCapabilities(t *testing.T) {
	t.Parallel()

	opts := &ProviderOptions{}
	desc := &provider.Descriptor{
		Name:         "test-provider",
		Capabilities: nil, // empty
	}

	_, err := opts.resolveCapability(desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares no capabilities")
}

// ── Benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkResolveCapability_Default(b *testing.B) {
	opts := &ProviderOptions{}
	desc := &provider.Descriptor{
		Name:         "bench-provider",
		Capabilities: []provider.Capability{provider.CapabilityFrom},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = opts.resolveCapability(desc)
	}
}

func BenchmarkProviderOptions_Run_NotFound(b *testing.B) {
	ctx := newProviderTestCtx(b)
	opts := &ProviderOptions{
		IOStreams:    terminal.NewIOStreams(nil, nil, nil, false),
		CliParams:    settings.NewCliParams(),
		ProviderName: "nonexistent-provider-xyz",
	}
	opts.Output = "json"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = opts.Run(ctx)
	}
}
