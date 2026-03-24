// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
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
