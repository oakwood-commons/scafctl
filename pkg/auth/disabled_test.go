// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisabledHandler_GetToken(t *testing.T) {
	original := NewMockHandler("entra")
	d := newDisabledHandler(original, "not configured for server mode")

	_, err := d.GetToken(context.Background(), TokenOptions{Scope: "scope"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerDisabled))
	assert.Contains(t, err.Error(), "entra")
	assert.Contains(t, err.Error(), "not configured for server mode")
}

func TestDisabledHandler_InjectAuth(t *testing.T) {
	original := NewMockHandler("github")
	d := newDisabledHandler(original, "reason")

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://api.github.com", nil)
	err := d.InjectAuth(context.Background(), req, TokenOptions{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerDisabled))
}

func TestDisabledHandler_Login(t *testing.T) {
	original := NewMockHandler("entra")
	d := newDisabledHandler(original, "reason")

	_, err := d.Login(context.Background(), LoginOptions{})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerDisabled))
}

func TestDisabledHandler_Logout(t *testing.T) {
	original := NewMockHandler("entra")
	d := newDisabledHandler(original, "reason")

	err := d.Logout(context.Background())

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerDisabled))
}

func TestDisabledHandler_Status(t *testing.T) {
	original := NewMockHandler("entra")
	d := newDisabledHandler(original, "reason")

	status, err := d.Status(context.Background())

	require.NoError(t, err)
	assert.False(t, status.Authenticated)
}

func TestDisabledHandler_Identity(t *testing.T) {
	original := NewMockHandler("entra")
	original.DisplayNameValue = "Microsoft Entra"
	d := newDisabledHandler(original, "reason")

	assert.Equal(t, "entra", d.Name())
	assert.Equal(t, "Microsoft Entra (disabled)", d.DisplayName())
}

func TestDisabledHandler_PreservesMetadata(t *testing.T) {
	original := NewMockHandler("entra")
	original.FlowsValue = []Flow{FlowDeviceCode, FlowClientCredentials}
	original.CapabilitiesValue = []Capability{CapScopesOnTokenRequest}
	d := newDisabledHandler(original, "reason")

	assert.Equal(t, []Flow{FlowDeviceCode, FlowClientCredentials}, d.SupportedFlows())
	assert.Equal(t, []Capability{CapScopesOnTokenRequest}, d.Capabilities())
}

func TestIsDisabled(t *testing.T) {
	original := NewMockHandler("entra")
	d := newDisabledHandler(original, "reason")

	assert.True(t, IsDisabled(d))
	assert.False(t, IsDisabled(original))
}

func TestRegistry_Disable(t *testing.T) {
	reg := NewRegistry()
	handler := NewMockHandler("entra")
	require.NoError(t, reg.Register(handler))

	err := reg.Disable("entra", "not configured for server mode")

	require.NoError(t, err)
	assert.True(t, reg.Has("entra"))

	h, err := reg.Get("entra")
	require.NoError(t, err)
	assert.True(t, IsDisabled(h))

	_, tokenErr := h.GetToken(context.Background(), TokenOptions{})
	assert.True(t, errors.Is(tokenErr, ErrHandlerDisabled))
}

func TestRegistry_Disable_NotFound(t *testing.T) {
	reg := NewRegistry()

	err := reg.Disable("missing", "reason")

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrHandlerNotFound))
}
