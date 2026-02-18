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

func TestGetExternalAccountConfig_NoEnv(t *testing.T) {
	t.Setenv(EnvGoogleExternalAccount, "")

	cfg, err := GetExternalAccountConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestGetExternalAccountConfig_FileNotFound(t *testing.T) {
	t.Setenv(EnvGoogleExternalAccount, "/nonexistent/path/config.json")

	cfg, err := GetExternalAccountConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestGetExternalAccountConfig_InvalidJSON(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "config.json")
	err := os.WriteFile(tmpFile, []byte("not-valid-json"), 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleExternalAccount, tmpFile)

	cfg, err := GetExternalAccountConfig()
	require.Error(t, err)
	assert.Nil(t, cfg)
}

func TestGetExternalAccountConfig_WrongType(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "config.json")
	configData := map[string]string{
		"type": "service_account",
	}
	data, err := json.Marshal(configData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleExternalAccount, tmpFile)

	cfg, err := GetExternalAccountConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg) // not an external_account type
}

func TestGetExternalAccountConfig_Valid(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "config.json")
	configData := ExternalAccountConfig{
		Type:             "external_account",
		Audience:         "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/pool/providers/provider",
		SubjectTokenType: "urn:ietf:params:oauth:token-type:jwt",
		TokenURL:         "https://sts.googleapis.com/v1/token",
		CredentialSource: CredentialSource{
			File: "/var/run/secrets/token",
		},
	}
	data, err := json.Marshal(configData)
	require.NoError(t, err)
	err = os.WriteFile(tmpFile, data, 0o600)
	require.NoError(t, err)

	t.Setenv(EnvGoogleExternalAccount, tmpFile)

	cfg, err := GetExternalAccountConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "external_account", cfg.Type)
	assert.Contains(t, cfg.Audience, "workloadIdentityPools")
	assert.Equal(t, "/var/run/secrets/token", cfg.CredentialSource.File)
}

func TestHasWorkloadIdentityCredentials(t *testing.T) {
	t.Run("no credentials", func(t *testing.T) {
		t.Setenv(EnvGoogleExternalAccount, "")
		assert.False(t, HasWorkloadIdentityCredentials())
	})

	t.Run("valid external account", func(t *testing.T) {
		tmpFile := filepath.Join(t.TempDir(), "config.json")
		configData := ExternalAccountConfig{
			Type:     "external_account",
			Audience: "test-audience",
		}
		data, _ := json.Marshal(configData)
		_ = os.WriteFile(tmpFile, data, 0o600)

		t.Setenv(EnvGoogleExternalAccount, tmpFile)
		assert.True(t, HasWorkloadIdentityCredentials())
	})
}

func TestReadSubjectToken_FromFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	err := os.WriteFile(tokenFile, []byte("my-subject-token"), 0o600)
	require.NoError(t, err)

	cfg := &ExternalAccountConfig{
		CredentialSource: CredentialSource{
			File: tokenFile,
		},
	}

	token, err := readSubjectToken(cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-subject-token", token)
}

func TestReadSubjectToken_FromJSONFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token.json")
	tokenData := map[string]string{
		"access_token": "my-json-token",
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	err = os.WriteFile(tokenFile, data, 0o600)
	require.NoError(t, err)

	cfg := &ExternalAccountConfig{
		CredentialSource: CredentialSource{
			File: tokenFile,
			Format: &CredentialSourceFormat{
				Type:                  "json",
				SubjectTokenFieldName: "access_token",
			},
		},
	}

	token, err := readSubjectToken(cfg)
	require.NoError(t, err)
	assert.Equal(t, "my-json-token", token)
}

func TestReadSubjectToken_FileNotFound(t *testing.T) {
	cfg := &ExternalAccountConfig{
		CredentialSource: CredentialSource{
			File: "/nonexistent/token",
		},
	}

	_, err := readSubjectToken(cfg)
	require.Error(t, err)
}

func TestReadSubjectToken_NoSource(t *testing.T) {
	cfg := &ExternalAccountConfig{
		CredentialSource: CredentialSource{},
	}

	_, err := readSubjectToken(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported credential source")
}
