// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/kvx/pkg/tui"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCommandStatus_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
	ctx = config.WithConfig(ctx, &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
	})
	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"unknown"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
}

func TestCommandStatus_AllHandlers(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	// With test handler injection, listHandlers returns only the mock's name
	assert.Equal(t, 1, mock.StatusCalls)
}

func TestCommandStatus_SpecificHandler(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{
		Email:    "test@example.com",
		Name:     "Test User",
		TenantID: "test-tenant",
	})
	mock.StatusResult.TenantID = "test-tenant"
	mock.StatusResult.ExpiresAt = time.Now().Add(24 * time.Hour)
	mock.StatusResult.LastRefresh = time.Now()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify status was checked
	assert.Equal(t, 1, mock.StatusCalls)
}

func TestCommandStatus_NotAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCommandStatus_StatusError(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.StatusErr = errors.New("failed to check status")

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra"})

	err := cmd.Execute()
	// Error is logged as warning, returns "no auth handlers found"
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handlers found")
}

func TestCommandStatus_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{
		Email: "test@example.com",
		Name:  "Test User",
	})
	mock.StatusResult.TenantID = "test-tenant"

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test@example.com")
	assert.Contains(t, output, "Test User")
	assert.Contains(t, output, "authenticated")
}

func TestQueryHandlerStatuses_DeterministicOrder(t *testing.T) {
	ctx, _ := newTestContext(t)

	// Register multiple handlers.
	registry := auth.NewRegistry()
	for _, name := range []string{"zulu", "alpha", "mike"} {
		mock := auth.NewMockHandler(name)
		mock.StatusResult = &auth.Status{Authenticated: false}
		require.NoError(t, registry.Register(mock))
	}
	ctx = auth.WithRegistry(ctx, registry)
	cliParams := settings.NewCliParams()

	// Request in a specific order; results should preserve that order.
	results, warnings := queryHandlerStatuses(ctx, cliParams, []string{"zulu", "alpha", "mike"}, false)
	assert.Empty(t, warnings)
	require.Len(t, results, 3)

	assert.Equal(t, "zulu", results[0]["handler"])
	assert.Equal(t, "alpha", results[1]["handler"])
	assert.Equal(t, "mike", results[2]["handler"])
}

func TestQueryHandlerStatuses_WarningOnFailure(t *testing.T) {
	ctx, _ := newTestContext(t)

	// No registry means handler lookup will fail.
	cliParams := settings.NewCliParams()

	results, warnings := queryHandlerStatuses(ctx, cliParams, []string{"missing"}, false)
	assert.Empty(t, results)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "Failed to initialize missing")
}

func TestBuildStatusResult_Authenticated(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{Flow: auth.FlowDeviceCode},
	}

	now := time.Now()
	status := &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Email:    "test@example.com",
			Name:     "Test User",
			Username: "testuser",
			TenantID: "test-tenant",
		},
		TenantID:     "test-tenant",
		IdentityType: auth.IdentityTypeUser,
		ClientID:     "client-123",
		ExpiresAt:    now.Add(2 * time.Hour),
		LastRefresh:  now.Add(-10 * time.Minute),
		Scopes:       []string{"openid", "profile"},
	}

	cliParams := settings.NewCliParams()
	result := buildStatusResult(ctx, cliParams, "entra", mock, status)

	assert.Equal(t, "authenticated", result["status"])
	assert.Equal(t, true, result["authenticated"])
	assert.Equal(t, "test@example.com", result["email"])
	assert.Equal(t, "Test User", result["name"])
	assert.Equal(t, "testuser", result["username"])
	assert.Equal(t, "test@example.com", result["user"])
	assert.Equal(t, "test-tenant", result["tenantId"])
	assert.Equal(t, string(auth.IdentityTypeUser), result["identityType"])
	assert.Equal(t, "client-123", result["clientId"])
	assert.NotEmpty(t, result["expiresAt"])
	assert.NotEmpty(t, result["lastRefresh"])
	assert.NotEmpty(t, result["expiresIn"])
	assert.Equal(t, []string{"openid", "profile"}, result["scopes"])
	assert.Equal(t, 1, result["cachedTokens"])
	// Flow should come from cached token fallback since mock doesn't implement FlowReporter
	assert.Equal(t, string(auth.FlowDeviceCode), result["flow"])
}

