// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandStatus_UnknownHandler(t *testing.T) {
	ctx, _ := newTestContext(t)
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
