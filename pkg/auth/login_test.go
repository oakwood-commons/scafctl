// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFlow_ExplicitFlow(t *testing.T) {
	result := DetectFlow(FlowDeviceCode, nil, FlowInteractive)
	assert.Equal(t, FlowDeviceCode, result.Flow)
	assert.Empty(t, result.Description)
}

func TestDetectFlow_DetectorMatch(t *testing.T) {
	detectors := []CredentialDetector{
		{
			HasCredentials: func() bool { return false },
			Flow:           FlowWorkloadIdentity,
			Description:    "workload identity",
		},
		{
			HasCredentials: func() bool { return true },
			Flow:           FlowServicePrincipal,
			Description:    "Detected service principal credentials",
		},
	}

	result := DetectFlow("", detectors, FlowInteractive)
	assert.Equal(t, FlowServicePrincipal, result.Flow)
	assert.Equal(t, "Detected service principal credentials", result.Description)
}

func TestDetectFlow_FirstDetectorWins(t *testing.T) {
	detectors := []CredentialDetector{
		{
			HasCredentials: func() bool { return true },
			Flow:           FlowWorkloadIdentity,
			Description:    "workload identity",
		},
		{
			HasCredentials: func() bool { return true },
			Flow:           FlowServicePrincipal,
			Description:    "service principal",
		},
	}

	result := DetectFlow("", detectors, FlowInteractive)
	assert.Equal(t, FlowWorkloadIdentity, result.Flow)
}

func TestDetectFlow_DefaultFallback(t *testing.T) {
	detectors := []CredentialDetector{
		{
			HasCredentials: func() bool { return false },
			Flow:           FlowPAT,
			Description:    "pat",
		},
	}

	result := DetectFlow("", detectors, FlowInteractive)
	assert.Equal(t, FlowInteractive, result.Flow)
	assert.Empty(t, result.Description)
}

func TestDetectFlow_NoDetectors(t *testing.T) {
	result := DetectFlow("", nil, FlowInteractive)
	assert.Equal(t, FlowInteractive, result.Flow)
}

func TestPreLoginCheck_Force(t *testing.T) {
	handler := NewMockHandler("test")
	handler.StatusResult = &Status{
		Authenticated: true,
		Claims:        &Claims{Name: "Test User"},
	}

	result, err := PreLoginCheck(context.Background(), handler, FlowInteractive, true, false)
	require.NoError(t, err)
	assert.Equal(t, PreLoginProceed, result.Action)
	assert.Equal(t, 1, handler.LogoutCalls) // force should trigger logout
}

func TestPreLoginCheck_SkipCheckFlow(t *testing.T) {
	handler := NewMockHandler("test")

	result, err := PreLoginCheck(context.Background(), handler, FlowPAT, false, false, FlowPAT)
	require.NoError(t, err)
	assert.Equal(t, PreLoginProceed, result.Action)
	assert.Equal(t, 0, handler.StatusCalls) // status should not be checked
}

func TestPreLoginCheck_NotAuthenticated(t *testing.T) {
	handler := NewMockHandler("test")
	handler.StatusResult = &Status{Authenticated: false}

	result, err := PreLoginCheck(context.Background(), handler, FlowInteractive, false, false)
	require.NoError(t, err)
	assert.Equal(t, PreLoginProceed, result.Action)
}

func TestPreLoginCheck_AlreadyAuthenticated_Skip(t *testing.T) {
	handler := NewMockHandler("test")
	handler.StatusResult = &Status{
		Authenticated: true,
		Claims:        &Claims{Name: "Test User", Email: "test@example.com"},
	}

	result, err := PreLoginCheck(context.Background(), handler, FlowInteractive, false, true)
	require.NoError(t, err)
	assert.Equal(t, PreLoginSkip, result.Action)
	assert.NotEmpty(t, result.Identity)
}

func TestPreLoginCheck_AlreadyAuthenticated_NoSkip(t *testing.T) {
	handler := NewMockHandler("test")
	handler.StatusResult = &Status{
		Authenticated: true,
		Claims:        &Claims{Name: "Test User"},
	}

	result, err := PreLoginCheck(context.Background(), handler, FlowInteractive, false, false)
	require.NoError(t, err)
	assert.Equal(t, PreLoginAlreadyAuthenticated, result.Action)
	assert.NotEmpty(t, result.Identity)
}

func TestPreLoginCheck_StatusError(t *testing.T) {
	handler := NewMockHandler("test")
	handler.StatusErr = assert.AnError

	_, err := PreLoginCheck(context.Background(), handler, FlowInteractive, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check auth status")
}

func BenchmarkDetectFlow(b *testing.B) {
	detectors := []CredentialDetector{
		{HasCredentials: func() bool { return false }, Flow: FlowWorkloadIdentity},
		{HasCredentials: func() bool { return true }, Flow: FlowServicePrincipal},
	}

	for b.Loop() {
		DetectFlow("", detectors, FlowInteractive)
	}
}
