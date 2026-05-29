// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"testing"
)

// isolatedCatalogEnv returns env vars that fully isolate catalog tests from
// the host environment. The returned config disables the official remote
// catalog, preventing any network calls to ghcr.io during tests that only
// exercise local catalog operations.
//
// Use this for tests that build/list/inspect local artifacts and should not
// be affected by remote catalog availability or authentication state.
func isolatedCatalogEnv(t *testing.T) map[string]string {
	t.Helper()
	tmpDir := t.TempDir()

	configDir := filepath.Join(tmpDir, "scafctl")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configContent := `# Integration test config — no remote catalogs
catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
`
	configPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	return map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}
}
