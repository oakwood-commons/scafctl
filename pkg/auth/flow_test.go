// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFlow_Empty(t *testing.T) {
	flow, err := ParseFlow("", "entra")
	require.NoError(t, err)
	assert.Equal(t, Flow(""), flow)
}

func TestParseFlow_KnownFlows(t *testing.T) {
	tests := []struct {
		input    string
		expected Flow
	}{
		{"device_code", FlowDeviceCode},
		{"device-code", FlowDeviceCode},
		{"devicecode", FlowDeviceCode},
		{"interactive", FlowInteractive},
		{"service_principal", FlowServicePrincipal},
		{"service-principal", FlowServicePrincipal},
		{"serviceprincipal", FlowServicePrincipal},
		{"sp", FlowServicePrincipal},
		{"workload_identity", FlowWorkloadIdentity},
		{"workload-identity", FlowWorkloadIdentity},
		{"workloadidentity", FlowWorkloadIdentity},
		{"wi", FlowWorkloadIdentity},
		{"pat", FlowPAT},
		{"metadata", FlowMetadata},
		{"gcloud_adc", FlowGcloudADC},
		{"gcloud-adc", FlowGcloudADC},
		{"gcloudadc", FlowGcloudADC},
		{"adc", FlowGcloudADC},
		{"github_app", FlowGitHubApp},
		{"github-app", FlowGitHubApp},
		{"githubapp", FlowGitHubApp},
		{"app", FlowGitHubApp},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			flow, err := ParseFlow(tt.input, "")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, flow)
		})
	}
}

func TestParseFlow_CaseInsensitive(t *testing.T) {
	flow, err := ParseFlow("Device-Code", "entra")
	require.NoError(t, err)
	assert.Equal(t, FlowDeviceCode, flow)

	flow, err = ParseFlow("INTERACTIVE", "gcp")
	require.NoError(t, err)
	assert.Equal(t, FlowInteractive, flow)

	flow, err = ParseFlow("PAT", "github")
	require.NoError(t, err)
	assert.Equal(t, FlowPAT, flow)
}

func TestParseFlow_UnknownFlow_HandlerSpecificErrors(t *testing.T) {
	tests := []struct {
		handler  string
		contains string
	}{
		{"github", "valid for github"},
		{"gcp", "valid for gcp"},
		{"entra", "valid for entra"},
		{"unknown", "unknown flow: bogus"},
	}

	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			_, err := ParseFlow("bogus", tt.handler)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.contains)
		})
	}
}

func TestParseFlow_UnknownFlow_DefaultHandler(t *testing.T) {
	_, err := ParseFlow("bogus", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flow: bogus")
}
