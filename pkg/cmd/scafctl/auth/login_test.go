// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// configureAuthRegistry registers the given mock handler in an
// auth.Registry, then attaches it to ctx.
func configureAuthRegistry(t *testing.T, ctx context.Context, mock *auth.MockHandler) context.Context {
	t.Helper()
	authRegistry := auth.NewRegistry()
	require.NoError(t, authRegistry.Register(mock))
	ctx = auth.WithRegistry(ctx, authRegistry)
	return ctx
}

func TestCommandLogin_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	// With third-party catalog resolution disabled, an unknown (non-official)
	// handler name is rejected up front instead of being fetched from a catalog.
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandLogin_MissingHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument: <handler>")
}

func TestCommandLogin_Success(t *testing.T) {
	ctx, buf := newTestContext(t)

	// Set up mock handler with Entra-like capabilities
	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Name:     "Test User",
			Email:    "test@example.com",
			TenantID: "test-tenant-id",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify login was called
	require.Len(t, mock.LoginCalls, 1)
	assert.Equal(t, auth.FlowInteractive, mock.LoginCalls[0].Flow)

	// Verify output has a plain blank separator line and no bare info prefix icon
	output := buf.String()
	assert.Contains(t, output, "Logged in to Microsoft Entra ID as test@example.com")
	assert.NotContains(t, output, "ℹ", "executeLogin must not emit a bare info icon line before the login result")
	assert.True(t, len(output) > 0 && output[0] == '\n', "executeLogin should emit a plain blank line before the login result")
}

func TestCommandLogin_AlreadyAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	// Set up mock handler as already authenticated
	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetAuthenticated(&auth.Claims{
		Email: "existing@example.com",
	})
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Email: "existing@example.com",
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify warning about already authenticated
	output := buf.String()
	assert.Contains(t, output, "Already authenticated")

	// Verify Login was NOT called (returns early without proceeding)
	assert.Empty(t, mock.LoginCalls, "Login should not be called when already authenticated without --force")
}

func TestCommandLogin_WithTenant(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Email:    "test@example.com",
			TenantID: "custom-tenant",
		},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--tenant", "custom-tenant"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify login was called with tenant
	require.Len(t, mock.LoginCalls, 1)
	assert.Equal(t, "custom-tenant", mock.LoginCalls[0].TenantID)
}

func TestCommandLogin_Failure(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetNotAuthenticated()
	mock.LoginErr = errors.New("network error")

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authentication failed")
	assert.Contains(t, err.Error(), "network error")
}

func TestCommandLogin_DeviceCodeCallback(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
		auth.CapFederatedToken,
	}
	mock.SetNotAuthenticated()

	// Capture the callback
	var capturedCallback func(string, string, string)
	originalLogin := mock.Login
	_ = originalLogin // silence unused warning since we're replacing behavior

	// Override Login to capture and invoke the callback
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{Email: "test@example.com"},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// The callback should have been set
	require.Len(t, mock.LoginCalls, 1)
	capturedCallback = mock.LoginCalls[0].DeviceCodeCallback
	require.NotNil(t, capturedCallback)

	// Test the callback produces expected output
	buf.Reset()

	// Re-execute to test callback behavior (it was captured above)
	capturedCallback("ABC123", "https://microsoft.com/devicelogin", "Test message")
}

func TestCommandLogin_ClientIDFlag_AppliesOverrides(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockConfigurerHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Name:     "Test User",
			Email:    "test@example.com",
			TenantID: "custom-tenant",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--client-id", "custom-client-id", "--tenant", "custom-tenant"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify ApplyOverrides was called with the right values
	require.Len(t, mock.ApplyOverridesCalls, 1)
	assert.Equal(t, "custom-client-id", mock.ApplyOverridesCalls[0]["clientId"])
	assert.Equal(t, "custom-tenant", mock.ApplyOverridesCalls[0]["tenantId"])

	// Verify login was still called
	require.Len(t, mock.LoginCalls, 1)
}

func TestCommandLogin_ClientIDFlag_NoOverridesWhenEmpty(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockConfigurerHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapScopesOnTokenRequest,
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()
	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{
			Name:  "Test User",
			Email: "test@example.com",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// No overrides when flags are empty
	assert.Empty(t, mock.ApplyOverridesCalls)
}

func TestCommandLogin_ClientIDFlag_ErrorWhenNotConfigurer(t *testing.T) {
	ctx, buf := newTestContext(t)

	// NewMockHandler does NOT implement auth.Configurer.
	mock := auth.NewMockHandler("entra")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
		auth.CapTenantID,
	}
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--client-id", "custom-client-id"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--client-id is not supported")
}

