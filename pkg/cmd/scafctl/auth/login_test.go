// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/auth/oauth2"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
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

type mockLazyProfileConfigurerHandler struct {
	*auth.MockConfigurerHandler
	capabilitiesFn   func() []auth.Capability
	applyOverridesFn func(ctx context.Context, overrides map[string]string) error
	loginFn          func(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error)
}

func (m *mockLazyProfileConfigurerHandler) Capabilities() []auth.Capability {
	if m.capabilitiesFn != nil {
		return m.capabilitiesFn()
	}
	return m.MockConfigurerHandler.Capabilities()
}

func (m *mockLazyProfileConfigurerHandler) ApplyOverrides(ctx context.Context, overrides map[string]string) error {
	if err := m.MockConfigurerHandler.ApplyOverrides(ctx, overrides); err != nil {
		return err
	}
	if m.applyOverridesFn != nil {
		return m.applyOverridesFn(ctx, overrides)
	}
	return nil
}

func (m *mockLazyProfileConfigurerHandler) Login(ctx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	if m.loginFn != nil {
		return m.loginFn(ctx, opts)
	}
	return m.MockConfigurerHandler.Login(ctx, opts)
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

	t.Run("ResolveEntraConfig normalizes hyphenated device-code for FlowDetector and forwards global --auth-profile override", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", tmpDir)
		t.Setenv("XDG_STATE_HOME", tmpDir)

		// 1. Top-level hyphenated DefaultFlow: "device-code" configured via ResolveEntraConfig.
		topLevelCfg := &config.EntraAuthConfig{DefaultFlow: "device-code"}
		resolvedTop := auth.ResolveEntraConfig(topLevelCfg, "")
		require.NotNil(t, resolvedTop)
		assert.Equal(t, string(auth.FlowDeviceCode), resolvedTop.DefaultFlow)

		ctxTop, bufTop := newTestContext(t)
		ctxTop = config.WithConfig(ctxTop, &config.Config{
			Auth: config.GlobalAuthConfig{Entra: topLevelCfg},
		})
		detectorTop := auth.NewMockFlowDetectorHandler("entra")
		detectorTop.DisplayNameValue = "Microsoft Entra ID"
		detectorTop.SetNotAuthenticated()
		detectorTop.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}
		// Mirror scafctl-plugin-auth-entra preferredFlow(), which compares strictly
		// against canonical auth.FlowDeviceCode ("device_code") and defaults to FlowInteractive.
		if auth.Flow(resolvedTop.DefaultFlow) == auth.FlowDeviceCode {
			detectorTop.DetectFlowsResult = []auth.FlowAvailability{
				{Flow: auth.FlowDeviceCode, Available: true},
				{Flow: auth.FlowInteractive, Available: true},
			}
		} else {
			detectorTop.DetectFlowsResult = []auth.FlowAvailability{
				{Flow: auth.FlowInteractive, Available: true},
				{Flow: auth.FlowDeviceCode, Available: true},
			}
		}
		ctxTop = withTestHandler(ctxTop, detectorTop)

		cmdTop := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, bufTop, bufTop, false), "scafctl/auth")
		cmdTop.SetContext(ctxTop)
		cmdTop.SetArgs([]string{"entra"})

		require.NoError(t, cmdTop.Execute())
		require.Len(t, detectorTop.LoginCalls, 1)
		assert.Equal(t, auth.FlowDeviceCode, detectorTop.LoginCalls[0].Flow)

		// 2. Global --auth-profile (WithGlobalProfile) forwards normalized override to
		// preloaded Configurer before DetectAvailableFlows runs.
		ctxGlobal, bufGlobal := newTestContext(t)
		ctxGlobal = config.WithConfig(ctxGlobal, &config.Config{
			Auth: config.GlobalAuthConfig{
				Entra: &config.EntraAuthConfig{
					DefaultFlow: "interactive",
					Profiles: map[string]*config.EntraProfileConfig{
						"headless": {DefaultFlow: "device-code"},
					},
				},
			},
		})
		ctxGlobal = auth.WithGlobalProfile(ctxGlobal, "headless")

		base := auth.NewMockConfigurerHandler("entra")
		base.DisplayNameValue = "Microsoft Entra ID"
		base.SetNotAuthenticated()
		base.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}

		// Preloaded plugin starts with top-level "interactive" prior to --auth-profile.
		pluginDefaultFlow := "interactive"
		handler := &mockDetectorConfigurerHandler{
			MockConfigurerHandler: base,
			detectFn: func(_ context.Context) ([]auth.FlowAvailability, error) {
				if len(base.ApplyOverridesCalls) > 0 && base.ApplyOverridesCalls[0]["defaultFlow"] != "" {
					pluginDefaultFlow = base.ApplyOverridesCalls[0]["defaultFlow"]
				}
				if auth.Flow(pluginDefaultFlow) == auth.FlowDeviceCode {
					return []auth.FlowAvailability{
						{Flow: auth.FlowDeviceCode, Available: true},
						{Flow: auth.FlowInteractive, Available: true},
					}, nil
				}
				return []auth.FlowAvailability{
					{Flow: auth.FlowInteractive, Available: true},
					{Flow: auth.FlowDeviceCode, Available: true},
				}, nil
			},
		}
		ctxGlobal = withTestHandler(ctxGlobal, handler)

		cmdGlobal := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, bufGlobal, bufGlobal, false), "scafctl/auth")
		cmdGlobal.SetContext(ctxGlobal)
		cmdGlobal.SetArgs([]string{"entra"})

		require.NoError(t, cmdGlobal.Execute())
		require.Len(t, base.ApplyOverridesCalls, 1)
		assert.Equal(t, "device_code", base.ApplyOverridesCalls[0]["defaultFlow"])
		require.Len(t, base.LoginCalls, 1)
		assert.Equal(t, auth.FlowDeviceCode, base.LoginCalls[0].Flow)
	})

	t.Run("explicit --profile default with explicit --flow applies top-level credentials over activeProfile while preserving flow", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", tmpDir)
		t.Setenv("XDG_STATE_HOME", tmpDir)

		appCfg := &config.Config{
			Auth: config.GlobalAuthConfig{
				Entra: &config.EntraAuthConfig{
					ClientID:      "top-level-client-id",
					TenantID:      "top-level-tenant-id",
					DefaultFlow:   "interactive",
					ActiveProfile: "work",
					Profiles: map[string]*config.EntraProfileConfig{
						"work": {
							ClientID:    "work-profile-client-id",
							TenantID:    "work-profile-tenant-id",
							DefaultFlow: "interactive",
						},
					},
				},
			},
		}

		baseCtx, buf := newTestContext(t)
		baseCtx = config.WithConfig(baseCtx, appCfg)

		base := auth.NewMockConfigurerHandler("entra")
		base.DisplayNameValue = "Microsoft Entra ID"
		base.CapabilitiesValue = []auth.Capability{auth.CapTenantID, auth.CapScopesOnLogin}
		base.SetNotAuthenticated()
		base.LoginResult = &auth.Result{Claims: &auth.Claims{Email: "user@example.com"}}

		var (
			initialized    bool
			initialCfg     *config.EntraAuthConfig
			effectiveCfg   *config.EntraAuthConfig
			loginSnapCfg   *config.EntraAuthConfig
			overrideProf   string
			overrideProfOK bool
		)

		handler := &mockLazyProfileConfigurerHandler{
			MockConfigurerHandler: base,
			capabilitiesFn: func() []auth.Capability {
				if !initialized {
					initialized = true
					// Mirror LazyAuthHandlerWrapper.Capabilities() initializing the
					// cached plugin from baseCtx before CLI --profile takes effect.
					prof := auth.ResolveActiveProfile(baseCtx, "entra")
					resolved := auth.ResolveEntraConfig(appCfg.Auth.Entra, prof)
					cp := *resolved
					initialCfg = &cp
					effectiveCfg = resolved
				}
				return base.CapabilitiesValue
			},
			applyOverridesFn: func(callCtx context.Context, overrides map[string]string) error {
				// Mirror AuthHandlerWrapper.ApplyOverrides re-injecting the
				// selected profile's resolved Entra config before overlaying flags.
				overrideProf = auth.ResolveActiveProfile(callCtx, "entra")
				overrideProfOK = auth.IsProfileResolved(callCtx)
				resolved := auth.ResolveEntraConfig(config.FromContext(callCtx).Auth.Entra, overrideProf)
				if v := overrides["clientId"]; v != "" {
					resolved.ClientID = v
				}
				if v := overrides["tenantId"]; v != "" {
					resolved.TenantID = v
				}
				if v := overrides["defaultFlow"]; v != "" {
					resolved.DefaultFlow = v
				}
				effectiveCfg = resolved
				return nil
			},
			loginFn: func(loginCtx context.Context, opts auth.LoginOptions) (*auth.Result, error) {
				if effectiveCfg != nil {
					cp := *effectiveCfg
					loginSnapCfg = &cp
				}
				return base.Login(loginCtx, opts)
			},
		}
		ctx := withTestHandler(baseCtx, handler)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--profile", "default", "--flow", "device-code"})

		require.NoError(t, cmd.Execute())

		require.NotNil(t, initialCfg)
		assert.Equal(t, "work-profile-client-id", initialCfg.ClientID)
		assert.Equal(t, "work-profile-tenant-id", initialCfg.TenantID)

		require.Len(t, base.ApplyOverridesCalls, 1)
		assert.True(t, overrideProfOK)
		assert.Empty(t, overrideProf)
		assert.Equal(t, "interactive", base.ApplyOverridesCalls[0]["defaultFlow"])

		require.NotNil(t, loginSnapCfg)
		assert.Equal(t, "top-level-client-id", loginSnapCfg.ClientID)
		assert.Equal(t, "top-level-tenant-id", loginSnapCfg.TenantID)
		assert.Empty(t, base.LastContextProfile)

		require.Len(t, base.LoginCalls, 1)
		assert.Equal(t, auth.FlowDeviceCode, base.LoginCalls[0].Flow)
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

func TestCommandLogin_HybridHandlerPreservesExplicitInteractiveAndDeviceCodeDefaults(t *testing.T) {
	t.Setenv("TEST_HYBRID_ROBOT_TOKEN", "robot-secret-token")
	t.Setenv("TEST_HYBRID_WIF_JWT", "oidc-jwt-token")

	t.Run("defaultFlow device_code runs device_code instead of ambient PAT or WIF", func(t *testing.T) {
		var deviceCodeHits, tokenHits int
		srv := newHybridLoginTestServer(t, &deviceCodeHits, &tokenHits)
		defer srv.Close()

		store := newTestMemorySecretStore()
		h, err := newCustomOAuth2HandlerForLoginTest(config.CustomOAuth2Config{
			Name:                   "registry-auth",
			DisplayName:            "Container Registry",
			DeviceAuthURL:          srv.URL + "/device",
			TokenURL:               srv.URL + "/token",
			ClientID:               "registry-cli",
			DefaultFlow:            "device_code",
			DeviceCodePollInterval: 1,
			Token:                  "env://TEST_HYBRID_ROBOT_TOKEN",
			Username:               "acme+ci_robot",
			FederatedToken:         "env://TEST_HYBRID_WIF_JWT",
		}, store, srv.Client())
		require.NoError(t, err)

		// Ambient GetToken before login still falls back to headless credentials.
		ambientTok, err := h.GetToken(context.Background(), auth.TokenOptions{})
		require.NoError(t, err)
		assert.Equal(t, auth.FlowWorkloadIdentity, ambientTok.Flow)

		// Running 'auth login registry-auth' without --flow or --force must use
		// device_code rather than silently selecting PAT/WIF or stopping on ambient status.
		ctx, buf := newTestContext(t)
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(h))
		ctx = auth.WithRegistry(ctx, reg)

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"registry-auth"})

		require.NoError(t, cmd.Execute())
		assert.Equal(t, 1, deviceCodeHits, "CLI login must execute configured device_code flow")
		assert.Equal(t, auth.FlowDeviceCode, h.ActiveFlow(context.Background()))
		assert.NotContains(t, buf.String(), "Already authenticated")
	})

	t.Run("defaultFlow interactive selects interactive flow even when PAT is resolvable", func(t *testing.T) {
		store := newTestMemorySecretStore()
		rec := &recordingCustomFlowHandler{
			Handler: mustNewHybridInteractiveHandler(t, store),
		}

		ctx, buf := newTestContext(t)
		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(rec))
		ctx = auth.WithRegistry(ctx, reg)

		cliParams := settings.NewCliParams()
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)
		cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"registry-auth"})

		require.NoError(t, cmd.Execute())
		require.Len(t, rec.loginFlows, 1)
		assert.Equal(t, auth.FlowInteractive, rec.loginFlows[0], "CLI login must pass FlowInteractive instead of FlowPAT")
		assert.NotContains(t, buf.String(), "Already authenticated")
	})
}

