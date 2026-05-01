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
			assert.NotEqual(t, "/custom/path", m["path"], "reserved catalog path must be enforced from defaults")
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

func TestEnsureDefaultsWith_CreatesFileFromCustomDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	embedderDefaults := []byte(`auth:
  entra:
    clientId: embedder-client-id
    tenantId: embedder-tenant
    defaultFlow: device_code
catalogs:
  - name: local
    type: filesystem
  - name: corp-registry
    type: oci
    url: oci://registry.corp.example.com/myorg
settings:
  defaultCatalog: corp-registry
`)

	err := EnsureDefaultsWith(path, embedderDefaults)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "embedder-client-id")
	assert.Contains(t, content, "embedder-tenant")
	assert.Contains(t, content, "corp-registry")
	assert.NotContains(t, content, "official", "embedder defaults should not include scafctl official catalog")
}

func TestEnsureDefaultsWith_MergesCustomDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Existing config with user's custom catalog.
	existing := "catalogs:\n  - name: my-team\n    type: oci\n    url: oci://team.example.com\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	embedderDefaults := []byte(`catalogs:
  - name: local
    type: filesystem
  - name: corp-registry
    type: oci
    url: oci://registry.corp.example.com/myorg
settings:
  defaultCatalog: corp-registry
`)

	err := EnsureDefaultsWith(path, embedderDefaults)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	// Embedder catalogs should be merged.
	assert.Contains(t, content, "corp-registry")
	assert.Contains(t, content, "local")
	// User's catalog must be preserved.
	assert.Contains(t, content, "my-team")
	// defaultCatalog should be set from embedder defaults.
	assert.Contains(t, content, "defaultCatalog")
}

func TestEnsureDefaultsWith_PreservesReservedCatalogProtection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// User tried to override "local" catalog.
	existing := "catalogs:\n  - name: local\n    type: oci\n    url: oci://evil.example.com\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o600))

	embedderDefaults := []byte(`catalogs:
  - name: local
    type: filesystem
`)

	err := EnsureDefaultsWith(path, embedderDefaults)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg map[string]any
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	catalogs := toSlice(cfg["catalogs"])
	for _, c := range catalogs {
		m, _ := c.(map[string]any)
		if m["name"] == "local" {
			assert.Equal(t, "filesystem", m["type"], "reserved catalog must be enforced from embedder defaults")
		}
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

func TestSettings_DisableOfficialProviders(t *testing.T) {
	t.Parallel()

	t.Run("defaults to false", func(t *testing.T) {
		t.Parallel()

		var s Settings
		assert.False(t, s.DisableOfficialProviders)
	})

	t.Run("deserializes from YAML", func(t *testing.T) {
		t.Parallel()

		data := []byte(`disableOfficialProviders: true`)
		var s Settings
		require.NoError(t, yaml.Unmarshal(data, &s))
		assert.True(t, s.DisableOfficialProviders)
	})

	t.Run("independent of DisableOfficialCatalog", func(t *testing.T) {
		t.Parallel()

		s := Settings{DisableOfficialCatalog: true}
		assert.False(t, s.DisableOfficialProviders)

		s2 := Settings{DisableOfficialProviders: true}
		assert.False(t, s2.DisableOfficialCatalog)
	})
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

func TestEnsureDefaults_ReservedCatalogEnforced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// User tries to redirect "official" to an evil registry.
	existing := `catalogs:
  - name: official
    type: oci
    url: oci://evil.example.com/hijacked
    authProvider: custom-auth
  - name: my-catalog
    type: oci
    url: oci://my-registry.example.com/myorg
`
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
		if m["name"] == "official" {
			assert.Equal(t, "oci://ghcr.io/oakwood-commons", m["url"],
				"reserved official catalog URL must be enforced from defaults")
			assert.Equal(t, "github", m["authProvider"],
				"reserved official catalog authProvider must be enforced")
			assert.NotEqual(t, "custom-auth", m["authProvider"])
		}
		if m["name"] == "my-catalog" {
			assert.Equal(t, "oci://my-registry.example.com/myorg", m["url"],
				"non-reserved catalog must preserve user values")
		}
	}
}

func TestIsReservedCatalogName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected bool
	}{
		{"local", true},
		{"official", true},
		{"my-catalog", false},
		{"Local", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, IsReservedCatalogName(tt.name))
		})
	}
}

func TestMergeDefaultCatalogEntries_ReservedOverwrite(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{Name: "official", Type: CatalogTypeOCI, URL: "oci://evil.example.com/hijacked", AuthProvider: "custom-auth"},
			{Name: "my-catalog", Type: CatalogTypeOCI, URL: "oci://my-registry.example.com/myorg"},
		},
	}

	mergeDefaultCatalogEntries(cfg)

	for _, c := range cfg.Catalogs {
		if c.Name == "official" {
			assert.Equal(t, "oci://ghcr.io/oakwood-commons", c.URL,
				"reserved catalog URL must be enforced")
			assert.Equal(t, "github", c.AuthProvider,
				"reserved catalog authProvider must be enforced")
		}
		if c.Name == "my-catalog" {
			assert.Equal(t, "oci://my-registry.example.com/myorg", c.URL,
				"non-reserved catalog must preserve user values")
		}
	}
}
