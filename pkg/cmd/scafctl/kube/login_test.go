// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	kubeapi "github.com/oakwood-commons/scafctl/pkg/kube"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

func newTestContext(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, logger.GetNoopLogger())
	return ctx, &buf
}

// mockHandlerName is the handler name registered by withMockHandler.
const mockHandlerName = "oidc"

// withMockHandler registers a mock auth handler in the context's auth registry.
func withMockHandler(t *testing.T, ctx context.Context) (context.Context, *auth.MockHandler) {
	t.Helper()
	mock := auth.NewMockHandler(mockHandlerName)
	mock.LoginResult = &auth.Result{}
	reg := auth.NewRegistry()
	require.NoError(t, reg.Register(mock))
	return auth.WithRegistry(ctx, reg), mock
}

// embedderParams returns CLI params with a non-default binary name to exercise
// the embedder contract.
func embedderParams() *settings.Run {
	p := settings.NewCliParams()
	p.BinaryName = "mycli"
	return p
}

func runCmd(t *testing.T, cmd *cobra.Command, ctx context.Context, args ...string) error {
	t.Helper()
	cmd.SetContext(ctx)
	cmd.SetArgs(args)
	return cmd.Execute()
}

func TestCommandLogin_MissingHandler(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)

	// No --handler, no resolver default: login cannot pick a handler.
	cmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	err := runCmd(t, cmd, ctx, "prod", "--server", "https://api.example.com:6443")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth handler is required")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandLogin_MissingServer(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)
	ctx, _ = withMockHandler(t, ctx)

	// No --server and no resolver default: login cannot determine the server.
	cmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	err := runCmd(t, cmd, ctx, "prod", "--handler", "oidc")
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandLogin_UnknownHandler(t *testing.T) {
	t.Parallel()
	ctx, _ := newTestContext(t)
	ctx = auth.WithRegistry(ctx, auth.NewRegistry())

	cmd := CommandLogin(embedderParams(), terminal.NewIOStreams(nil, nil, nil, false), "mycli")
	err := runCmd(t, cmd, ctx, "prod", "--handler", "nope", "--server", "https://api.example.com:6443")
	require.Error(t, err)
}

func TestCommandLogin_FallbackWritesKubeconfig(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)
	mock.GetTokenResult = &auth.Token{AccessToken: "tok"}

	path := filepath.Join(t.TempDir(), "config")
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
	err := runCmd(t, cmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
		"--current",
	)
	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Len(t, mock.LoginCalls, 1)
	assert.Contains(t, buf.String(), "Logged in")
}

func TestCommandLogin_JSONOutput(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, _ = withMockHandler(t, ctx)

	path := filepath.Join(t.TempDir(), "config")
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
	err := runCmd(t, cmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
		"--output", "json",
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "\"context\"")
	assert.Contains(t, buf.String(), "prod")
}

func TestCommandLogin_JSONReportsPerClusterTokens(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)
	mock.CapabilitiesValue = []auth.Capability{auth.CapTokenHostname}
	mock.GetTokenResult = &auth.Token{AccessToken: "tok"}

	path := filepath.Join(t.TempDir(), "config")
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
	err := runCmd(t, cmd, ctx, "prod",
		"--handler", mockHandlerName,
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
		"--output", "json",
	)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "\"perClusterTokens\"")
	assert.Contains(t, buf.String(), "true",
		"a CapTokenHostname handler must report perClusterTokens:true")
}

func TestCommandLogin_HandlerFromResolverDefault(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)
	mock.GetTokenResult = &auth.Token{AccessToken: "tok"}

	// The resolver supplies both the API server and the default handler, so the
	// user can omit --server and --handler entirely.
	resolver := &kubeapi.MockResolver{ResolveResult: &kubeapi.ClusterInfo{
		Name:           "prod",
		APIServerURL:   "https://api.example.com:6443",
		DefaultHandler: mockHandlerName,
	}}
	ctx = kubeapi.WithResolver(ctx, resolver)

	path := filepath.Join(t.TempDir(), "config")
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
	err := runCmd(t, cmd, ctx, "prod", "--kubeconfig", path)
	require.NoError(t, err)
	assert.FileExists(t, path)
	assert.Len(t, mock.LoginCalls, 1)
	assert.Contains(t, buf.String(), "Logged in")
}

func TestCommandLogin_VerifyFailsWithoutProvider(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)
	ctx, mock := withMockHandler(t, ctx)
	mock.GetTokenResult = &auth.Token{AccessToken: "tok"}

	path := filepath.Join(t.TempDir(), "config")
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	// In the static-fallback path the kubeconfig provider (and thus whoami) is
	// unavailable, so --verify must fail even though the entry is written.
	cmd := CommandLogin(embedderParams(), ioStreams, "mycli")
	err := runCmd(t, cmd, ctx, "prod",
		"--handler", "oidc",
		"--server", "https://api.example.com:6443",
		"--kubeconfig", path,
		"--verify",
	)
	require.Error(t, err)
	assert.FileExists(t, path, "kubeconfig entry is written even when verification fails")
	assert.Contains(t, buf.String(), "was written", "user is told the entry was written despite the verify failure")
}

func TestLoginTUIEligibleFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		output string
		want   bool
	}{
		{output: "", want: true},         // auto
		{output: "auto", want: true},     // auto
		{output: "table", want: true},    // human table
		{output: "list", want: true},     // human list
		{output: "json", want: false},    // structured
		{output: "yaml", want: false},    // structured
		{output: "csv", want: false},     // structured
		{output: "toml", want: false},    // structured
		{output: "mermaid", want: false}, // structured
		{output: "quiet", want: false},   // suppressed
		{output: "bogus", want: true},    // unrecognized parses as auto
	}

	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, loginTUIEligibleFormat(tt.output),
				"format %q eligibility", tt.output)
		})
	}
}