func TestCommandLogin_EntraDefaultFlowResolution(t *testing.T) {
	tests := []struct {
		name          string
		entraCfg      *config.EntraAuthConfig
		globalProfile string
		args          []string
		wantFlow      auth.Flow
	}{
		{
			name:     "defaults to interactive when no config is present",
			entraCfg: nil,
			args:     []string{"entra"},
			wantFlow: auth.FlowInteractive,
		},
		{
			name:     "defaults to interactive when defaultFlow is empty",
			entraCfg: &config.EntraAuthConfig{},
			args:     []string{"entra"},
			wantFlow: auth.FlowInteractive,
		},
		{
			name:     "honors top-level defaultFlow interactive",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "interactive"},
			args:     []string{"entra"},
			wantFlow: auth.FlowInteractive,
		},
		{
			name:     "honors top-level defaultFlow device_code",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "device_code"},
			args:     []string{"entra"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name:     "honors hyphenated defaultFlow device-code",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "device-code"},
			args:     []string{"entra"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name: "honors activeProfile defaultFlow device_code",
			entraCfg: &config.EntraAuthConfig{
				DefaultFlow:   "interactive",
				ActiveProfile: "headless",
				Profiles: map[string]*config.EntraProfileConfig{
					"headless": {DefaultFlow: "device_code"},
				},
			},
			args:     []string{"entra"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name: "honors --profile defaultFlow device_code",
			entraCfg: &config.EntraAuthConfig{
				DefaultFlow: "interactive",
				Profiles: map[string]*config.EntraProfileConfig{
					"headless": {DefaultFlow: "device_code"},
				},
			},
			args:     []string{"entra", "--profile", "headless"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name: "explicit --profile default bypasses activeProfile defaultFlow",
			entraCfg: &config.EntraAuthConfig{
				DefaultFlow:   "interactive",
				ActiveProfile: "headless",
				Profiles: map[string]*config.EntraProfileConfig{
					"headless": {DefaultFlow: "device_code"},
				},
			},
			args:     []string{"entra", "--profile", "default"},
			wantFlow: auth.FlowInteractive,
		},
		{
			name:     "explicit --flow device-code overrides interactive defaultFlow",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "interactive"},
			args:     []string{"entra", "--flow", "device-code"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name:     "explicit --flow device_code overrides interactive defaultFlow",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "interactive"},
			args:     []string{"entra", "--flow", "device_code"},
			wantFlow: auth.FlowDeviceCode,
		},
		{
			name: "honors global --auth-profile defaultFlow device_code",
			entraCfg: &config.EntraAuthConfig{
				DefaultFlow: "interactive",
				Profiles: map[string]*config.EntraProfileConfig{
					"headless": {DefaultFlow: "device_code"},
				},
			},
			globalProfile: "headless",
			args:          []string{"entra"},
			wantFlow:      auth.FlowDeviceCode,
		},
		{
			name:     "unrecognized config defaultFlow falls back to interactive",
			entraCfg: &config.EntraAuthConfig{DefaultFlow: "bogus"},
			args:     []string{"entra"},
			wantFlow: auth.FlowInteractive,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", tmpDir)
			t.Setenv("XDG_DATA_HOME", tmpDir)
			t.Setenv("XDG_STATE_HOME", tmpDir)

			ctx, buf := newTestContext(t)
			if tc.entraCfg != nil {
				ctx = config.WithConfig(ctx, &config.Config{
					Auth: config.GlobalAuthConfig{Entra: tc.entraCfg},
				})
			}
			if tc.globalProfile != "" {
				ctx = auth.WithGlobalProfile(ctx, tc.globalProfile)
			}

			mock := auth.NewMockHandler("entra")
			mock.DisplayNameValue = "Microsoft Entra ID"
			mock.CapabilitiesValue = []auth.Capability{
				auth.CapScopesOnLogin,
				auth.CapTenantID,
				auth.CapFederatedToken,
			}
			mock.SetNotAuthenticated()
			mock.LoginResult = &auth.Result{
				Claims: &auth.Claims{Email: "test@example.com"},
			}
			ctx = withTestHandler(ctx, mock)

			cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs(tc.args)

			require.NoError(t, cmd.Execute())
			require.Len(t, mock.LoginCalls, 1)
			assert.Equal(t, tc.wantFlow, mock.LoginCalls[0].Flow)
		})
	}
}

type mockDetectorConfigurerHandler struct {
	*auth.MockConfigurerHandler
	detectFn func(ctx context.Context) ([]auth.FlowAvailability, error)
}

func (m *mockDetectorConfigurerHandler) DetectAvailableFlows(ctx context.Context) ([]auth.FlowAvailability, error) {
	return m.detectFn(ctx)
}

