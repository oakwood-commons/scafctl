// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Capability validation tests ────────────────────────────────────────────────

// TestCommandLogin_TenantNotSupported verifies that --tenant is rejected when
// the handler doesn't declare CapTenantID.
func TestCommandLogin_TenantNotSupported(t *testing.T) {
	ctx, buf := newTestContext(t)

	// GitHub handler does NOT support CapTenantID by default
	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		// No CapTenantID
		auth.CapScopesOnLogin,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--tenant", "some-tenant-id"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant is not supported")
}

// TestCommandLogin_HostnameNotSupported verifies that --hostname is rejected
// when the handler doesn't declare CapHostname.
func TestCommandLogin_HostnameNotSupported(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		// No CapHostname
		auth.CapScopesOnLogin,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--hostname", "github.example.com"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--hostname is not supported")
}

// TestCommandLogin_FederatedTokenNotSupported verifies that --federated-token
// is rejected when the handler doesn't declare CapFederatedToken.
func TestCommandLogin_FederatedTokenNotSupported(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		// No CapFederatedToken
		auth.CapScopesOnLogin,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--federated-token", "some-jwt-token"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--federated-token is not supported")
}

// TestCommandLogin_ScopesNotSupported verifies that --scope is rejected
// when the handler doesn't declare CapScopesOnLogin.
func TestCommandLogin_ScopesNotSupported(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		// No CapScopesOnLogin
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--scope", "openid"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope is not supported at login time")
}

// TestCommandLogin_CallbackPortNotSupported verifies that --callback-port is
// rejected when the handler doesn't declare CapCallbackPort.
func TestCommandLogin_CallbackPortNotSupported(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		// No CapCallbackPort
		auth.CapScopesOnLogin,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--callback-port", "8400"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--callback-port is not supported")
}

// TestCommandLogin_ImpersonateOnNonGCP verifies that --impersonate-service-account
// is only allowed for the gcp handler.
func TestCommandLogin_ImpersonateOnNonGCP(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--impersonate-service-account", "svc@project.iam.gserviceaccount.com"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--impersonate-service-account is only supported by the 'gcp' auth handler")
}

// TestCommandLogin_SkipIfAuthenticated_AlreadyAuthenticated verifies the
// --skip-if-authenticated flag exits successfully when already logged in.
func TestCommandLogin_SkipIfAuthenticated_AlreadyAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetAuthenticated(&auth.Claims{
		Email: "already@example.com",
	})

	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--skip-if-authenticated"})

	err := cmd.Execute()
	require.NoError(t, err)

	// When already authenticated with --skip-if-authenticated, login should not be called
	assert.Len(t, mock.LoginCalls, 0, "Login should not be called when skip-if-authenticated and already logged in")

	output := buf.String()
	assert.Contains(t, output, "skipping login")
}

// TestCommandLogin_InvalidFlow verifies that an invalid --flow value is rejected.
func TestCommandLogin_InvalidFlow(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--flow", "not-a-real-flow"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flow")
}

// TestCommandLogin_WithForce verifies that the --force flag allows re-authentication.
func TestCommandLogin_WithForce(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	// Already authenticated — with --force, should proceed with login
	mock.SetAuthenticated(&auth.Claims{
		Email: "existing@example.com",
	})
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Email: "existing@example.com",
		},
	}
	ctx = withTestHandler(ctx, mock)

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
	cmd := CommandLogin(settings.NewCliParams(), ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--force"})

	err := cmd.Execute()
	require.NoError(t, err, "--force login should succeed")
	assert.Len(t, mock.LoginCalls, 1, "Login should be called once when --force is set")
}

// ── CommandLogin flags tests ───────────────────────────────────────────────────

func TestCommandLogin_FlagsExtra(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")

	flagTests := []struct {
		name     string
		hasShort string
	}{
		{"tenant", ""},
		{"client-id", ""},
		{"hostname", ""},
		{"timeout", ""},
		{"flow", ""},
		{"federated-token", ""},
		{"scope", ""},
		{"impersonate-service-account", ""},
		{"force", "f"},
		{"skip-if-authenticated", ""},
		{"callback-port", ""},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			if tt.hasShort != "" {
				assert.Equal(t, tt.hasShort, f.Shorthand)
			}
		})
	}
}

func BenchmarkCommandLogin_Construction(b *testing.B) {
	cliParams := settings.NewCliParams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var buf bytes.Buffer
		_ = CommandLogin(cliParams, terminal.NewIOStreams(nil, &buf, &buf, false), "scafctl/auth")
	}
}
