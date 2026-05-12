// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/diagnose"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	// Replace the clock skew check with a fast, offline stub so tests
	// don't make real HTTPS calls (~3 s each).
	clockSkewCheckFunc = func() diagnose.Check {
		return diagnose.Check{
			Category: "clock",
			Name:     "clock skew",
			Status:   diagnose.StatusOK,
			Message:  "stubbed — no network call",
		}
	}
	os.Exit(m.Run())
}

func TestCommandDiagnose_Structure(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")

	assert.Equal(t, "diagnose", cmd.Use)
	assert.Contains(t, cmd.Aliases, "doctor")
	assert.Contains(t, cmd.Short, "diagnostics")
}

func TestCommandDiagnose_Flags(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")

	flag := cmd.Flags().Lookup("live-token")
	require.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestCommandDiagnose_AcceptsMaxOneArg(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	ctx, _ := newTestContext(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"arg1", "arg2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts at most 1 arg")
}

func TestCommandDiagnose_NoWriterInContext(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(context.Background()) // no writer
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestCommandDiagnose_Authenticated(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{
		Name:     "Test User",
		Username: "testuser@example.com",
	})
	mock.StatusResult.ExpiresAt = time.Now().Add(1 * time.Hour)
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[ok]")
	assert.Contains(t, output, "authenticated")
}

func TestCommandDiagnose_NotAuthenticated(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[warn]")
	assert.Contains(t, output, "not authenticated")
}

func TestCommandDiagnose_StatusError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.StatusErr = assert.AnError
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)

	output := buf.String()
	assert.Contains(t, output, "[fail]")
}

func TestCommandDiagnose_WithConfigSections(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			Entra:  &config.EntraAuthConfig{ClientID: "entra-client", TenantID: "entra-tenant"},
			GitHub: &config.GitHubAuthConfig{ClientID: "gh-client", Hostname: "github.com"},
			GCP:    &config.GCPAuthConfig{ClientID: "gcp-client", ImpersonateServiceAccount: "sa@project.iam"},
		},
	}
	ctx = config.WithConfig(ctx, cfg)

	mock := auth.NewMockHandler("test-handler")
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "entra-client")
	assert.Contains(t, output, "entra-tenant")
	assert.Contains(t, output, "gh-client")
	assert.Contains(t, output, "github.com")
	assert.Contains(t, output, "gcp-client")
	assert.Contains(t, output, "sa@project.iam")
}

func TestCommandDiagnose_TokenCache_AllValid(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "test-handler", IsExpired: false},
		{Handler: "test-handler", IsExpired: false},
	}
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "2 cached token(s)")
}

func TestCommandDiagnose_TokenCache_SomeExpired(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Handler: "test-handler", IsExpired: false},
		{Handler: "test-handler", IsExpired: true},
	}
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "1 expired")
	assert.Contains(t, output, "[warn]")
}

func TestCommandDiagnose_TokenCache_Error(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.ListCachedTokensErr = assert.AnError
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "could not read cached tokens")
}

func TestCommandDiagnose_LiveToken_Success(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.CapabilitiesValue = nil // no CapScopesOnTokenRequest
	mock.SetToken(&auth.Token{
		AccessToken: "test-token",
		ExpiresAt:   time.Now().Add(1 * time.Hour),
	})
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--live-token"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "token acquired successfully")
}

func TestCommandDiagnose_LiveToken_Failure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.CapabilitiesValue = nil // no CapScopesOnTokenRequest
	mock.SetTokenError(assert.AnError)
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--live-token"})

	err := cmd.Execute()
	require.Error(t, err) // failCount > 0

	output := buf.String()
	assert.Contains(t, output, "[fail]")
}

func TestCommandDiagnose_LiveToken_RequiresScope(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnTokenRequest}
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--live-token"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[info]")
	assert.Contains(t, output, "handler requires --scope")
}

func TestCommandDiagnose_ScopedToHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"test-handler"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "not authenticated")
}

func TestCommandDiagnose_SummaryAllPassed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// Config file may not exist in test env, so we may get warnings.
	// Just verify diagnostics completed successfully.
	assert.Contains(t, output, "Diagnostics complete")
}

