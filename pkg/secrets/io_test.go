// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/secrets/secretcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestImport_PlaintextJSON(t *testing.T) {
	data := secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "secret-value"},
			{Name: "another-secret", Value: "another-value"},
		},
	}
	jsonData, err := json.Marshal(data)
	require.NoError(t, err)

	result, err := Import(jsonData, ImportOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 2)
	assert.Equal(t, "my-secret", result.Secrets[0].Name)
	assert.Equal(t, "secret-value", result.Secrets[0].Value)
	assert.Equal(t, 0, result.SkippedInternal)
	assert.False(t, result.VersionMismatch)
}

func TestImport_PlaintextYAML(t *testing.T) {
	data := secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "yaml-secret", Value: "yaml-value"},
		},
	}
	yamlData, err := yaml.Marshal(data)
	require.NoError(t, err)

	result, err := Import(yamlData, ImportOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 1)
	assert.Equal(t, "yaml-secret", result.Secrets[0].Name)
}

func TestImport_FiltersInternalSecrets(t *testing.T) {
	data := secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "user-secret", Value: "value1"},
			{Name: "scafctl.auth.token", Value: "internal-value"},
			{Name: "other-secret", Value: "value2"},
		},
	}
	jsonData, err := json.Marshal(data)
	require.NoError(t, err)

	result, err := Import(jsonData, ImportOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 2)
	assert.Equal(t, "user-secret", result.Secrets[0].Name)
	assert.Equal(t, "other-secret", result.Secrets[1].Name)
	assert.Equal(t, 1, result.SkippedInternal)
}

func TestImport_VersionMismatch(t *testing.T) {
	data := secretcrypto.ExportFormat{
		Version: "old-version",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "value"},
		},
	}
	jsonData, err := json.Marshal(data)
	require.NoError(t, err)

	result, err := Import(jsonData, ImportOptions{})
	require.NoError(t, err)
	assert.True(t, result.VersionMismatch)
	assert.Equal(t, "old-version", result.Version)
}

func TestImport_Encrypted(t *testing.T) {
	original := secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "encrypted-secret", Value: "encrypted-value"},
		},
	}
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	encrypted, err := secretcrypto.Encrypt(jsonData, "test-password")
	require.NoError(t, err)

	result, err := Import(encrypted, ImportOptions{Password: "test-password"})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 1)
	assert.Equal(t, "encrypted-secret", result.Secrets[0].Name)
}

func TestImport_EncryptedNoPassword(t *testing.T) {
	original := secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret", Value: "value"},
		},
	}
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	encrypted, err := secretcrypto.Encrypt(jsonData, "test-password")
	require.NoError(t, err)

	_, err = Import(encrypted, ImportOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no password provided")
}

func TestImport_InvalidData(t *testing.T) {
	_, err := Import([]byte("not valid data {{{"), ImportOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestExport_JSON(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "my-value"},
		},
	}

	result, err := Export(data, ExportOptions{Format: "json"})
	require.NoError(t, err)

	var parsed secretcrypto.ExportFormat
	require.NoError(t, json.Unmarshal(result, &parsed))
	assert.Equal(t, "my-secret", parsed.Secrets[0].Name)
}

func TestExport_YAML(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "my-value"},
		},
	}

	result, err := Export(data, ExportOptions{Format: "yaml"})
	require.NoError(t, err)

	var parsed secretcrypto.ExportFormat
	require.NoError(t, yaml.Unmarshal(result, &parsed))
	assert.Equal(t, "my-secret", parsed.Secrets[0].Name)
}

func TestExport_DefaultFormatIsYAML(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "my-value"},
		},
	}

	result, err := Export(data, ExportOptions{})
	require.NoError(t, err)

	// Should be valid YAML
	var parsed secretcrypto.ExportFormat
	require.NoError(t, yaml.Unmarshal(result, &parsed))
	assert.Equal(t, "my-secret", parsed.Secrets[0].Name)
}

func TestExport_Encrypted(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "my-secret", Value: "my-value"},
		},
	}

	result, err := Export(data, ExportOptions{
		Format:   "json",
		Encrypt:  true,
		Password: "test-password",
	})
	require.NoError(t, err)
	assert.True(t, len(result) > 0)

	// Should be decryptable
	decrypted, err := secretcrypto.Decrypt(result, "test-password")
	require.NoError(t, err)

	var parsed secretcrypto.ExportFormat
	require.NoError(t, json.Unmarshal(decrypted, &parsed))
	assert.Equal(t, "my-secret", parsed.Secrets[0].Name)
}

func TestExport_EncryptNoPassword(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{},
	}

	_, err := Export(data, ExportOptions{Encrypt: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no password provided")
}

func TestExport_UnsupportedFormat(t *testing.T) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{},
	}

	_, err := Export(data, ExportOptions{Format: "xml"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestImportExportRoundTrip(t *testing.T) {
	original := &secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret1", Value: "value1"},
			{Name: "secret2", Value: "value2"},
		},
	}

	// Export as JSON
	exported, err := Export(original, ExportOptions{Format: "json"})
	require.NoError(t, err)

	// Import back
	result, err := Import(exported, ImportOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 2)
	assert.Equal(t, "secret1", result.Secrets[0].Name)
	assert.Equal(t, "value1", result.Secrets[0].Value)
	assert.Equal(t, "secret2", result.Secrets[1].Name)
	assert.Equal(t, "value2", result.Secrets[1].Value)
}

func TestImportExportRoundTrip_Encrypted(t *testing.T) {
	original := &secretcrypto.ExportFormat{
		Version:    secretcrypto.ExportVersion,
		ExportedAt: "2026-01-01T00:00:00Z",
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret1", Value: "value1"},
		},
	}

	// Export encrypted
	exported, err := Export(original, ExportOptions{
		Format:   "json",
		Encrypt:  true,
		Password: "roundtrip-password",
	})
	require.NoError(t, err)

	// Import back
	result, err := Import(exported, ImportOptions{Password: "roundtrip-password"})
	require.NoError(t, err)
	assert.Len(t, result.Secrets, 1)
	assert.Equal(t, "secret1", result.Secrets[0].Name)
}

func BenchmarkImport_PlaintextJSON(b *testing.B) {
	data := secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret1", Value: "value1"},
			{Name: "secret2", Value: "value2"},
			{Name: "secret3", Value: "value3"},
		},
	}
	jsonData, _ := json.Marshal(data)

	for b.Loop() {
		_, _ = Import(jsonData, ImportOptions{})
	}
}

func BenchmarkExport_JSON(b *testing.B) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret1", Value: "value1"},
			{Name: "secret2", Value: "value2"},
			{Name: "secret3", Value: "value3"},
		},
	}

	for b.Loop() {
		_, _ = Export(data, ExportOptions{Format: "json"})
	}
}

func BenchmarkExport_YAML(b *testing.B) {
	data := &secretcrypto.ExportFormat{
		Version: secretcrypto.ExportVersion,
		Secrets: []secretcrypto.ExportedSecret{
			{Name: "secret1", Value: "value1"},
			{Name: "secret2", Value: "value2"},
			{Name: "secret3", Value: "value3"},
		},
	}

	for b.Loop() {
		_, _ = Export(data, ExportOptions{Format: "yaml"})
	}
}

func TestIsEncrypted(t *testing.T) {
	assert.True(t, IsEncrypted([]byte(secretcrypto.EncryptedHeader+"rest")))
	assert.False(t, IsEncrypted([]byte("plain text")))
	assert.False(t, IsEncrypted([]byte{}))
}
