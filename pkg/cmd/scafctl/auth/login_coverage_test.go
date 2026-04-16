// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisplayLoginResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   *auth.Result
		flow     auth.Flow
		expected []string
	}{
		{
			name: "full claims interactive",
			result: &auth.Result{
				Claims: &auth.Claims{
					Name:     "Test User",
					Username: "testuser",
					Email:    "test@example.com",
					TenantID: "tenant-123",
				},
				ExpiresAt: time.Now().Add(time.Hour),
			},
			flow:     auth.FlowInteractive,
			expected: []string{"Authentication successful", "Test User", "testuser", "test@example.com", "tenant-123", "Interactive"},
		},
		{
			name: "service principal flow",
			result: &auth.Result{
				Claims: &auth.Claims{
					Email: "sp@example.com",
				},
			},
			flow:     auth.FlowServicePrincipal,
			expected: []string{"Authentication successful", "sp@example.com", "Service Principal"},
		},
		{
			name: "workload identity flow",
			result: &auth.Result{
				Claims: &auth.Claims{
					Email: "wi@example.com",
				},
			},
			flow:     auth.FlowWorkloadIdentity,
			expected: []string{"Authentication successful", "Workload Identity"},
		},
		{
			name: "PAT flow",
			result: &auth.Result{
				Claims: &auth.Claims{
					Username: "ghuser",
				},
			},
			flow:     auth.FlowPAT,
			expected: []string{"Authentication successful", "ghuser", "Personal Access Token"},
		},
		{
			name: "metadata flow",
			result: &auth.Result{
				Claims: &auth.Claims{
					Email: "svc@project.iam.gserviceaccount.com",
				},
			},
			flow:     auth.FlowMetadata,
			expected: []string{"Authentication successful", "Metadata Server"},
		},
		{
			name: "name equals username should not duplicate",
			result: &auth.Result{
				Claims: &auth.Claims{
					Name:     "sameuser",
					Username: "sameuser",
				},
			},
			flow:     auth.FlowInteractive,
			expected: []string{"Authentication successful", "sameuser"},
		},
		{
			name: "minimal claims",
			result: &auth.Result{
				Claims: &auth.Claims{},
			},
			flow:     auth.FlowDeviceCode,
			expected: []string{"Authentication successful"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			streams := terminal.NewIOStreams(nil, &buf, &buf, false)
			w := writer.New(streams, settings.NewCliParams())

			err := displayLoginResult(w, tc.result, tc.flow)
			require.NoError(t, err)

			output := buf.String()
			for _, exp := range tc.expected {
				assert.Contains(t, output, exp)
			}
		})
	}
}

func TestParseFlow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flowStr     string
		handlerName string
		expected    auth.Flow
		wantErr     bool
	}{
		{name: "empty returns default", flowStr: "", handlerName: "entra", expected: auth.Flow(""), wantErr: false},
		{name: "device-code", flowStr: "device-code", handlerName: "entra", expected: auth.FlowDeviceCode, wantErr: false},
		{name: "interactive", flowStr: "interactive", handlerName: "github", expected: auth.FlowInteractive, wantErr: false},
		{name: "service-principal", flowStr: "service-principal", handlerName: "entra", expected: auth.FlowServicePrincipal, wantErr: false},
		{name: "pat", flowStr: "pat", handlerName: "github", expected: auth.FlowPAT, wantErr: false},
		{name: "workload-identity", flowStr: "workload-identity", handlerName: "entra", expected: auth.FlowWorkloadIdentity, wantErr: false},
		{name: "invalid flow", flowStr: "invalid", handlerName: "entra", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flow, err := parseFlow(tc.flowStr, tc.handlerName)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expected, flow)
			}
		})
	}
}

func TestCommandLogin_Structure(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")

	assert.Equal(t, "login <handler>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandLogin_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")

	flags := cmd.Flags()
	assert.NotNil(t, flags.Lookup("tenant"), "tenant flag should exist")
	assert.NotNil(t, flags.Lookup("client-id"), "client-id flag should exist")
	assert.NotNil(t, flags.Lookup("hostname"), "hostname flag should exist")
	assert.NotNil(t, flags.Lookup("timeout"), "timeout flag should exist")
	assert.NotNil(t, flags.Lookup("flow"), "flow flag should exist")
	assert.NotNil(t, flags.Lookup("federated-token"), "federated-token flag should exist")
	assert.NotNil(t, flags.Lookup("scope"), "scope flag should exist")
	assert.NotNil(t, flags.Lookup("impersonate-service-account"), "impersonate flag should exist")
	assert.NotNil(t, flags.Lookup("force"), "force flag should exist")
	assert.NotNil(t, flags.Lookup("skip-if-authenticated"), "skip-if-authenticated flag should exist")
	assert.NotNil(t, flags.Lookup("callback-port"), "callback-port flag should exist")
}