func TestCommandDiagnose_StructuredOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	mock := auth.NewMockHandler("test-handler")
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// Structured output should contain JSON, not human messages
	assert.Contains(t, output, "registry")
}

// Benchmarks

func BenchmarkCommandDiagnose(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	}
}

func BenchmarkDiagnose_Authenticated(b *testing.B) {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)

	mock := auth.NewMockHandler("test-handler")
	mock.SetAuthenticated(&auth.Claims{Name: "User"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		ctx := writer.WithWriter(context.Background(), w)
		ctx = withTestHandler(ctx, mock)

		cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{})
		_ = cmd.Execute()
	}
}

func TestHandlerSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func() *auth.Registry
		query    string
		expected string
	}{
		{
			name:     "nil registry",
			setup:    func() *auth.Registry { return nil },
			query:    "github",
			expected: "not found",
		},
		{
			name: "handler not registered",
			setup: func() *auth.Registry {
				r := auth.NewRegistry()
				_ = r.Register(auth.NewMockHandler("entra"))
				return r
			},
			query:    "github",
			expected: "not found",
		},
		{
			name: "built-in handler",
			setup: func() *auth.Registry {
				r := auth.NewRegistry()
				_ = r.Register(auth.NewMockHandler("github"))
				return r
			},
			query:    "github",
			expected: "built-in",
		},
		{
			name: "plugin handler",
			setup: func() *auth.Registry {
				r := auth.NewRegistry()
				wrapper := plugin.NewAuthHandlerWrapper(nil, plugin.AuthHandlerInfo{
					Name: "github",
				})
				_ = r.Register(wrapper)
				return r
			},
			query:    "github",
			expected: "plugin",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := tc.setup()
			assert.Equal(t, tc.expected, handlerSource(reg, tc.query))
		})
	}
}

func TestCommandDiagnose_StartupLatency_BuiltIn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	// Set up an auth registry with a built-in mock handler.
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("test-handler")
	mock.SetNotAuthenticated()
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "n/a (built-in, not measured)")
}

func TestCommandDiagnose_StartupLatency_Plugin(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	// Set up an auth registry with both a plugin wrapper (for startup latency)
	// and a built-in mock (for the status check section).
	registry := auth.NewRegistry()
	wrapper := plugin.NewAuthHandlerWrapper(nil, plugin.AuthHandlerInfo{
		Name: "test-plugin",
	})
	wrapper.SetStartupLatency(50 * time.Millisecond)
	require.NoError(t, registry.Register(wrapper))

	// Also register a built-in mock so the status check doesn't panic.
	builtIn := auth.NewMockHandler("builtin-handler")
	builtIn.SetNotAuthenticated()
	require.NoError(t, registry.Register(builtIn))
	ctx = auth.WithRegistry(ctx, registry)

	// Scope to builtin-handler for status checks; startup latency is still emitted for all.
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"builtin-handler"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	// The built-in handler should report "n/a (built-in, not measured)"
	assert.Contains(t, output, "n/a (built-in, not measured)")
}

func TestCommandDiagnose_StartupLatency_SlowPlugin(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)

	// Set up an auth registry with a slow plugin wrapper and a built-in mock
	// so the status check (section 4) uses the mock instead of the nil-client wrapper.
	registry := auth.NewRegistry()
	wrapper := plugin.NewAuthHandlerWrapper(nil, plugin.AuthHandlerInfo{
		Name: "slow-plugin",
	})
	wrapper.SetStartupLatency(3 * time.Second) // over pluginStartupWarnThreshold
	require.NoError(t, registry.Register(wrapper))

	builtIn := auth.NewMockHandler("builtin-handler")
	builtIn.SetNotAuthenticated()
	require.NoError(t, registry.Register(builtIn))
	ctx = auth.WithRegistry(ctx, registry)

	// Scope to builtin-handler for status checks; startup latency is still emitted for all.
	cmd := CommandDiagnose(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"builtin-handler"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "slow startup")
	assert.Contains(t, output, "[warn]")
}
