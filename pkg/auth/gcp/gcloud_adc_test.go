// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGcloudADCPath_CustomDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	path := getGcloudADCPath()
	assert.Equal(t, filepath.Join(tmpDir, "application_default_credentials.json"), path)
}

func TestGetGcloudADCPath_Default(t *testing.T) {
	t.Setenv(EnvCloudSDKConfig, "")
	path := getGcloudADCPath()
	// Should return a non-empty path on any platform
	assert.NotEmpty(t, path)
	assert.Contains(t, path, "application_default_credentials.json")
}

func TestLoadGcloudADCCredentials_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	creds, err := LoadGcloudADCCredentials()
	require.NoError(t, err)
	assert.Nil(t, creds) // No file, no error
}

func TestLoadGcloudADCCredentials_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	path := filepath.Join(tmpDir, "application_default_credentials.json")
	err := os.WriteFile(path, []byte("not-json"), 0o600)
	require.NoError(t, err)

	creds, err := LoadGcloudADCCredentials()
	require.Error(t, err)
	assert.Nil(t, creds)
}

func TestLoadGcloudADCCredentials_WrongType(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	path := filepath.Join(tmpDir, "application_default_credentials.json")
	data, _ := json.Marshal(map[string]string{
		"type":          "service_account",
		"client_id":     "test",
		"client_secret": "test",
	})
	err := os.WriteFile(path, data, 0o600)
	require.NoError(t, err)

	creds, err := LoadGcloudADCCredentials()
	require.NoError(t, err)
	assert.Nil(t, creds) // not authorized_user type
}

func TestLoadGcloudADCCredentials_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	path := filepath.Join(tmpDir, "application_default_credentials.json")
	adcCreds := GcloudADCCredentials{
		ClientID:     "test-client-id.apps.googleusercontent.com",
		ClientSecret: "test-client-secret",
		RefreshToken: "test-refresh-token",
		Type:         "authorized_user",
	}
	data, err := json.Marshal(adcCreds)
	require.NoError(t, err)
	err = os.WriteFile(path, data, 0o600)
	require.NoError(t, err)

	creds, err := LoadGcloudADCCredentials()
	require.NoError(t, err)
	require.NotNil(t, creds)
	assert.Equal(t, "authorized_user", creds.Type)
	assert.Equal(t, "test-client-id.apps.googleusercontent.com", creds.ClientID)
	assert.Equal(t, "test-refresh-token", creds.RefreshToken)
}

func TestHasGcloudADCCredentials_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	assert.False(t, HasGcloudADCCredentials())
}

func TestHasGcloudADCCredentials_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv(EnvCloudSDKConfig, tmpDir)

	path := filepath.Join(tmpDir, "application_default_credentials.json")
	data, _ := json.Marshal(GcloudADCCredentials{
		ClientID:     "test",
		ClientSecret: "test",
		RefreshToken: "test",
		Type:         "authorized_user",
	})
	_ = os.WriteFile(path, data, 0o600)

	assert.True(t, HasGcloudADCCredentials())
}

func TestFormatGcloudTokenError_InvalidRapt(t *testing.T) {
	err := formatGcloudTokenError(TokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "reauth related error (invalid_rapt)",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-authentication")
	assert.Contains(t, err.Error(), "invalid_rapt")
	assert.Contains(t, err.Error(), "scafctl auth login gcp")
	assert.NotContains(t, err.Error(), "gcloud auth application-default login")
	assert.NotContains(t, err.Error(), "your organization")
}

func TestFormatGcloudTokenError_InvalidGrantOther(t *testing.T) {
	err := formatGcloudTokenError(TokenErrorResponse{
		Error:            "invalid_grant",
		ErrorDescription: "Token has been expired or revoked",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired or been revoked")
	assert.Contains(t, err.Error(), "Token has been expired or revoked")
	assert.Contains(t, err.Error(), "scafctl auth login gcp")
	assert.NotContains(t, err.Error(), "gcloud auth application-default login")
}

func TestFormatGcloudTokenError_OtherError(t *testing.T) {
	err := formatGcloudTokenError(TokenErrorResponse{
		Error:            "access_denied",
		ErrorDescription: "the client is not authorized",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access_denied")
	assert.Contains(t, err.Error(), "the client is not authorized")
}
