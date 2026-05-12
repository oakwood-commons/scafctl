// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDetectAvailableFlowsAsDetectors_NoFlowDetector(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockHandler("test")

	// MockHandler does not implement FlowDetector.
	detectors := detectAvailableFlowsAsDetectors(ctx, mock, nil)
	assert.Nil(t, detectors)
}

func TestDetectAvailableFlowsAsDetectors_WithFlows(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockFlowDetectorHandler("test")
	mock.DetectFlowsResult = []auth.FlowAvailability{
		{Flow: "device-code", Available: true, Reason: "always available"},
		{Flow: "pat", Available: false, Reason: "no token found"},
		{Flow: "interactive", Available: true, Reason: "browser available"},
	}

	detectors := detectAvailableFlowsAsDetectors(ctx, mock, nil)
	require.Len(t, detectors, 2, "only available flows should be returned")

	assert.Equal(t, auth.Flow("device-code"), detectors[0].Flow)
	assert.Equal(t, "always available", detectors[0].Description)
	assert.True(t, detectors[0].HasCredentials())

	assert.Equal(t, auth.Flow("interactive"), detectors[1].Flow)
	assert.Equal(t, "browser available", detectors[1].Description)
	assert.True(t, detectors[1].HasCredentials())
}

func TestDetectAvailableFlowsAsDetectors_SkipFlows(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockFlowDetectorHandler("test")
	mock.DetectFlowsResult = []auth.FlowAvailability{
		{Flow: "device-code", Available: true, Reason: "available"},
		{Flow: "pat", Available: true, Reason: "token found"},
	}

	detectors := detectAvailableFlowsAsDetectors(ctx, mock, []auth.Flow{"pat"})
	require.Len(t, detectors, 1)
	assert.Equal(t, auth.Flow("device-code"), detectors[0].Flow)
}

func TestDetectAvailableFlowsAsDetectors_GRPCUnimplemented(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockFlowDetectorHandler("test")
	mock.DetectFlowsErr = status.Error(codes.Unimplemented, "not implemented")

	// Should gracefully return nil for older plugins.
	detectors := detectAvailableFlowsAsDetectors(ctx, mock, nil)
	assert.Nil(t, detectors)
}

func TestDetectAvailableFlowsAsDetectors_OtherError(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockFlowDetectorHandler("test")
	mock.DetectFlowsErr = fmt.Errorf("network error")

	// Non-gRPC errors also fall back gracefully.
	detectors := detectAvailableFlowsAsDetectors(ctx, mock, nil)
	assert.Nil(t, detectors)
}

func TestDetectAvailableFlowsAsDetectors_AllUnavailable(t *testing.T) {
	ctx, _ := newTestContext(t)
	mock := auth.NewMockFlowDetectorHandler("test")
	mock.DetectFlowsResult = []auth.FlowAvailability{
		{Flow: "device-code", Available: false, Reason: "no network"},
		{Flow: "pat", Available: false, Reason: "no token"},
	}

	detectors := detectAvailableFlowsAsDetectors(ctx, mock, nil)
	assert.Empty(t, detectors)
}