func TestCommandLogin_FlagDefaults(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	flags := cmd.Flags()

	timeout := flags.Lookup("timeout")
	require.NotNil(t, timeout)
	assert.Equal(t, "5m0s", timeout.DefValue)

	force := flags.Lookup("force")
	require.NotNil(t, force)
	assert.Equal(t, "false", force.DefValue)
	assert.Equal(t, "f", force.Shorthand)

	skipAuth := flags.Lookup("skip-if-authenticated")
	require.NotNil(t, skipAuth)
	assert.Equal(t, "false", skipAuth.DefValue)

	callbackPort := flags.Lookup("callback-port")
	require.NotNil(t, callbackPort)
	assert.Equal(t, "0", callbackPort.DefValue)
}

func TestCommandLogin_UnsupportedCapabilityFlags(t *testing.T) {
	t.Parallel()

	// Test that --hostname on a handler without CapHostname fails
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
	}
	// No CapHostname

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--hostname", "enterprise.example.com"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--hostname is not supported")
}

func TestCommandLogin_ImpersonateServiceAccount_NonGCP(t *testing.T) {
	t.Parallel()

	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--impersonate-service-account", "sa@project.iam.gserviceaccount.com"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--impersonate-service-account is only supported")
}

func TestCommandLogin_SkipIfAuthenticated(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetAuthenticated(&auth.Claims{
		Email: "existing@example.com",
	})

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--skip-if-authenticated"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Already authenticated")
	assert.Contains(t, output, "skipping login")

	// Login should NOT have been called
	assert.Empty(t, mock.LoginCalls)
}

func TestCommandLogin_ForceReAuth(t *testing.T) {
	t.Parallel()
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetAuthenticated(&auth.Claims{
		Email: "existing@example.com",
	})
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Email: "new@example.com",
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--force"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Login should have been called despite existing auth
	require.NotEmpty(t, mock.LoginCalls)
}

// Benchmarks

func BenchmarkCommandLogin(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandLogin(cliParams, ioStreams, "scafctl/auth")
	}
}

func BenchmarkDisplayLoginResult(b *testing.B) {
	var buf bytes.Buffer
	streams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(streams, settings.NewCliParams())
	result := &auth.Result{
		Claims: &auth.Claims{
			Name:     "Test User",
			Email:    "test@example.com",
			TenantID: "tenant-123",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_ = displayLoginResult(w, result, auth.FlowInteractive)
	}
}

func TestCommandLogin_CustomHandler_Success(t *testing.T) {
	// Tests the loginGeneric() path (non-built-in handler named "quay")
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims:    &auth.Claims{Email: "robot@quay.io"},
		ExpiresAt: time.Now().Add(time.Hour),
	}
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Len(t, mock.LoginCalls, 1)
	assert.Contains(t, buf.String(), "Authentication successful")
}

func TestCommandLogin_CustomHandler_AlreadyAuthenticated(t *testing.T) {
	// loginGeneric should print a warning when already authenticated
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetAuthenticated(&auth.Claims{Email: "robot@quay.io"})
	mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "robot@quay.io"}}
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay"})

	err := cmd.Execute()
	require.NoError(t, err)
	// Should still call Login (PreLoginAlreadyAuthenticated just warns)
	require.Len(t, mock.LoginCalls, 1)
	out := buf.String()
	assert.Contains(t, out, "Already authenticated")
}

func TestCommandLogin_CustomHandler_SkipIfAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetAuthenticated(&auth.Claims{Email: "robot@quay.io"})
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay", "--skip-if-authenticated"})

	err := cmd.Execute()
	require.NoError(t, err)
	// Should NOT call Login since skip-if-authenticated is set
	assert.Empty(t, mock.LoginCalls)
	assert.Contains(t, buf.String(), "skipping login")
}

func TestCommandLogin_CustomHandler_Force(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetAuthenticated(&auth.Claims{Email: "robot@quay.io"})
	mock.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "robot@quay.io"}}
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay", "--force"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Len(t, mock.LoginCalls, 1)
}

