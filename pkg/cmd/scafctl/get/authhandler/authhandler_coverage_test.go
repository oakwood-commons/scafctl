// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authhandler

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Helpers ──────────────────────────────────────────────────────────────────

func newAuthHandlerCtx(t *testing.T) context.Context {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	return ctx
}

// ── RunListHandlers tests ─────────────────────────────────────────────────────

func TestRunListHandlers_NoHandlers(t *testing.T) {
	t.Parallel()

	ctx := newAuthHandlerCtx(t)
	// Empty registry — no handlers registered
	reg := auth.NewRegistry()
	ctx = auth.WithRegistry(ctx, reg)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	opts := &Options{
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := opts.RunListHandlers(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handlers")
}

func TestRunListHandlers_WithHandlers_QuietOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
	mock.FlowsValue = []auth.Flow{auth.FlowInteractive}

	reg := auth.NewRegistry()
	_ = reg.Register(mock)
	ctx = auth.WithRegistry(ctx, reg)

	opts := &Options{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: cmdflags.KvxOutputFlags{Output: "quiet"},
	}

	// quiet format suppresses output — just verify no error
	err := opts.RunListHandlers(ctx)
	require.NoError(t, err)
}

// ── RunGetHandler tests ───────────────────────────────────────────────────────

func TestRunGetHandler_NotFound(t *testing.T) {
	t.Parallel()

	ctx := newAuthHandlerCtx(t)
	reg := auth.NewRegistry()
	ctx = auth.WithRegistry(ctx, reg)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	opts := &Options{
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := opts.RunGetHandler(ctx, "nonexistent-handler")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRunGetHandler_Found_DefaultOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("github")
	mock.DisplayNameValue = "GitHub"
	mock.CapabilitiesValue = []auth.Capability{auth.CapHostname}
	mock.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowPAT}

	reg := auth.NewRegistry()
	_ = reg.Register(mock)
	ctx = auth.WithRegistry(ctx, reg)

	opts := &Options{
		IOStreams: ioStreams,
		CliParams: cliParams,
		// No Output set — defaults to formatted detail view
	}

	err := opts.RunGetHandler(ctx, "github")
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "GitHub")
}

func TestRunGetHandler_Found_JSONOutput(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &out, &out, false)
	cliParams := settings.NewCliParams()
	cliParams.NoColor = true

	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("gcp")
	mock.DisplayNameValue = "Google Cloud Platform"
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
	mock.FlowsValue = []auth.Flow{auth.FlowInteractive}

	reg := auth.NewRegistry()
	_ = reg.Register(mock)
	ctx = auth.WithRegistry(ctx, reg)

	opts := &Options{
		IOStreams:      ioStreams,
		CliParams:      cliParams,
		KvxOutputFlags: cmdflags.KvxOutputFlags{Output: "json"},
	}

	err := opts.RunGetHandler(ctx, "gcp")
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "gcp")
}

// ── buildHandlerRow / buildHandlerDetail tests ────────────────────────────────

func TestBuildHandlerRow(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("test-handler")
	mock.DisplayNameValue = "Test Handler"
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapTenantID}
	mock.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowPAT}

	row := buildHandlerRow(mock)
	assert.Equal(t, "test-handler", row["name"])
	assert.Equal(t, "Test Handler", row["displayName"])

	caps, ok := row["capabilities"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{string(auth.CapScopesOnLogin), string(auth.CapTenantID)}, caps)

	flows, ok := row["flows"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{string(auth.FlowInteractive), string(auth.FlowPAT)}, flows)
}

func TestBuildHandlerDetail(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("detail-handler")
	mock.DisplayNameValue = "Detail Handler"
	mock.CapabilitiesValue = []auth.Capability{auth.CapHostname}
	mock.FlowsValue = []auth.Flow{auth.FlowDeviceCode}

	detail := buildHandlerDetail(mock)
	assert.Equal(t, "detail-handler", detail["name"])
	assert.Equal(t, "Detail Handler", detail["displayName"])
}

func TestBuildHandlerRow_EmptyFlowsAndCaps(t *testing.T) {
	t.Parallel()

	mock := auth.NewMockHandler("simple")
	mock.DisplayNameValue = "Simple"
	mock.CapabilitiesValue = []auth.Capability{} // empty (not nil, to avoid mock default)
	mock.FlowsValue = []auth.Flow{}              // empty (not nil, to avoid mock default of FlowDeviceCode)

	row := buildHandlerRow(mock)
	assert.Equal(t, "simple", row["name"])

	caps, ok := row["capabilities"].([]string)
	require.True(t, ok)
	assert.Empty(t, caps)

	flows, ok := row["flows"].([]string)
	require.True(t, ok)
	assert.Empty(t, flows)
}

// ── CommandAuthHandler flags ──────────────────────────────────────────────────

func TestCommandAuthHandler_HasOutputFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAuthHandler(cliParams, ioStreams, "scafctl/get")

	expectedFlags := []string{"output", "interactive", "expression"}
	for _, name := range expectedFlags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(name)
			assert.NotNil(t, f, "flag %q should exist", name)
		})
	}
}

func TestCommandAuthHandler_SilenceUsage(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandAuthHandler(cliParams, ioStreams, "scafctl/get")
	assert.True(t, cmd.SilenceUsage)
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkBuildHandlerRow(b *testing.B) {
	mock := auth.NewMockHandler("bench-handler")
	mock.DisplayNameValue = "Bench Handler"
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapTenantID}
	mock.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buildHandlerRow(mock)
	}
}