func TestCommandLogin_GCPAmbientGcloudADC(t *testing.T) {
	newGCPDetector := func(status *auth.Status) *auth.MockFlowDetectorHandler {
		h := auth.NewMockFlowDetectorHandler("gcp")
		h.DisplayNameValue = "Google Cloud Platform"
		h.FlowsValue = []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode, auth.FlowGcloudADC}
		h.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin}
		h.StatusResult = status
		h.DetectFlowsResult = []auth.FlowAvailability{
			{Flow: auth.FlowInteractive, Available: true, Reason: "workstation browser flow"},
			{Flow: auth.FlowGcloudADC, Available: true, Reason: "application_default_credentials.json present"},
		}
		h.LoginResult = &auth.Result{
			Claims:    &auth.Claims{Email: "user@example.com"},
			ExpiresAt: time.Now().Add(time.Hour),
		}
		return h
	}

	t.Run("ambient gcloud_adc proceeds with interactive login without --force", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		mock := newGCPDetector(&auth.Status{
			Authenticated: true,
			Flow:          auth.FlowGcloudADC,
			Claims:        &auth.Claims{Name: "gcloud ADC (application default credentials)"},
		})
		ctx = withTestHandler(ctx, mock)

		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cmd := CommandLogin(cliParams, terminal.NewIOStreams(nil, buf, buf, false), "mycli/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"gcp"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, auth.FlowInteractive, mock.LoginCalls[0].Flow)
		assert.Contains(t, buf.String(), "Logged in to Google Cloud Platform as user@example.com")
		assert.NotContains(t, buf.String(), "Already authenticated")
	})

	t.Run("ambient gcloud_adc with --skip-if-authenticated skips cleanly", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		mock := newGCPDetector(&auth.Status{
			Authenticated: true,
			Flow:          auth.FlowGcloudADC,
			Claims:        &auth.Claims{Name: "gcloud ADC (application default credentials)"},
		})
		ctx = withTestHandler(ctx, mock)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"gcp", "--skip-if-authenticated"})

		require.NoError(t, cmd.Execute())
		assert.Empty(t, mock.LoginCalls)
		assert.Contains(t, buf.String(), "Already authenticated as gcloud ADC (application default credentials)")
		assert.Contains(t, buf.String(), "skipping login")
	})

	t.Run("explicit --flow gcloud-adc runs without --force and honors --skip-if-authenticated", func(t *testing.T) {
		ctx, buf := newTestContext(t)
		mock := newGCPDetector(&auth.Status{
			Authenticated: true,
			Flow:          auth.FlowInteractive,
			Claims:        &auth.Claims{Email: "existing@example.com"},
		})
		ctx = withTestHandler(ctx, mock)

		cmd := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"gcp", "--flow", "gcloud-adc"})

		require.NoError(t, cmd.Execute())
		require.Len(t, mock.LoginCalls, 1)
		assert.Equal(t, auth.FlowGcloudADC, mock.LoginCalls[0].Flow)
		assert.Equal(t, 0, mock.StatusCalls, "explicit --flow gcloud-adc without --skip-if-authenticated should skip Status()")
		assert.NotContains(t, buf.String(), "Already authenticated")

		// With --skip-if-authenticated, already-authenticated handler skips cleanly.
		ctxSkip, bufSkip := newTestContext(t)
		mockSkip := newGCPDetector(&auth.Status{
			Authenticated: true,
			Flow:          auth.FlowGcloudADC,
			Claims:        &auth.Claims{Name: "gcloud ADC (application default credentials)"},
		})
		ctxSkip = withTestHandler(ctxSkip, mockSkip)

		cmdSkip := CommandLogin(settings.NewCliParams(), terminal.NewIOStreams(nil, bufSkip, bufSkip, false), "scafctl/auth")
		cmdSkip.SetContext(ctxSkip)
		cmdSkip.SetArgs([]string{"gcp", "--flow", "gcloud-adc", "--skip-if-authenticated"})

		require.NoError(t, cmdSkip.Execute())
		assert.Empty(t, mockSkip.LoginCalls)
		assert.Equal(t, 1, mockSkip.StatusCalls)
		assert.Contains(t, bufSkip.String(), "skipping login")
	})

	t.Run("stored interactive and device_code sessions still warn with embedder binary name", func(t *testing.T) {
		for _, storedFlow := range []auth.Flow{auth.FlowInteractive, auth.FlowDeviceCode} {
			ctx, buf := newTestContext(t)
			mock := newGCPDetector(&auth.Status{
				Authenticated: true,
				Flow:          storedFlow,
				Claims:        &auth.Claims{Email: "stored@example.com"},
			})
			ctx = withTestHandler(ctx, mock)

			cliParams := settings.NewCliParams()
			cliParams.BinaryName = "mycli"
			cmd := CommandLogin(cliParams, terminal.NewIOStreams(nil, buf, buf, false), "mycli/auth")
			cmd.SetContext(ctx)
			cmd.SetArgs([]string{"gcp"})

			require.NoError(t, cmd.Execute())
			assert.Empty(t, mock.LoginCalls)
			assert.Contains(t, buf.String(), "Already authenticated as stored@example.com.")
			assert.Contains(t, buf.String(), "Use 'mycli auth logout gcp' to sign out first, or use --force to re-authenticate.")
		}
	})
}

