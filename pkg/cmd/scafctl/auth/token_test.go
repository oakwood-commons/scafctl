// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newEntraMock creates a mock handler with Entra-like capabilities (supports per-request scopes).
func newEntraMock() *auth.MockHandler {
	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	return mock
}

// newGitHubMock creates a mock handler with GitHub-like capabilities (no per-request scopes).
func newGitHubMock() *auth.MockHandler {
	mock := auth.NewMockHandler("github")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapHostname,
	}
	return mock
}

func TestCommandToken_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown", "--scope", "test"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandToken_MissingHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument: <handler>")
}

func TestCommandToken_MissingScopeForEntra(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := newEntraMock()
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scope")
	assert.Contains(t, err.Error(), "required")
}

func TestCommandToken_ScopeRejectedForGitHub(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := newGitHubMock()
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "--scope", "repo"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support per-request scopes")
	assert.Contains(t, err.Error(), "scafctl auth login")
}

func TestCommandToken_GitHubSuccessWithoutScope(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newGitHubMock()
	mock.SetToken(&auth.Token{
		AccessToken: "gho_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "repo read:user",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with empty scope
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "", mock.GetTokenCalls[0].Scope)

	// Default output is the raw token (scriptable), not the metadata object.
	output := buf.String()
	assert.Equal(t, "gho_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", strings.TrimSpace(output))
	assert.NotContains(t, output, "handler")
}

func TestCommandToken_Success(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://graph.microsoft.com/.default", mock.GetTokenCalls[0].Scope)

	// Default output is the raw access token for scriptability.
	output := buf.String()
	assert.Equal(t, "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP", strings.TrimSpace(output))
	assert.NotContains(t, output, "tokenType")
	assert.NotContains(t, output, "handler")
}

func TestCommandToken_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify output includes full token in JSON
	output := buf.String()
	assert.Contains(t, output, "accessToken")
	assert.Contains(t, output, "eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6Ik1uQ19WWmNBVGZNNXBP")
}

func TestCommandToken_WithMinValidFor(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "test-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope", "--min-valid-for", "5m"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with correct min-valid-for
	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, 5*time.Minute, mock.GetTokenCalls[0].MinValidFor)
}

func TestCommandToken_ForceRefresh(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "fresh-token-value-that-is-long-enough",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope", "--force-refresh"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify GetToken was called with ForceRefresh=true
	require.Len(t, mock.GetTokenCalls, 1)
	assert.True(t, mock.GetTokenCalls[0].ForceRefresh)
}

func TestCommandToken_NotAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetTokenError(auth.ErrNotAuthenticated)

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get token")
}

func TestCommandToken_TokenError(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetTokenError(errors.New("token refresh failed"))

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get token")
	assert.Contains(t, err.Error(), "token refresh failed")
}

func TestCommandToken_ShortToken(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "short", // Less than 20 chars
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "test-scope",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "test-scope"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Short token should be shown without masking
	output := buf.String()
	assert.Contains(t, output, "short")
	assert.NotContains(t, output, "...")
}

// decodeExecCredential parses the command output as an ExecCredential JSON.
func decodeExecCredential(t *testing.T, output string) map[string]any {
	t.Helper()
	var ec map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(output)), &ec))
	return ec
}

func TestCommandToken_ExecCredential(t *testing.T) {
	ctx, buf := newTestContext(t)

	expiry := time.Now().Add(time.Hour).UTC()
	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "eyJexec-cred-token",
		TokenType:   "Bearer",
		ExpiresAt:   expiry,
		Scope:       "https://graph.microsoft.com/.default",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "--exec-credential"})

	require.NoError(t, cmd.Execute())

	ec := decodeExecCredential(t, buf.String())
	assert.Equal(t, "client.authentication.k8s.io/v1", ec["apiVersion"])
	assert.Equal(t, "ExecCredential", ec["kind"])
	status, ok := ec["status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "eyJexec-cred-token", status["token"])
	assert.Equal(t, expiry.Format(time.RFC3339), status["expirationTimestamp"])
}