// TestBridgeAuthToRegistryPostLogin_Success verifies that credentials are stored
// after a successful token bridge.
func TestBridgeAuthToRegistryPostLogin_Success(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx, buf := newTestContext(t)
	w := writer.New(terminal.NewIOStreams(nil, buf, buf, false), settings.NewCliParams())

	mock := auth.NewMockHandler("quay")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "fake-registry-token",
		TokenType:   "Bearer",
	}

	err := bridgeAuthToRegistryPostLogin(ctx, w, mock, "quay", "quay.io", "", false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Registry credentials stored for quay.io")

	// Verify the credential is written to the isolated native store.
	// Use the same secrets backend to retrieve the encrypted password.
	ss, err := secrets.New()
	require.NoError(t, err)
	store := catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	cred, err := store.GetCredential("quay.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "fake-registry-token", cred.Password)
}

// TestBridgeAuthToRegistryPostLogin_GetTokenError verifies that a GetToken failure
// is propagated as an error.
func TestBridgeAuthToRegistryPostLogin_GetTokenError(t *testing.T) {
	ctx, buf := newTestContext(t)
	w := writer.New(terminal.NewIOStreams(nil, buf, buf, false), settings.NewCliParams())

	mock := auth.NewMockHandler("quay")
	mock.GetTokenErr = fmt.Errorf("token exchange failed")

	err := bridgeAuthToRegistryPostLogin(ctx, w, mock, "quay", "quay.io", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to bridge quay auth to registry quay.io")
}

// TestCommandLogin_WithRegistryBridge tests the --registry flag path after successful login.
func TestCommandLogin_WithRegistryBridge(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{Email: "robot@quay.io"},
	}
	mock.GetTokenResult = &auth.Token{
		AccessToken: "fake-oci-token",
		TokenType:   "Bearer",
	}
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay", "--registry", "quay.io"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Registry credentials stored for quay.io")
}

// TestDiscoverRegistriesForHandler_CustomOAuth2 verifies that the handler's
// Registry field is picked up when no --registry flag is provided.
func TestDiscoverRegistriesForHandler_CustomOAuth2(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "quay", Registry: "quay.io"},
				{Name: "other", Registry: "other.io"},
			},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	registries := discoverRegistriesForHandler(ctx, "quay")
	assert.Equal(t, []registryWithScope{{Host: "quay.io"}}, registries)
}

// TestDiscoverRegistriesForHandler_CatalogAuthProvider verifies that catalogs
// with a matching authProvider contribute their registry host and scope.
func TestDiscoverRegistriesForHandler_CatalogAuthProvider(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "prod", URL: "oci://ghcr.io/myorg/catalog", AuthProvider: "github"},
			{Name: "staging", URL: "oci://ghcr.io/myorg/staging", AuthProvider: "github"},
			{Name: "unrelated", URL: "oci://quay.io/myorg", AuthProvider: "quay"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	registries := discoverRegistriesForHandler(ctx, "github")
	// ghcr.io appears in two catalogs but should be deduplicated.
	assert.Equal(t, []registryWithScope{{Host: "ghcr.io"}}, registries)
}

// TestDiscoverRegistriesForHandler_CatalogAuthScope verifies that the catalog's
// authScope is carried through to the discovered registry.
func TestDiscoverRegistriesForHandler_CatalogAuthScope(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "gcp-catalog", URL: "oci://us-central1-docker.pkg.dev/proj/repo", AuthProvider: "gcp", AuthScope: "https://www.googleapis.com/auth/cloud-platform"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	registries := discoverRegistriesForHandler(ctx, "gcp")
	assert.Equal(t, []registryWithScope{{
		Host:  "us-central1-docker.pkg.dev",
		Scope: "https://www.googleapis.com/auth/cloud-platform",
	}}, registries)
}

// TestDiscoverRegistriesForHandler_CombinedSources verifies that both custom
// handler registry and catalog auth providers are merged and deduplicated.
func TestDiscoverRegistriesForHandler_CombinedSources(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "quay", Registry: "quay.io"},
			},
		},
		Catalogs: []config.CatalogConfig{
			{Name: "prod", URL: "oci://quay.io/myorg/catalog", AuthProvider: "quay"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	registries := discoverRegistriesForHandler(ctx, "quay")
	// quay.io from handler config and catalog should be deduplicated.
	assert.Equal(t, []registryWithScope{{Host: "quay.io"}}, registries)
}

// TestDiscoverRegistriesForHandler_NoConfig returns nil when no config is in context.
func TestDiscoverRegistriesForHandler_NoConfig(t *testing.T) {
	t.Parallel()

	registries := discoverRegistriesForHandler(context.Background(), "github")
	assert.Nil(t, registries)
}

// TestDiscoverRegistriesForHandler_NoMatch returns nil when no registries match.
func TestDiscoverRegistriesForHandler_NoMatch(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "quay", Registry: "quay.io"},
			},
		},
		Catalogs: []config.CatalogConfig{
			{Name: "prod", URL: "oci://ghcr.io/myorg", AuthProvider: "github"},
		},
	}
	ctx := config.WithConfig(context.Background(), cfg)

	registries := discoverRegistriesForHandler(ctx, "entra")
	assert.Nil(t, registries)
}