func TestBuildStatusResult_NotAuthenticated(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("github")
	mock.DisplayNameValue = "GitHub"

	status := &auth.Status{
		Authenticated: false,
		Reason:        "token expired",
	}

	cliParams := &settings.Run{BinaryName: "mycli"}
	result := buildStatusResult(ctx, cliParams, "github", mock, status)

	assert.Equal(t, "token expired", result["status"])
	assert.Equal(t, false, result["authenticated"])
	assert.Contains(t, result["hint"], "mycli auth login github")
}

func TestBuildStatusResult_WorkloadIdentity(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"

	status := &auth.Status{
		Authenticated: true,
		Flow:          auth.FlowWorkloadIdentity,
		IdentityType:  auth.IdentityTypeWorkloadIdentity,
		TokenFile:     "/var/run/secrets/token",
		ClientID:      "wif-client",
		Claims: &auth.Claims{
			Email: "wif@example.com",
		},
		ExpiresAt: time.Now().Add(time.Hour),
	}

	cliParams := settings.NewCliParams()
	result := buildStatusResult(ctx, cliParams, "entra", mock, status)

	assert.Equal(t, string(auth.FlowWorkloadIdentity), result["flow"])
	assert.Equal(t, "/var/run/secrets/token", result["tokenFile"])
	assert.Equal(t, string(auth.IdentityTypeWorkloadIdentity), result["identityType"])
}

func TestBuildStatusResult_UsernameOnlyUser(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("github")

	status := &auth.Status{
		Authenticated: true,
		Flow:          "pat",
		Claims: &auth.Claims{
			Username: "octocat",
		},
	}

	cliParams := settings.NewCliParams()
	result := buildStatusResult(ctx, cliParams, "github", mock, status)

	assert.Equal(t, "octocat", result["user"])
	assert.Equal(t, "octocat", result["username"])
	assert.Empty(t, result["email"])
}

func TestBuildStatusResult_NameOnlyUser(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("gcp")

	status := &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Name: "Service Account",
		},
	}

	cliParams := settings.NewCliParams()
	result := buildStatusResult(ctx, cliParams, "gcp", mock, status)

	assert.Equal(t, "Service Account", result["user"])
	assert.Equal(t, "Service Account", result["name"])
}

func TestBuildStatusResult_InvalidExpiryTime(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("gcp")

	// Unix epoch zero - should be filtered out
	status := &auth.Status{
		Authenticated: true,
		Claims: &auth.Claims{
			Email: "test@example.com",
		},
		ExpiresAt:   time.Unix(0, 0),
		LastRefresh: time.Unix(0, 0),
	}

	cliParams := settings.NewCliParams()
	result := buildStatusResult(ctx, cliParams, "gcp", mock, status)

	assert.Equal(t, "", result["expiresAt"])
	assert.Equal(t, "", result["lastRefresh"])
	assert.Equal(t, "", result["expiresIn"])
}

func TestBuildStatusResult_ExpiresAtTimeAlwaysPresent(t *testing.T) {
	ctx, _ := newTestContext(t)

	t.Run("nil when expiry is invalid", func(t *testing.T) {
		mock := auth.NewMockHandler("entra")
		status := &auth.Status{
			Authenticated: true,
			Claims:        &auth.Claims{Email: "a@b.com"},
			ExpiresAt:     time.Time{}, // zero time - invalid
		}

		result := buildStatusResult(ctx, settings.NewCliParams(), "entra", mock, status)

		_, exists := result["_expiresAtTime"]
		assert.True(t, exists, "_expiresAtTime must always be present to keep maps homogeneous")
		assert.Nil(t, result["_expiresAtTime"])
	})

	t.Run("time.Time when expiry is valid", func(t *testing.T) {
		mock := auth.NewMockHandler("entra")
		future := time.Now().Add(2 * time.Hour)
		status := &auth.Status{
			Authenticated: true,
			Claims:        &auth.Claims{Email: "a@b.com"},
			ExpiresAt:     future,
		}

		result := buildStatusResult(ctx, settings.NewCliParams(), "entra", mock, status)

		_, exists := result["_expiresAtTime"]
		assert.True(t, exists, "_expiresAtTime must always be present to keep maps homogeneous")
		assert.Equal(t, future, result["_expiresAtTime"])
	})
}

