// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetKnownConfigKeys(t *testing.T) {
	keys := getKnownConfigKeys()

	// Check that top-level keys are present (all lowercase due to Viper normalization)
	assert.True(t, keys["version"], "version key should be known")
	assert.True(t, keys["catalogs"], "catalogs key should be known")
	assert.True(t, keys["settings"], "settings key should be known")
	assert.True(t, keys["logging"], "logging key should be known")
	assert.True(t, keys["httpclient"], "httpclient key should be known")

	// Check nested settings keys
	assert.True(t, keys["settings.defaultcatalog"], "settings.defaultcatalog should be known")
	assert.True(t, keys["settings.nocolor"], "settings.nocolor should be known")
	assert.True(t, keys["settings.quiet"], "settings.quiet should be known")

	// Check nested logging keys
	assert.True(t, keys["logging.level"], "logging.level should be known")
	assert.True(t, keys["logging.format"], "logging.format should be known")
	assert.True(t, keys["logging.timestamps"], "logging.timestamps should be known")
	assert.True(t, keys["logging.enableprofiling"], "logging.enableprofiling should be known")

	// Check HTTP client keys
	assert.True(t, keys["httpclient.timeout"], "httpclient.timeout should be known")
	assert.True(t, keys["httpclient.retrymax"], "httpclient.retrymax should be known")
	assert.True(t, keys["httpclient.enablecache"], "httpclient.enablecache should be known")
	assert.True(t, keys["httpclient.cachetype"], "httpclient.cachetype should be known")

	// Check array element keys (with wildcard)
	assert.True(t, keys["catalogs.*.name"], "catalogs.*.name should be known")
	assert.True(t, keys["catalogs.*.type"], "catalogs.*.type should be known")
	assert.True(t, keys["catalogs.*.path"], "catalogs.*.path should be known")
	assert.True(t, keys["catalogs.*.url"], "catalogs.*.url should be known")
	assert.True(t, keys["catalogs.*.auth"], "catalogs.*.auth should be known")
	assert.True(t, keys["catalogs.*.metadata"], "catalogs.*.metadata should be known")
	assert.True(t, keys["catalogs.*.httpclient"], "catalogs.*.httpclient should be known")

	// Check nested catalog auth keys
	assert.True(t, keys["catalogs.*.auth.type"], "catalogs.*.auth.type should be known")
	assert.True(t, keys["catalogs.*.auth.tokenenvvar"], "catalogs.*.auth.tokenenvvar should be known")

	// Check nested catalog httpclient keys
	assert.True(t, keys["catalogs.*.httpclient.timeout"], "catalogs.*.httpclient.timeout should be known")
}

func TestIsKnownKey(t *testing.T) {
	knownKeys := getKnownConfigKeys()

	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{"top level key", "version", true},
		{"nested key", "logging.level", true},
		{"array element key", "catalogs.0.name", true},
		{"array element nested key", "catalogs.0.auth.type", true},
		{"multiple array indices", "catalogs.5.name", true},
		{"http client key", "httpclient.timeout", true},
		{"catalog http client key", "catalogs.0.httpclient.timeout", true},

		{"unknown top level", "unknownfield", false},
		{"unknown nested", "settings.unknownsetting", false},
		{"typo in key", "htpclient.timeout", false},
		{"typo in nested", "httpclient.timeot", false},
		{"unknown catalog field", "catalogs.0.unknownfield", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isKnownKey(tc.key, knownKeys)
			assert.Equal(t, tc.expected, result, "isKnownKey(%q) should be %v", tc.key, tc.expected)
		})
	}
}

func TestNormalizeArrayKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"catalogs.0.name", "catalogs.*.name"},
		{"catalogs.10.auth.type", "catalogs.*.auth.type"},
		{"logging.level", "logging.level"},
		{"httpClient.timeout", "httpClient.timeout"},
		{"catalogs.0.httpClient.1.timeout", "catalogs.*.httpClient.*.timeout"},
		{"0.1.2", "*.*.*"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeArrayKey(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0", true},
		{"123", true},
		{"00", true},
		{"", false},
		{"abc", false},
		{"12a", false},
		{"a12", false},
		{"-1", false},
		{"1.5", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, isNumeric(tc.input))
		})
	}
}

func TestManager_GetUnknownKeys(t *testing.T) {
	// Create a temp config file with unknown keys
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Note: Viper stores arrays as single values, so unknown fields inside
	// array elements (like catalogs.0.unknownField) won't be detected as
	// individual keys. Only top-level and nested object keys are tracked.
	configContent := `
version: 1
settings:
  defaultCatalog: test
  unknownSetting: value
htpclient:
  timeout: 30s
httpClient:
  timeout: 30s
  unknownOption: true
catalogs:
  - name: test
    type: filesystem
    path: /tmp
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	unknownKeys := mgr.GetUnknownKeys()

	// Should find these unknown keys (Viper lowercases all keys)
	assert.Contains(t, unknownKeys, "settings.unknownsetting", "should detect settings.unknownsetting")
	assert.Contains(t, unknownKeys, "htpclient.timeout", "should detect htpclient typo")
	assert.Contains(t, unknownKeys, "httpclient.unknownoption", "should detect httpclient.unknownoption")

	// Note: Unknown fields inside array elements are NOT detected because
	// Viper stores arrays as single values, not as individual indexed keys.
}

func TestManager_GetUnknownKeys_NoUnknown(t *testing.T) {
	// Create a temp config file with only valid keys
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: 1
settings:
  defaultCatalog: test
  noColor: true
httpClient:
  timeout: 30s
  retryMax: 3
catalogs:
  - name: test
    type: filesystem
    path: /tmp
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	unknownKeys := mgr.GetUnknownKeys()
	assert.Empty(t, unknownKeys, "should not find any unknown keys")
}

func TestManager_WarnUnknownKeys(t *testing.T) {
	// Create a temp config file with unknown keys
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: 1
settings:
  unknownSetting: value
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	// Capture log output
	var logBuf bytes.Buffer
	testLogger := funcr.New(func(prefix, args string) {
		logBuf.WriteString(args)
	}, funcr.Options{})

	ctx := logger.WithLogger(context.Background(), &testLogger)

	// Call WarnUnknownKeys
	mgr.WarnUnknownKeys(ctx)

	// Check that the warning was logged
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "unknown config key", "should log warning for unknown key")
	assert.Contains(t, logOutput, "unknownsetting", "should mention the unknown key")
}

func TestManager_WarnUnknownKeys_NilViper(t *testing.T) {
	mgr := &Manager{v: nil}

	// Should not panic
	discardLogger := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &discardLogger)
	mgr.WarnUnknownKeys(ctx)
}

func TestManager_GetUnknownKeys_NilViper(t *testing.T) {
	mgr := &Manager{v: nil}

	result := mgr.GetUnknownKeys()
	assert.Nil(t, result)
}

func TestManager_GetUnknownKeys_MetadataAllowed(t *testing.T) {
	// Metadata fields can have arbitrary keys - they should NOT be flagged as unknown
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: 1
catalogs:
  - name: test
    type: filesystem
    path: /tmp
    metadata:
      customKey1: value1
      customKey2: value2
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	unknownKeys := mgr.GetUnknownKeys()

	// Metadata keys should NOT be flagged as unknown
	for _, key := range unknownKeys {
		assert.NotContains(t, key, "metadata.customKey", "metadata arbitrary keys should not be flagged")
	}
}