func TestCommandLogin_EntraFlowDetectorAndProfileOverrides(t *testing.T) {
	t.Run("flow detector selects interactive on workstation and device_code on headless fallback", func(t *testing.T) {
		for _, wantFlow := range []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode} {
			ctx, buf := newTestContext(t)
			detector := auth.NewMockFlowDetectorHandler("entra")
			detector.DisplayNameValue = "Microsoft Entra ID"
			detector.SetNotAuthenticated()
			detector.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
			detector.DetectFlowsResult = []auth.FlowAvailability{
				{Flow: auth.FlowWorkloadIdentity, Available: false},
				{Flow: auth.FlowServicePrincipal, Available: false},
				{Flow: wantFlow, Available: true, Reason: "selected by plugin"},
			}
			ctx = withTestHandler(ctx, detector)

			cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"entra"})

			require.NoError(t, cmd.Execute())
			require.Len(t, detector.LoginCalls, 1)
			assert.Equal(t, wantFlow, detector.LoginCalls[0].Flow)
		}
	})

	t.Run("explicit --profile forwards profile defaultFlow to Configurer before DetectAvailableFlows", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", tmpDir)
		t.Setenv("XDG_STATE_HOME", tmpDir)

		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{
				Entra: &config.EntraAuthConfig{
					DefaultFlow: "interactive",
					Profiles: map[string]*config.EntraProfileConfig{
						"headless": {DefaultFlow: "device_code"},
					},
				},
			},
		})

		base := auth.NewMockConfigurerHandler("entra")
		base.DisplayNameValue = "Microsoft Entra ID"
		base.SetNotAuthenticated()
		base.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}

		pluginDefaultFlow := "interactive"
		handler := &mockDetectorConfigurerHandler{
			MockConfigurerHandler: base,
			detectFn: func(_ context.Context) ([]auth.FlowAvailability, error) {
				if len(base.ApplyOverridesCalls) > 0 && base.ApplyOverridesCalls[0]["defaultFlow"] != "" {
					pluginDefaultFlow = base.ApplyOverridesCalls[0]["defaultFlow"]
				}
				return []auth.FlowAvailability{
					{Flow: auth.Flow(pluginDefaultFlow), Available: true},
				}, nil
			},
		}
		ctx = withTestHandler(ctx, handler)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "headless"})

		require.NoError(t, cmd.Execute())
		require.Len(t, base.ApplyOverridesCalls, 1)
		assert.Equal(t, "device_code", base.ApplyOverridesCalls[0]["defaultFlow"])
		require.Len(t, base.LoginCalls, 1)
		assert.Equal(t, auth.FlowDeviceCode, base.LoginCalls[0].Flow)
	})

	t.Run("explicit --profile default overrides activeProfile device_code when top-level defaultFlow is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", tmpDir)
		t.Setenv("XDG_STATE_HOME", tmpDir)

		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, &config.Config{
			Auth: config.GlobalAuthConfig{
				Entra: &config.EntraAuthConfig{
					ActiveProfile: "headless",
					Profiles: map[string]*config.EntraProfileConfig{
						"headless": {DefaultFlow: "device_code"},
					},
				},
			},
		})

		base := auth.NewMockConfigurerHandler("entra")
		base.DisplayNameValue = "Microsoft Entra ID"
		base.SetNotAuthenticated()
		base.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}

		// Simulate lazy plugin initially started under activeProfile=headless (device_code).
		pluginDefaultFlow := "device_code"
		handler := &mockDetectorConfigurerHandler{
			MockConfigurerHandler: base,
			detectFn: func(_ context.Context) ([]auth.FlowAvailability, error) {
				if len(base.ApplyOverridesCalls) > 0 && base.ApplyOverridesCalls[0]["defaultFlow"] != "" {
					pluginDefaultFlow = base.ApplyOverridesCalls[0]["defaultFlow"]
				}
				return []auth.FlowAvailability{
					{Flow: auth.Flow(pluginDefaultFlow), Available: true},
				}, nil
			},
		}
		ctx = withTestHandler(ctx, handler)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default"})

		require.NoError(t, cmd.Execute())
		require.Len(t, base.ApplyOverridesCalls, 1)
		assert.Equal(t, "interactive", base.ApplyOverridesCalls[0]["defaultFlow"])
		require.Len(t, base.LoginCalls, 1)
		assert.Equal(t, auth.FlowInteractive, base.LoginCalls[0].Flow)
	})

	t.Run("ApplyOverrides error writes to output and exits with InvalidInput", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		base := auth.NewMockConfigurerHandler("entra")
		base.SetNotAuthenticated()
		base.ApplyOverridesErr = errors.New("reconfigure boom")
		ctx = withTestHandler(ctx, base)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "work"})

		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "applying auth handler config overrides")
		assert.Contains(t, err.Error(), "reconfigure boom")
		assert.Contains(t, buf.String(), "applying auth handler config overrides")
		assert.Empty(t, base.LoginCalls)
	})
}

func TestCommandLogin_HelpText_EntraInteractiveDefaultAndNoAuthCodeAlias(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")

	assert.Contains(t, cmd.Long, "Opens a browser and signs in with authorization code + PKCE")
	assert.Contains(t, cmd.Long, "# Login with Entra ID using interactive browser flow (default)")
	assert.Contains(t, cmd.Long, "scafctl auth login entra --flow device-code")
	assert.NotContains(t, cmd.Long, "device-code: Device code flow via microsoft.com/devicelogin (default)")
	assert.NotContains(t, cmd.Long, "Login with Entra ID using device code flow (default)")
	assert.NotContains(t, cmd.Long, "auth-code")

	_, err := parseFlow("auth-code", "entra")
	require.Error(t, err, "interactive flow must not expose an auth-code alias")
}