func TestCommandToken_ExecCredentialAutoDetect(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","spec":{}}`)

	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "auto-detected-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	// No --exec-credential flag: presence of KUBERNETES_EXEC_INFO triggers it.
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default"})

	require.NoError(t, cmd.Execute())

	ec := decodeExecCredential(t, buf.String())
	status, ok := ec["status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "auto-detected-token", status["token"])
}

// TestCommandToken_ExecCredentialNoScopeForScopeCapableHandler verifies that a
// handler advertising CapScopesOnTokenRequest may be used in exec-credential
// mode WITHOUT --scope: the kubeconfig exec block wants the held (empty-scope)
// user token, so requiring a scope would break kubectl/oc. Regression for the
// openshift credential-helper flow.
func TestCommandToken_ExecCredentialNoScopeForScopeCapableHandler(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","spec":{}}`)

	ctx, buf := newTestContext(t)

	mock := newEntraMock() // advertises CapScopesOnTokenRequest
	mock.SetToken(&auth.Token{AccessToken: "held-user-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandToken(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--exec-credential"}) // no --scope

	require.NoError(t, cmd.Execute())

	ec := decodeExecCredential(t, buf.String())
	status, ok := ec["status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "held-user-token", status["token"])
}

// TestCommandToken_InteractiveNoScopeStillRequiredForScopeCapableHandler
// verifies the scope-required check is relaxed ONLY in exec-credential mode: an
// interactive `auth token <handler>` with no --scope for a scope-capable
// handler still errors early.
func TestCommandToken_InteractiveNoScopeStillRequiredForScopeCapableHandler(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", "") // ensure exec mode is not auto-detected

	ctx, buf := newTestContext(t)

	mock := newEntraMock() // advertises CapScopesOnTokenRequest
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandToken(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"}) // no --scope, no exec mode

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope is required")
}

func TestCommandToken_ExecCredentialForwardsClusterHostname(t *testing.T) {
	// Regression test for #581: with provideClusterInfo the exec info carries
	// the target cluster's server, which must be forwarded as the token
	// hostname so cluster-aware handlers mint the correct per-cluster token.
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","spec":{"cluster":{"server":"https://api.a.example.com:6443"}}}`)

	ctx, buf := newTestContext(t)

	mock := newGitHubMock()
	mock.CapabilitiesValue = append(mock.CapabilitiesValue, auth.CapTokenHostname)
	mock.SetToken(&auth.Token{AccessToken: "cluster-a-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	require.NoError(t, cmd.Execute())

	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://api.a.example.com:6443", mock.GetTokenCalls[0].Hostname,
		"cluster server must be forwarded as the token hostname for CapTokenHostname handlers")
}

func TestCommandToken_ExecCredentialHostnameGatedByCapability(t *testing.T) {
	// A handler that advertises only CapHostname (login) but not
	// CapTokenHostname must not receive the token hostname: it would silently
	// ignore it (older SDK) and return its default token, so the host must not
	// rely on it. See #583.
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","spec":{"cluster":{"server":"https://api.a.example.com:6443"}}}`)

	ctx, buf := newTestContext(t)

	mock := newGitHubMock() // caps: CapScopesOnLogin, CapHostname (no CapTokenHostname)
	mock.SetToken(&auth.Token{AccessToken: "default-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	require.NoError(t, cmd.Execute())

	require.Len(t, mock.GetTokenCalls, 1)
	assert.Empty(t, mock.GetTokenCalls[0].Hostname,
		"token hostname must not be forwarded to handlers lacking CapTokenHostname")
}

func TestCommandToken_ServerFlagSelectsClusterHostname(t *testing.T) {
	// #581: --server lets callers request a specific cluster's token without
	// kubectl, and takes precedence over KUBERNETES_EXEC_INFO.
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","spec":{"cluster":{"server":"https://api.b.example.com:6443"}}}`)

	ctx, buf := newTestContext(t)

	mock := newGitHubMock()
	mock.CapabilitiesValue = append(mock.CapabilitiesValue, auth.CapTokenHostname)
	mock.SetToken(&auth.Token{AccessToken: "cluster-a-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "--server", "https://api.a.example.com:6443", "--raw"})

	require.NoError(t, cmd.Execute())

	require.Len(t, mock.GetTokenCalls, 1)
	assert.Equal(t, "https://api.a.example.com:6443", mock.GetTokenCalls[0].Hostname,
		"--server must override the exec-info cluster server")
}

func TestCommandToken_ServerFlagGatedByCapability(t *testing.T) {
	// --server is ignored for handlers that do not advertise CapTokenHostname.
	ctx, buf := newTestContext(t)

	mock := newGitHubMock() // no CapTokenHostname
	mock.SetToken(&auth.Token{AccessToken: "default-token", TokenType: "Bearer", ExpiresAt: time.Now().Add(time.Hour)})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "--server", "https://api.a.example.com:6443", "--raw"})

	require.NoError(t, cmd.Execute())

	require.Len(t, mock.GetTokenCalls, 1)
	assert.Empty(t, mock.GetTokenCalls[0].Hostname)
}

func TestCommandToken_ExecCredentialEchoesAPIVersion(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1beta1","kind":"ExecCredential","spec":{}}`)

	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "beta-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "--exec-credential"})

	require.NoError(t, cmd.Execute())

	ec := decodeExecCredential(t, buf.String())
	assert.Equal(t, "client.authentication.k8s.io/v1beta1", ec["apiVersion"])
}

func TestCommandToken_ExplicitFormatBeatsExecInfo(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1"}`)

	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "explicit-json-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	// Explicit -o json must win over KUBERNETES_EXEC_INFO auto-detect.
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "-o", "json"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "tokenType")
	assert.NotContains(t, output, "ExecCredential")
}

func TestCommandToken_ExpressionBeatsExecInfo(t *testing.T) {
	t.Setenv("KUBERNETES_EXEC_INFO", `{"apiVersion":"client.authentication.k8s.io/v1"}`)

	ctx, buf := newTestContext(t)

	mock := newEntraMock()
	mock.SetToken(&auth.Token{
		AccessToken: "expression-token",
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(time.Hour),
		Scope:       "https://graph.microsoft.com/.default",
	})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandToken(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	// An explicit -e/--expression must win over KUBERNETES_EXEC_INFO auto-detect.
	cmd.SetArgs([]string{"entra", "--scope", "https://graph.microsoft.com/.default", "-e", "_.handler"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "entra")
	assert.NotContains(t, output, "ExecCredential")
}
