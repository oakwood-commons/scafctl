// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/auth"
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
	results, warnings := queryHandlerStatuses(ctx, cliParams, []string{"zulu", "alpha", "mike"})
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

	results, warnings := queryHandlerStatuses(ctx, cliParams, []string{"missing"})
	assert.Empty(t, results)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "Failed to initialize missing")
}
