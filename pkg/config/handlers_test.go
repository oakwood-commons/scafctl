// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManager_Load_AuthHandlersNamespace verifies that the open
// auth.handlers.<name> namespace round-trips through viper/mapstructure: the
// reserved "hostname" block is parsed into typed fields, and every other key is
// captured into the opaque Settings passthrough map.
func TestManager_Load_AuthHandlersNamespace(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: 1
auth:
  handlers:
    openshift:
      hostname:
        aliases:
          pd1020: https://api.pd1020.example.com:6443
        resolver:
          source:
            url: https://clusters.example.com
            authProvider: entra
            authScope: api://fleet/.default
          transform: '_.map(k, {"name": k, "url": _[k].apiServerURL})'
          ttl: 1h
      apiTimeout: 30
      caBundlePath: /etc/ssl/corp.pem
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	handler, ok := cfg.Auth.Handlers["openshift"]
	require.True(t, ok, "expected openshift handler entry")

	// Host-consumed hostname block parsed into typed fields.
	require.NotNil(t, handler.Hostname)
	assert.Equal(t, "https://api.pd1020.example.com:6443", handler.Hostname.Aliases["pd1020"])
	require.NotNil(t, handler.Hostname.Resolver)
	assert.Equal(t, "https://clusters.example.com", handler.Hostname.Resolver.Source.URL)
	assert.Equal(t, "entra", handler.Hostname.Resolver.Source.AuthProvider)
	assert.Equal(t, "api://fleet/.default", handler.Hostname.Resolver.Source.AuthScope)
	assert.Equal(t, "1h", handler.Hostname.Resolver.TTL)
	assert.NotEmpty(t, handler.Hostname.Resolver.Transform)

	// Opaque passthrough: every key other than "hostname" lands in Settings.
	// Viper lowercases keys.
	require.NotNil(t, handler.Settings)
	assert.NotContains(t, handler.Settings, "hostname", "hostname must not leak into opaque Settings")
	assert.Contains(t, handler.Settings, "apitimeout")
	assert.Contains(t, handler.Settings, "cabundlepath")
}

// TestManager_GetUnknownKeys_AuthHandlersAllowed verifies that arbitrary keys
// under the open auth.handlers.<name> namespace are not flagged as unknown.
func TestManager_GetUnknownKeys_AuthHandlersAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
version: 1
auth:
  handlers:
    openshift:
      hostname:
        aliases:
          pd1020: https://api.pd1020.example.com:6443
      apiTimeout: 30
      nested:
        deeply:
          custom: value
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	for _, key := range mgr.GetUnknownKeys() {
		assert.NotContains(t, key, "auth.handlers", "auth.handlers.* keys must not be flagged as unknown: %s", key)
	}
}