func TestExpandProfileStatuses(t *testing.T) {
	ctx, _ := newTestContext(t)

	// Register a handler with profiles configured.
	registry := auth.NewRegistry()
	mock := auth.NewMockHandler("github")
	mock.StatusResult = &auth.Status{Authenticated: false}
	require.NoError(t, registry.Register(mock))
	ctx = auth.WithRegistry(ctx, registry)

	cliParams := settings.NewCliParams()

	// Without profiles configured, should query once per handler.
	results, warnings := expandProfileStatuses(ctx, cliParams, []string{"github"})
	assert.Empty(t, warnings)
	assert.Len(t, results, 1)
}

func TestIsValidExpiryTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{name: "zero time", t: time.Time{}, want: false},
		{name: "unix epoch zero", t: time.Unix(0, 0), want: false},
		{name: "year 1999", t: time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC), want: false},
		{name: "year 2000", t: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), want: true},
		{name: "current time", t: time.Now(), want: true},
		{name: "future time", t: time.Now().Add(24 * time.Hour), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isValidExpiryTime(tt.t))
		})
	}
}

func TestCommandStatus_ExitCode_Unauthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetNotAuthenticated()

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--exit-code"})

	err := cmd.Execute()
	require.Error(t, err)

	var exitErr *exitcode.ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, exitcode.GeneralError, exitErr.Code)
	assert.Contains(t, err.Error(), "not authenticated")
}

func TestCommandStatus_ExitCode_AllAuthenticated(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{Email: "test@example.com"})
	mock.StatusResult.ExpiresAt = time.Now().Add(24 * time.Hour)

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--exit-code"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCommandStatus_WarnWithin_Expiring(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{Email: "test@example.com"})
	// Token expires in 5 minutes, but --warn-within is 1 hour.
	mock.StatusResult.ExpiresAt = time.Now().Add(5 * time.Minute)

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "--warn-within", "1h"})

	err := cmd.Execute()
	require.Error(t, err)

	var exitErr *exitcode.ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, exitcode.GeneralError, exitErr.Code)
	assert.Contains(t, err.Error(), "expire within")
}

func TestCommandStatus_JSONOutput_NoInternalFields(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{Email: "test@example.com"})
	mock.StatusResult.ExpiresAt = time.Now().Add(2 * time.Hour)

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandStatus(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"entra", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.NotContains(t, output, "_expiresAtTime")
	assert.Contains(t, output, "authenticated")
}

func TestAuthHandlersTable_RendersBorderedAt100Columns(t *testing.T) {
	ctx, _ := newTestContext(t)
	ctx = auth.WithProfile(ctx, "test")

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.SetAuthenticated(&auth.Claims{Email: "user@example.com"})
	mock.StatusResult.Flow = auth.FlowInteractive
	mock.StatusResult.ExpiresAt = time.Now().Add(90 * 24 * time.Hour)

	row := buildStatusResult(ctx, settings.NewCliParams(), "entra", mock, mock.StatusResult)
	for k := range row {
		if strings.HasPrefix(k, "_") {
			delete(row, k)
		}
	}
	rows := []map[string]any{row}

	hints := authHandlerColumnHints(hasClusterScopedRows(rows))
	assert.True(t, hints["kind"].Hidden, "redundant kind column must be hidden under Auth Handlers")
	assert.Equal(t, authHandlerProfileMaxWidth, hints["profile"].MaxWidth)
	assert.Equal(t, authHandlerFlowMaxWidth, hints["flow"].MaxWidth)
	assert.True(t, hints["user"].Flex, "user column must be Flex")

	for _, width := range []int{90, 100, 120} {
		var out bytes.Buffer
		err := kvx.View(rows,
			kvx.WithIO(nil, &out),
			kvx.WithNoColor(true),
			kvx.WithDimensions(width, 24),
			kvx.WithColumnOrder([]string{"handler", "profile", "status", "user", "flow", "expiresIn"}),
			kvx.WithColumnarMode(tui.ColumnarModeAlways),
			kvx.WithSchemaJSON(authStatusSchema),
			kvx.WithColumnHints(hints),
		)
		require.NoError(t, err, "width=%d", width)

		rendered := out.String()
		assert.Contains(t, rendered, "╭", "width=%d must render bordered kvx table", width)
		assert.Contains(t, rendered, "Handler")
		assert.Contains(t, rendered, "Profile")
		assert.Contains(t, rendered, "Status")
		assert.Contains(t, rendered, "User")
		assert.Contains(t, rendered, "Flow")
		assert.Contains(t, rendered, "Expires")
		assert.Contains(t, rendered, "user@example.com")
		assert.NotContains(t, rendered, "Kind", "hidden Kind column must not render in Auth Handlers table")
		assert.NotContains(t, rendered, "expiresIn:", "table must not collapse into vertical key/value cards")
	}
}

