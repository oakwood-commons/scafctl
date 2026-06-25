// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execcredential

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWithAPIVersion_Expiry(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 6, 24, 15, 4, 5, 0, time.UTC)
	tests := []struct {
		name          string
		token         string
		expiresAt     time.Time
		wantExpiry    string
		wantHasExpiry bool
	}{
		{
			name:          "with expiry",
			token:         "abc123",
			expiresAt:     expiry,
			wantExpiry:    "2026-06-24T15:04:05Z",
			wantHasExpiry: true,
		},
		{
			name:          "zero expiry omits timestamp",
			token:         "abc123",
			expiresAt:     time.Time{},
			wantHasExpiry: false,
		},
		{
			name:          "non-UTC expiry normalized to UTC",
			token:         "abc123",
			expiresAt:     time.Date(2026, 6, 24, 11, 4, 5, 0, time.FixedZone("EST", -4*3600)),
			wantExpiry:    "2026-06-24T15:04:05Z",
			wantHasExpiry: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ec := NewWithAPIVersion(DefaultAPIVersion, tt.token, tt.expiresAt)
			assert.Equal(t, DefaultAPIVersion, ec.APIVersion)
			assert.Equal(t, Kind, ec.Kind)
			assert.Equal(t, tt.token, ec.Status.Token)
			if tt.wantHasExpiry {
				assert.Equal(t, tt.wantExpiry, ec.Status.ExpirationTimestamp)
			} else {
				assert.Empty(t, ec.Status.ExpirationTimestamp)
			}
		})
	}
}

func TestNewWithAPIVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiVersion string
		want       string
	}{
		{name: "explicit v1beta1", apiVersion: "client.authentication.k8s.io/v1beta1", want: "client.authentication.k8s.io/v1beta1"},
		{name: "empty falls back to default", apiVersion: "", want: DefaultAPIVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ec := NewWithAPIVersion(tt.apiVersion, "tok", time.Time{})
			assert.Equal(t, tt.want, ec.APIVersion)
		})
	}
}

func TestJSON(t *testing.T) {
	t.Parallel()

	ec := NewWithAPIVersion(DefaultAPIVersion, "eyJtoken", time.Date(2026, 6, 24, 15, 4, 5, 0, time.UTC))
	data, err := ec.JSON()
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, DefaultAPIVersion, decoded["apiVersion"])
	assert.Equal(t, Kind, decoded["kind"])

	status, ok := decoded["status"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "eyJtoken", status["token"])
	assert.Equal(t, "2026-06-24T15:04:05Z", status["expirationTimestamp"])
}

func TestJSON_ZeroExpiryOmitsField(t *testing.T) {
	t.Parallel()

	ec := NewWithAPIVersion(DefaultAPIVersion, "tok", time.Time{})
	data, err := ec.JSON()
	require.NoError(t, err)
	assert.NotContains(t, string(data), "expirationTimestamp")
}

func TestAPIVersionFromExecInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		execInfo string
		want     string
	}{
		{name: "empty", execInfo: "", want: DefaultAPIVersion},
		{name: "invalid json", execInfo: "{not json", want: DefaultAPIVersion},
		{name: "v1", execInfo: `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential"}`, want: "client.authentication.k8s.io/v1"},
		{name: "v1beta1", execInfo: `{"apiVersion":"client.authentication.k8s.io/v1beta1"}`, want: "client.authentication.k8s.io/v1beta1"},
		{name: "unrelated group rejected", execInfo: `{"apiVersion":"v1"}`, want: DefaultAPIVersion},
		{name: "missing apiVersion", execInfo: `{"kind":"ExecCredential"}`, want: DefaultAPIVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, APIVersionFromExecInfo(tt.execInfo))
		})
	}
}
