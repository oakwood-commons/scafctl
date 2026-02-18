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

func TestGetServiceAccountKey_NoEnv(t *testing.T) {
	t.Setenv(EnvGoogleApplicationCredentials, "")

	key, err := GetServiceAccountKey()
	require.NoError(t, err)
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_FileNotFound(t *testing.T) {
	t.Setenv(EnvGoogleApplicationCredentials, "/nonexistent/path/key.json")

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_InvalidJSON(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	err := os.WriteFile(tmpFile, []byte("not-valid-json"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.Error(t, err)
	assert.Nil(t, key)
}

func TestGetServiceAccountKey_WrongType(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	keyData := map[string]string{
		"type":         "authorized_user",
		"client_email": "user@example.com",
	}
	data, err := json.Marshal(keyData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.NoError(t, err)
	assert.Nil(t, key) // not a service_account type
}

func TestGetServiceAccountKey_Valid(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "key.json")
	keyData := ServiceAccountKey{
		Type:         "service_account",
		ProjectID:    "my-project",
		PrivateKeyID: "key-id",
		ClientEmail:  "sa@my-project.iam.gserviceaccount.com",
		ClientID:     "12345",
		TokenURI:     "https://oauth2.googleapis.com/token",
		PrivateKey:   "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n",
	}
	data, err := json.Marshal(keyData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleApplicationCredentials, tmpFile)

	key, err := GetServiceAccountKey()
	require.NoError(t, err)
	require.NotNil(t, key)
	assert.Equal(t, "service_account", key.Type)
	assert.Equal(t, "my-project", key.ProjectID)
	assert.Equal(t, "sa@my-project.iam.gserviceaccount.com", key.ClientEmail)
}

func TestHasServiceAccountCredentials(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		t.Setenv(EnvGoogleApplicationCredentials, "")
		assert.False(t, HasServiceAccountCredentials())
	})

	t.Run("valid service account", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "key.json")
		keyData := ServiceAccountKey{
			Type:        "service_account",
			ClientEmail: "sa@project.iam.gserviceaccount.com",
			PrivateKey:  "fake-key",
		}
		data, _ := json.Marshal(keyData)
		_ = os.WriteFile(tmpFile, data, 0o600)

		t.Setenv(EnvGoogleApplicationCredentials, tmpFile)
		assert.True(t, HasServiceAccountCredentials())
	})
}