func TestAuthHandlersTable_PreservesWiderProfileForClusterScopedRows(t *testing.T) {
	ctx, _ := newTestContext(t)
	now := time.Now()

	mock := auth.NewMockHandler("openshift")
	mock.CapabilitiesValue = []auth.Capability{auth.CapInstanceHostname}
	mock.SetAuthenticated(&auth.Claims{Email: "user@example.com"})
	mock.ListCachedTokensResult = []*auth.CachedTokenInfo{
		{
			Handler:   "openshift",
			Hostname:  "https://api.cluster-a.example.com:6443",
			Flow:      auth.FlowInteractive,
			ExpiresAt: now.Add(2 * time.Hour),
			CachedAt:  now,
		},
	}

	base := buildStatusResult(ctx, settings.NewCliParams(), "openshift", mock, mock.StatusResult)
	base["profile"] = "work"
	rows := expandInstanceRows(ctx, "openshift", mock, mock.StatusResult, base)
	require.True(t, hasClusterScopedRows(rows))

	for _, r := range rows {
		for k := range r {
			if strings.HasPrefix(k, "_") {
				delete(r, k)
			}
		}
	}

	hints := authHandlerColumnHints(true)
	assert.Equal(t, authHandlerClusterProfileMaxWidth, hints["profile"].MaxWidth, "cluster-scoped rows preserve wider profile cap")

	// Also verify "<profile> / <cluster>" string detection without internal marker.
	assert.True(t, hasClusterScopedRows(rows))

	var out bytes.Buffer
	err := kvx.View(rows,
		kvx.WithIO(nil, &out),
		kvx.WithNoColor(true),
		kvx.WithDimensions(100, 24),
		kvx.WithColumnOrder([]string{"handler", "profile", "status", "user", "flow", "expiresIn"}),
		kvx.WithColumnarMode(tui.ColumnarModeAlways),
		kvx.WithSchemaJSON(authStatusSchema),
		kvx.WithColumnHints(hints),
	)
	require.NoError(t, err)

	rendered := out.String()
	assert.Contains(t, rendered, "╭", "cluster-scoped rows at 100 columns must still render as bordered table")
	assert.Contains(t, rendered, "work / api.cluster-a.", "cluster-scoped profile column must preserve >20-char cluster label at 100 columns")
	assert.NotContains(t, rendered, "expiresIn:")

	var wideOut bytes.Buffer
	require.NoError(t, kvx.View(rows,
		kvx.WithIO(nil, &wideOut),
		kvx.WithNoColor(true),
		kvx.WithDimensions(120, 24),
		kvx.WithColumnOrder([]string{"handler", "profile", "status", "user", "flow", "expiresIn"}),
		kvx.WithColumnarMode(tui.ColumnarModeAlways),
		kvx.WithSchemaJSON(authStatusSchema),
		kvx.WithColumnHints(hints),
	))
	assert.Contains(t, wideOut.String(), "work / api.cluster-a.example.com (active)")
}

func isolateAuthStatusXDG(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, "cfg"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmpDir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmpDir, "cache"))
}

