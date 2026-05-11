// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandMigrate_Structure(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandMigrate(cliParams, ioStreams, "scafctl/auth")

	assert.Equal(t, "migrate", cmd.Use)
	assert.Contains(t, cmd.Short, "Pre-install")
}

func TestCommandMigrate_NoArgs(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandMigrate(cliParams, ioStreams, "scafctl/auth")
	ctx, _ := newTestContext(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"extra-arg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandMigrate_NoWriterInContext(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandMigrate(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestCommandMigrate_NoOfficialRegistry(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.GetNoopLogger())

	cmd := CommandMigrate(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "Official auth handler registry not available")
}

func TestCommandMigrate_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandMigrate(cliParams, ioStreams, "mycli/auth")

	assert.Contains(t, cmd.Long, "mycli auth migrate")
	assert.NotContains(t, cmd.Long, "scafctl")
}

// migrateTestContext builds a context with writer, logger, and official+auth
// registries wired for runMigrate tests.
func migrateTestContext(t *testing.T, officialHandlers []authofficial.AuthHandler, authMocks []*auth.MockHandler) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)

	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.GetNoopLogger())

	officialReg := authofficial.NewRegistryFrom(officialHandlers)
	ctx = authofficial.WithRegistry(ctx, officialReg)

	if len(authMocks) > 0 {
		authReg := auth.NewRegistry()
		for _, m := range authMocks {
			require.NoError(t, authReg.Register(m))
		}
		ctx = auth.WithRegistry(ctx, authReg)
	}

	return ctx, &buf
}

func TestRunMigrate_AllReady(t *testing.T) {
	t.Parallel()

	handlers := []authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	}

	mock := auth.NewMockHandler("github")
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{{IsExpired: false}}

	ctx, buf := migrateTestContext(t, handlers, []*auth.MockHandler{mock})

	fetchFn := func(_ context.Context, _ []solution.PluginDependency) ([]plugin.FetchResult, error) {
		return []plugin.FetchResult{
			{Name: "github", Version: "0.1.0", FromCache: true},
			{Name: "entra", Version: "0.2.0", FromCache: false},
		}, nil
	}

	err := runMigrate(ctx, fetchFn)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "entra")
	assert.Contains(t, output, "READY")
	assert.Contains(t, output, "Migration complete")
}

func TestRunMigrate_PartialFailure(t *testing.T) {
	t.Parallel()

	handlers := []authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
		{Name: "entra", CatalogRef: "entra", DefaultVersion: "latest"},
	}

	ctx, buf := migrateTestContext(t, handlers, nil)

	fetchFn := func(_ context.Context, _ []solution.PluginDependency) ([]plugin.FetchResult, error) {
		// Only github succeeds; entra missing from results
		return []plugin.FetchResult{
			{Name: "github", Version: "0.1.0", FromCache: true},
		}, nil
	}

	err := runMigrate(ctx, fetchFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 handler(s) failed")

	output := buf.String()
	assert.Contains(t, output, "READY")
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "Migration incomplete")
}

func TestRunMigrate_FetchError(t *testing.T) {
	t.Parallel()

	handlers := []authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	}

	ctx, buf := migrateTestContext(t, handlers, nil)

	fetchFn := func(_ context.Context, _ []solution.PluginDependency) ([]plugin.FetchResult, error) {
		return nil, errors.New("network timeout")
	}

	err := runMigrate(ctx, fetchFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 handler(s) failed")

	output := buf.String()
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "network timeout")
}

func TestRunMigrate_NoWriter(t *testing.T) {
	t.Parallel()

	err := runMigrate(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestRunMigrate_NoOfficialRegistry(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	err := runMigrate(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, buf.String(), "Official auth handler registry not available")
}

func TestBuildPluginFetchFunc_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	fn := buildPluginFetchFunc("testcli")
	assert.NotNil(t, fn)
}

func TestBuildPluginFetchFunc_NilLoggerFallback(t *testing.T) {
	t.Parallel()

	fn := buildPluginFetchFunc("testcli")
	// Call with bare context (no logger, no config) — should not panic,
	// but will fail on catalog build (no config). We verify it doesn't
	// panic and returns a reasonable result or error.
	results, err := fn(context.Background(), []solution.PluginDependency{
		{Name: "test", Kind: solution.PluginKindProvider},
	})
	// May fail (no catalog configured) or succeed (plugin cached) —
	// the important thing is it does not panic.
	if err == nil {
		assert.NotEmpty(t, results)
	}
}

func BenchmarkRunMigrate(b *testing.B) {
	b.ReportAllocs()

	handlers := []authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	}
	officialReg := authofficial.NewRegistryFrom(handlers)
	fetchFn := func(_ context.Context, _ []solution.PluginDependency) ([]plugin.FetchResult, error) {
		return []plugin.FetchResult{{Name: "github", Version: "0.1.0", FromCache: true}}, nil
	}

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)

	b.ResetTimer()
	for b.Loop() {
		buf.Reset()
		ctx := writer.WithWriter(context.Background(), w)
		ctx = logger.WithLogger(ctx, logger.GetNoopLogger())
		ctx = authofficial.WithRegistry(ctx, officialReg)
		_ = runMigrate(ctx, fetchFn)
	}
}
