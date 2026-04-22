// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestDefaultsYAML_IsValidConfig(t *testing.T) {
	t.Parallel()

	// The embedded YAML must be a valid Config struct.
	var cfg Config
	err := yaml.Unmarshal(DefaultsYAML(), &cfg)
	require.NoError(t, err, "defaults.yaml must unmarshal into Config")
	assert.Equal(t, "official", cfg.Settings.DefaultCatalog)
	require.Len(t, cfg.Catalogs, 2)
	assert.Equal(t, "local", cfg.Catalogs[0].Name)
	assert.Equal(t, CatalogTypeFilesystem, cfg.Catalogs[0].Type)
	assert.Equal(t, "official", cfg.Catalogs[1].Name)
	assert.Equal(t, CatalogTypeOCI, cfg.Catalogs[1].Type)
	assert.Equal(t, "oci://ghcr.io/oakwood-commons", cfg.Catalogs[1].URL)
}

func TestDefaultsYAML_ReturnsCopy(t *testing.T) {
	t.Parallel()
	a := DefaultsYAML()
	b := DefaultsYAML()
	assert.Equal(t, a, b)

	// Mutating the returned slice must not affect the embedded data.
	a[0] = 0xFF
	c := DefaultsYAML()
	assert.NotEqual(t, a[0], c[0])
}

func TestEnsureDefaults_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "config.yaml")

	err := EnsureDefaults(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "local")
	assert.Contains(t, content, "defaultCatalog")
}

func TestEnsureDefaults_MergesMissingCatalogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Existing config with only a custom catalog, no "local".
	existing := "catalogs:\n  - name: my-registry\n    type: oci\n    url: oci://registry.example.com/myorg\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	err := EnsureDefaults(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	// "local" catalog should be merged in from defaults.
	assert.Contains(t, content, "local")
	// User's custom catalog must be preserved.
	assert.Contains(t, content, "my-registry")
	// defaultCatalog should be set since existing didn't have one.
	assert.Contains(t, content, "defaultCatalog")
}

func TestEnsureDefaults_DoesNotDuplicateExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	existing := "catalogs:\n  - name: local\n    type: filesystem\n    path: /custom/path\nsettings:\n  defaultCatalog: local\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	err := EnsureDefaults(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	catalogs := toSlice(cfg["catalogs"])

	localCount := 0
	for _, c := range catalogs {
		m, _ := c.(map[string]any)
		if m["name"] == "local" {
			localCount++
			assert.Equal(t, "/custom/path", m["path"], "user-customised path must be preserved")
		}
	}
	assert.Equal(t, 1, localCount, "local catalog must not be duplicated")
}

func TestEnsureDefaults_PreservesUserDefaultCatalog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	existing := "settings:\n  defaultCatalog: my-catalog\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	err := EnsureDefaults(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	s, _ := cfg["settings"].(map[string]any)
	require.NotNil(t, s)
	assert.Equal(t, "my-catalog", s["defaultCatalog"])
}

func TestEnsureDefaults_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	err := EnsureDefaults(path)
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestEnsureDefaults_BackfillsMissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// local catalog exists but is missing the "type" field.
	existing := "catalogs:\n  - name: local\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	err := EnsureDefaults(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	catalogs := toSlice(cfg["catalogs"])
	for _, c := range catalogs {
		m, _ := c.(map[string]any)
		if m["name"] == "local" {
			assert.Equal(t, "filesystem", m["type"], "missing type should be backfilled from defaults")
		}
	}
}

func BenchmarkEnsureDefaults(b *testing.B) {
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		path := filepath.Join(dir, "config.yaml")
		os.Remove(path)
		EnsureDefaults(path) //nolint:errcheck // benchmark
	}
}

func TestMergeDefaultCatalogEntries_DisableOfficialCatalog(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{Name: "my-catalog", Type: CatalogTypeOCI, URL: "registry.example.com"},
		},
		Settings: Settings{DisableOfficialCatalog: true},
	}

	mergeDefaultCatalogEntries(cfg)

	// "local" should be appended, "official" should NOT.
	names := make([]string, len(cfg.Catalogs))
	for i, c := range cfg.Catalogs {
		names[i] = c.Name
	}
	assert.Contains(t, names, CatalogNameLocal)
	assert.NotContains(t, names, CatalogNameOfficial)
}

func TestMergeDefaultCatalogEntries_DefaultIncludesOfficial(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{Name: "my-catalog", Type: CatalogTypeOCI, URL: "registry.example.com"},
		},
	}

	mergeDefaultCatalogEntries(cfg)

	names := make([]string, len(cfg.Catalogs))
	for i, c := range cfg.Catalogs {
		names[i] = c.Name
	}
	assert.Contains(t, names, CatalogNameLocal)
	assert.Contains(t, names, CatalogNameOfficial)
}

func TestEmbeddedCatalogDefaults(t *testing.T) {
	t.Parallel()

	catalogs := EmbeddedCatalogDefaults()
	require.NotNil(t, catalogs, "defaults.yaml must contain catalogs section")
	require.Len(t, catalogs, 2)
	assert.Equal(t, "local", catalogs[0].Name)
	assert.Equal(t, CatalogTypeFilesystem, catalogs[0].Type)
	assert.Equal(t, "official", catalogs[1].Name)
	assert.Equal(t, CatalogTypeOCI, catalogs[1].Type)
	assert.Equal(t, "oci://ghcr.io/oakwood-commons", catalogs[1].URL)
	assert.Equal(t, "github", catalogs[1].AuthProvider)
}

func TestEnsureDefaults_StatError(t *testing.T) {
	t.Parallel()

	// Use a path under a non-existent directory that we can't stat due to
	// a permission error simulation. On macOS/Linux, /dev/null is a file,
	// so /dev/null/config.yaml triggers a "not a directory" error from Stat.
	err := EnsureDefaults("/dev/null/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking config file")
}