type recordingCustomFlowHandler struct {
	*oauth2.Handler
	loginFlows []auth.Flow
}

func (r *recordingCustomFlowHandler) Login(_ context.Context, opts auth.LoginOptions) (*auth.Result, error) {
	r.loginFlows = append(r.loginFlows, opts.Flow)
	return &auth.Result{
		Claims:    &auth.Claims{Username: "workstation-user"},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func newTestMemorySecretStore() secrets.Store {
	return secrets.NewMockStore()
}

func newCustomOAuth2HandlerForLoginTest(cfg config.CustomOAuth2Config, store secrets.Store, httpClient *http.Client) (*oauth2.Handler, error) {
	return oauth2.New(cfg, oauth2.WithSecretStore(store), oauth2.WithHTTPClient(httpClient), oauth2.WithLogger(logr.Discard()))
}

func mustNewHybridInteractiveHandler(t *testing.T, store secrets.Store) *oauth2.Handler {
	t.Helper()
	h, err := oauth2.New(config.CustomOAuth2Config{
		Name:             "registry-auth",
		DisplayName:      "Container Registry",
		AuthorizeURL:     "https://registry.example.com/oauth/authorize",
		TokenURL:         "https://registry.example.com/oauth/token",
		ClientID:         "registry-cli",
		DefaultFlow:      "interactive",
		CallbackPort:     8400,
		Registry:         "registry.example.com",
		RegistryUsername: "$oauthtoken",
		Token:            "env://TEST_HYBRID_ROBOT_TOKEN",
		Username:         "acme+ci_robot",
	}, oauth2.WithSecretStore(store), oauth2.WithLogger(logr.Discard()))
	require.NoError(t, err)
	return h
}

func newHybridLoginTestServer(t *testing.T, deviceCodeHits, tokenHits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/device":
			*deviceCodeHits++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-code-1",
				"user_code":        "ABCD-EFGH",
				"verification_uri": "https://registry.example.com/activate",
				"expires_in":       300,
				"interval":         1,
			})
		case "/token":
			*tokenHits++
			_ = r.ParseForm()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-" + r.FormValue("grant_type"),
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
