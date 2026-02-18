// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandList_Success(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("entra")
	mock.DisplayNameValue = "Microsoft Entra ID"
	mock.FlowsValue = []auth.Flow{auth.FlowDeviceCode, auth.FlowServicePrincipal}
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapTenantID}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "entra")
	assert.Contains(t, output, "Microsoft Entra ID")
	assert.Contains(t, output, "device_code")
	assert.Contains(t, output, "service_principal")
	assert.Contains(t, output, "scopes_on_login")
	assert.Contains(t, output, "tenant_id")
}

func TestCommandList_NoHandlers(t *testing.T) {
	ctx, _ := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth handlers registered")
}

func TestCommandList_JSONOutput(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("github")
	mock.DisplayNameValue = "GitHub"
	mock.FlowsValue = []auth.Flow{auth.FlowDeviceCode, auth.FlowPAT}
	mock.CapabilitiesValue = []auth.Capability{auth.CapScopesOnLogin, auth.CapHostname}

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"name"`)
	assert.Contains(t, output, `"github"`)
	assert.Contains(t, output, `"displayName"`)
	assert.Contains(t, output, `"flows"`)
	assert.Contains(t, output, `"capabilities"`)
}

func TestCommandList_NoArgs(t *testing.T) {
	ctx, _ := newTestContext(t)

	mock := auth.NewMockHandler("test")
	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, nil, nil, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"extra-arg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandList_EmptyCapabilities(t *testing.T) {
	ctx, buf := newTestContext(t)

	mock := auth.NewMockHandler("custom")
	mock.DisplayNameValue = "Custom Handler"
	mock.FlowsValue = []auth.Flow{auth.FlowDeviceCode}
	mock.CapabilitiesValue = nil

	ctx = withTestHandler(ctx, mock)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := CommandList(cliParams, ioStreams, "scafctl/auth")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "custom")
	assert.Contains(t, output, "Custom Handler")
}

func TestFlowsToStrings(t *testing.T) {
	flows := []auth.Flow{auth.FlowDeviceCode, auth.FlowServicePrincipal, auth.FlowPAT}
	result := flowsToStrings(flows)

	assert.Equal(t, []string{"device_code", "service_principal", "pat"}, result)
}

func TestFlowsToStrings_Empty(t *testing.T) {
	result := flowsToStrings(nil)
	assert.Len(t, result, 0)
}

func TestCapabilitiesToStrings(t *testing.T) {
	caps := []auth.Capability{auth.CapScopesOnLogin, auth.CapTenantID, auth.CapHostname}
	result := capabilitiesToStrings(caps)

	assert.Equal(t, []string{"scopes_on_login", "tenant_id", "hostname"}, result)
}

func TestCapabilitiesToStrings_Empty(t *testing.T) {
	result := capabilitiesToStrings(nil)
	assert.Len(t, result, 0)
}