func TestCommandStatus_MixedAuthenticatedAndUnauthenticatedHandlers(t *testing.T) {
	newMixedCtx := func(t *testing.T) (context.Context, *bytes.Buffer) {
		t.Helper()
		isolateAuthStatusXDG(t)
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, &config.Config{
			Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
		})

		entra := auth.NewMockHandler("entra")
		entra.DisplayNameValue = "Microsoft Entra ID"
		entra.SetAuthenticated(&auth.Claims{Email: "user@example.com"})
		entra.StatusResult.Flow = auth.FlowInteractive
		entra.StatusResult.ExpiresAt = time.Now().Add(2 * time.Hour)

		gcp := auth.NewMockHandler("gcp")
		gcp.DisplayNameValue = "Google Cloud"
		gcp.SetAuthenticated(&auth.Claims{Email: "adc@example.com"})
		gcp.StatusResult.Flow = auth.FlowGcloudADC

		gh := auth.NewMockHandler("github")
		gh.DisplayNameValue = "GitHub"
		gh.SetNotAuthenticated()

		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(entra))
		require.NoError(t, reg.Register(gcp))
		require.NoError(t, reg.Register(gh))
		return auth.WithRegistry(ctx, reg), buf
	}

	t.Run("table includes unauthenticated handler alongside authenticated handlers", func(t *testing.T) {
		ctx, buf := newMixedCtx(t)
		cliParams := &settings.Run{BinaryName: "mycli", NoColor: true}
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

		cmd := CommandStatus(cliParams, ioStreams, "mycli/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{})

		require.NoError(t, cmd.Execute())
		out := buf.String()
		assert.Contains(t, out, "Auth Handlers")
		assert.Contains(t, out, "entra")
		assert.Contains(t, out, "gcp")
		assert.Contains(t, out, "github")
		assert.Contains(t, out, "not authenticated")
	})

	t.Run("json includes unauthenticated handler with embedder login hint", func(t *testing.T) {
		ctx, buf := newMixedCtx(t)
		cliParams := &settings.Run{BinaryName: "mycli", NoColor: true}
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

		cmd := CommandStatus(cliParams, ioStreams, "mycli/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"-o", "json"})

		require.NoError(t, cmd.Execute())

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
		require.Len(t, rows, 3)

		assert.Equal(t, "entra", rows[0]["handler"])
		assert.Equal(t, true, rows[0]["authenticated"])
		assert.Equal(t, "authenticated", rows[0]["status"])

		assert.Equal(t, "gcp", rows[1]["handler"])
		assert.Equal(t, true, rows[1]["authenticated"])
		assert.Equal(t, "authenticated", rows[1]["status"])

		assert.Equal(t, "github", rows[2]["handler"])
		assert.Equal(t, false, rows[2]["authenticated"])
		assert.Equal(t, "not authenticated", rows[2]["status"])
		assert.Equal(t, "built-in (active)", rows[2]["profile"])
		assert.Equal(t, "run 'mycli auth login github' to authenticate", rows[2]["hint"])
	})

	t.Run("yaml includes unauthenticated handler consistently with table and json", func(t *testing.T) {
		ctx, buf := newMixedCtx(t)
		cliParams := &settings.Run{BinaryName: "mycli", NoColor: true}
		ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

		cmd := CommandStatus(cliParams, ioStreams, "mycli/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"-o", "yaml"})

		require.NoError(t, cmd.Execute())

		var rows []map[string]any
		require.NoError(t, yaml.Unmarshal(buf.Bytes(), &rows))
		require.Len(t, rows, 3)

		assert.Equal(t, "entra", rows[0]["handler"])
		assert.Equal(t, true, rows[0]["authenticated"])
		assert.Equal(t, "gcp", rows[1]["handler"])
		assert.Equal(t, true, rows[1]["authenticated"])
		assert.Equal(t, "github", rows[2]["handler"])
		assert.Equal(t, false, rows[2]["authenticated"])
		assert.Equal(t, "not authenticated", rows[2]["status"])
		assert.Equal(t, "run 'mycli auth login github' to authenticate", rows[2]["hint"])
	})
}

func TestCommandStatus_MultiProfileFiltering(t *testing.T) {
	appCfg := &config.Config{
		Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
		Auth: config.GlobalAuthConfig{
			Entra: &config.EntraAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-entra",
				Profiles: map[string]*config.EntraProfileConfig{
					"staging": {ClientID: "staging-entra"},
					"work":    {ClientID: "work-entra"},
				},
			},
			GitHub: &config.GitHubAuthConfig{
				ActiveProfile: "work",
				ClientID:      "default-gh",
				Profiles: map[string]*config.GitHubProfileConfig{
					"personal": {ClientID: "personal-gh"},
					"work":     {ClientID: "work-gh"},
				},
			},
		},
	}

	newMultiProfileCtx := func(t *testing.T) (context.Context, *bytes.Buffer) {
		t.Helper()
		isolateAuthStatusXDG(t)
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, appCfg)

		entraWrapper, entraFake := newFakePluginHandler("entra", nil, []auth.Flow{auth.FlowInteractive})
		// Only the "work" profile is authenticated on entra; built-in and staging are unauthenticated.
		entraFake.authenticated["work"] = true

		ghWrapper, _ := newFakePluginHandler("github", nil, []auth.Flow{auth.FlowDeviceCode})
		// Zero profiles are authenticated on github; activeProfile is "work".

		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(entraWrapper))
		require.NoError(t, reg.Register(ghWrapper))
		return auth.WithRegistry(ctx, reg), buf
	}

	t.Run("default view omits secondary unauthenticated profiles but keeps active unauthenticated handler row", func(t *testing.T) {
		ctx, buf := newMultiProfileCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"-o", "json"})

		require.NoError(t, cmd.Execute())

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
		require.Len(t, rows, 2)

		assert.Equal(t, "entra", rows[0]["handler"])
		assert.Equal(t, "work (active)", rows[0]["profile"])
		assert.Equal(t, true, rows[0]["authenticated"])

		assert.Equal(t, "github", rows[1]["handler"])
		assert.Equal(t, "work (active)", rows[1]["profile"])
		assert.Equal(t, false, rows[1]["authenticated"])
		assert.Equal(t, "not authenticated", rows[1]["status"])
	})

	t.Run("--all includes all configured profiles across handlers", func(t *testing.T) {
		ctx, buf := newMultiProfileCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"--all", "-o", "json"})

		require.NoError(t, cmd.Execute())

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
		require.Len(t, rows, 6)

		assert.Equal(t, "entra", rows[0]["handler"])
		assert.Equal(t, "built-in", rows[0]["profile"])
		assert.Equal(t, "entra", rows[1]["handler"])
		assert.Equal(t, "staging", rows[1]["profile"])
		assert.Equal(t, "entra", rows[2]["handler"])
		assert.Equal(t, "work (active)", rows[2]["profile"])

		assert.Equal(t, "github", rows[3]["handler"])
		assert.Equal(t, "built-in", rows[3]["profile"])
		assert.Equal(t, "github", rows[4]["handler"])
		assert.Equal(t, "personal", rows[4]["profile"])
		assert.Equal(t, "github", rows[5]["handler"])
		assert.Equal(t, "work (active)", rows[5]["profile"])
	})

	t.Run("named handler argument shows all profiles for that handler without --all", func(t *testing.T) {
		ctx, buf := newMultiProfileCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github", "-o", "json"})

		require.NoError(t, cmd.Execute())

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &rows))
		require.Len(t, rows, 3)
		assert.Equal(t, "built-in", rows[0]["profile"])
		assert.Equal(t, "personal", rows[1]["profile"])
		assert.Equal(t, "work (active)", rows[2]["profile"])
	})
}

