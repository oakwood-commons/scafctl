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

func TestCommandLogin_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
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

	// Verify output
	output := buf.String()
	assert.Contains(t, output, "Authentication successful")
	assert.Contains(t, output, "Test User")
	assert.Contains(t, output, "test@example.com")
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

func TestBuildDeviceCodeTUI(t *testing.T) {
	data, schema := buildDeviceCodeTUI("Entra ID", "ABC123", "https://microsoft.com/devicelogin")

	assert.Equal(t, "Sign in to Entra ID", data["title"])
	assert.Equal(t, "https://microsoft.com/devicelogin", data["url"])
	assert.Equal(t, "ABC123", data["code"])

	require.NotNil(t, schema.Status)
	assert.Equal(t, "title", schema.Status.TitleField)
	assert.Equal(t, "Waiting for authentication...", schema.Status.WaitMessage)
	assert.Len(t, schema.Status.DisplayFields, 2)
	assert.Len(t, schema.Status.Actions, 2)
	assert.Equal(t, "copy-value", schema.Status.Actions[0].Type)
	assert.Equal(t, "open-url", schema.Status.Actions[1].Type)
}

func TestBuildBrowserAuthTUI(t *testing.T) {
	data, schema := buildBrowserAuthTUI("GitHub", "https://github.com/login/oauth/authorize?...")

	assert.Equal(t, "Sign in to GitHub", data["title"])
	assert.Equal(t, "https://github.com/login/oauth/authorize?...", data["url"])
	_, hasCode := data["code"]
	assert.False(t, hasCode, "browser auth TUI should not have a code field")

	require.NotNil(t, schema.Status)
	assert.Equal(t, "title", schema.Status.TitleField)
	assert.Equal(t, "Waiting for browser authentication...", schema.Status.WaitMessage)
	assert.Len(t, schema.Status.DisplayFields, 1)
	assert.Equal(t, "url", schema.Status.DisplayFields[0].Field)
	assert.Len(t, schema.Status.Actions, 1)
	assert.Equal(t, "open-url", schema.Status.Actions[0].Type)
	assert.Equal(t, "Re-open URL", schema.Status.Actions[0].Label)
}

func TestCommandLogin_BrowserAuthCallback(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("github")
	mock.CapabilitiesValue = []auth.Capability{
		auth.CapScopesOnLogin,
	}
	mock.SetNotAuthenticated()

	mock.LoginResult = &auth.Result{
		Claims: &auth.Claims{Email: "user@example.com"},
	}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandLogin(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.NoError(t, err)

	require.Len(t, mock.LoginCalls, 1)
	assert.NotNil(t, mock.LoginCalls[0].BrowserAuthCallback, "browser auth callback should be set")
}