// TestCommandLogin_AutoBridgeFromConfig verifies that auth login automatically
// bridges to registries discovered from config (no --registry needed).
func TestCommandLogin_AutoBridgeFromConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("quay")
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{Email: "robot@quay.io"},
	}
	mock.GetTokenResult = &auth.Token{
		AccessToken: "auto-bridged-token",
		TokenType:   "Bearer",
	}
	ctx = withTestHandler(ctx, mock)

	// Inject config with a custom handler that has a registry field.
	cfg := &config.Config{
		Auth: config.GlobalAuthConfig{
			CustomOAuth2: []config.CustomOAuth2Config{
				{Name: "quay", Registry: "quay.io"},
			},
		},
	}
	ctx = config.WithConfig(ctx, cfg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"quay"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Registry credentials stored for quay.io")

	// Verify credential was stored.
	ss, err := secrets.New()
	require.NoError(t, err)
	store := catalog.NewNativeCredentialStoreWithSecretsStore(ss)
	cred, err := store.GetCredential("quay.io")
	require.NoError(t, err)
	require.NotNil(t, cred)
	assert.Equal(t, "auto-bridged-token", cred.Password)
}

// TestBridgeAuthToRegistryPostLogin_WithScope verifies that bridge passes the
// scope through to GetToken when a scope is provided.
func TestBridgeAuthToRegistryPostLogin_WithScope(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx, buf := newTestContext(t)
	w := writer.New(terminal.NewIOStreams(nil, buf, buf, false), settings.NewCliParams())

	mock := auth.NewMockHandler("gcp")
	mock.GetTokenResult = &auth.Token{
		AccessToken: "scoped-token",
		TokenType:   "Bearer",
	}

	err := bridgeAuthToRegistryPostLogin(ctx, w, mock, "gcp", "us-central1-docker.pkg.dev", "https://www.googleapis.com/auth/cloud-platform", false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Registry credentials stored for us-central1-docker.pkg.dev")

	// Verify GetToken was called with the scope.
	require.NotEmpty(t, mock.GetTokenCalls)
	lastCall := mock.GetTokenCalls[len(mock.GetTokenCalls)-1]
	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", lastCall.Scope)
}

// TestCommandLogin_AutoBridgeWithScope verifies that auto-bridge uses the
// catalog's authScope when --registry-scope CLI flag is empty.
func TestCommandLogin_AutoBridgeWithScope(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("gcp")
	mock.SetNotAuthenticated()
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
	}
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{Email: "svc@project.iam.gserviceaccount.com"},
	}
	mock.GetTokenResult = &auth.Token{
		AccessToken: "scoped-auto-token",
		TokenType:   "Bearer",
	}
	ctx = withTestHandler(ctx, mock)

	// Config has a catalog with authProvider=gcp and an authScope.
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{
				Name:         "gcp-cat",
				Type:         "oci",
				URL:          "oci://us-central1-docker.pkg.dev/proj/repo",
				AuthProvider: "gcp",
				AuthScope:    "https://www.googleapis.com/auth/cloud-platform",
			},
		},
	}
	ctx = config.WithConfig(ctx, cfg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	// No --registry-scope flag -- should use config-discovered scope.
	cmd.SetArgs([]string{"gcp"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Registry credentials stored for us-central1-docker.pkg.dev")

	// Verify GetToken was called with the discovered scope.
	require.NotEmpty(t, mock.GetTokenCalls)
	lastCall := mock.GetTokenCalls[len(mock.GetTokenCalls)-1]
	assert.Equal(t, "https://www.googleapis.com/auth/cloud-platform", lastCall.Scope)
}

// TestCommandLogin_EmbedderBinaryName verifies that CommandLogin works with a
// non-default binary name (embedder contract compliance).
func TestCommandLogin_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandLogin(cliParams, ioStreams, "mycli/auth")

	assert.Equal(t, "login <handler>", cmd.Use)
	assert.NotNil(t, cmd.RunE)
}