func TestFilterDefaultHandlerRows(t *testing.T) {
	t.Parallel()

	t.Run("empty input returns empty slice", func(t *testing.T) {
		t.Parallel()
		got := filterDefaultHandlerRows(context.Background(), nil)
		assert.Empty(t, got)
	})

	t.Run("preserves handler order when unauthenticated handler precedes authenticated handler", func(t *testing.T) {
		t.Parallel()
		rows := []map[string]any{
			{"handler": "alpha", "profile": "built-in (active)", "authenticated": false},
			{"handler": "zulu", "profile": "built-in", "authenticated": false},
			{"handler": "zulu", "profile": "prod (active)", "authenticated": true},
			{"handler": "zulu", "profile": "staging", "authenticated": true},
		}

		got := filterDefaultHandlerRows(context.Background(), rows)
		require.Len(t, got, 3)
		assert.Equal(t, "alpha", got[0]["handler"])
		assert.Equal(t, false, got[0]["authenticated"])
		assert.Equal(t, "zulu", got[1]["handler"])
		assert.Equal(t, "prod (active)", got[1]["profile"])
		assert.Equal(t, "zulu", got[2]["handler"])
		assert.Equal(t, "staging", got[2]["profile"])
	})

	t.Run("falls back to ConfigActiveProfile when active suffix is absent", func(t *testing.T) {
		t.Parallel()
		ctx := config.WithConfig(context.Background(), &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{ActiveProfile: "work"},
			},
		})
		rows := []map[string]any{
			{"handler": "github", "profile": "built-in", "authenticated": false},
			{"handler": "github", "profile": "work", "authenticated": false},
		}

		got := filterDefaultHandlerRows(ctx, rows)
		require.Len(t, got, 1)
		assert.Equal(t, "work", got[0]["profile"])
	})

	t.Run("falls back to first row when neither active suffix nor ConfigActiveProfile matches", func(t *testing.T) {
		t.Parallel()
		ctx := config.WithConfig(context.Background(), &config.Config{
			Auth: config.GlobalAuthConfig{
				GitHub: &config.GitHubAuthConfig{ActiveProfile: "nonexistent"},
			},
		})
		rows := []map[string]any{
			{"handler": "github", "profile": "personal", "authenticated": false},
			{"handler": "github", "profile": "secondary", "authenticated": false},
		}

		got := filterDefaultHandlerRows(ctx, rows)
		require.Len(t, got, 1)
		assert.Equal(t, "personal", got[0]["profile"])
	})
}

func TestCommandStatus_ExitCode_MixedHandlers(t *testing.T) {
	newMixedExitCtx := func(t *testing.T) (context.Context, *bytes.Buffer) {
		t.Helper()
		isolateAuthStatusXDG(t)
		ctx, buf := newTestContext(t)
		ctx = config.WithConfig(ctx, &config.Config{
			Settings: config.Settings{DisableThirdPartyAuthHandlers: true},
		})

		entra := auth.NewMockHandler("entra")
		entra.DisplayNameValue = "Microsoft Entra ID"
		entra.SetAuthenticated(&auth.Claims{Email: "user@example.com"})
		entra.StatusResult.ExpiresAt = time.Now().Add(time.Hour)

		gh := auth.NewMockHandler("github")
		gh.DisplayNameValue = "GitHub"
		gh.SetNotAuthenticated()

		reg := auth.NewRegistry()
		require.NoError(t, reg.Register(entra))
		require.NoError(t, reg.Register(gh))
		return auth.WithRegistry(ctx, reg), buf
	}

	t.Run("unfiltered --exit-code fails when any registered handler is unauthenticated", func(t *testing.T) {
		ctx, buf := newMixedExitCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"--exit-code"})

		err := cmd.Execute()
		require.Error(t, err)
		var exitErr *exitcode.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, exitcode.GeneralError, exitErr.Code)
	})

	t.Run("named authenticated handler --exit-code succeeds despite unauthenticated sibling handler", func(t *testing.T) {
		ctx, buf := newMixedExitCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"entra", "--exit-code"})

		require.NoError(t, cmd.Execute())
	})

	t.Run("named unauthenticated handler --exit-code fails", func(t *testing.T) {
		ctx, buf := newMixedExitCtx(t)
		cmd := CommandStatus(settings.NewCliParams(), terminal.NewIOStreams(nil, buf, buf, false), "scafctl/auth")
		cmd.SetContext(ctx)
		cmd.SetArgs([]string{"github", "--exit-code"})

		err := cmd.Execute()
		require.Error(t, err)
		var exitErr *exitcode.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, exitcode.GeneralError, exitErr.Code)
	})
}
