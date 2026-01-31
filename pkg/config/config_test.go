package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_Load_NoFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent", "config.yaml")

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()

	require.NoError(t, err)
	assert.NotNil(t, cfg)
	// Default catalog should be configured
	assert.Len(t, cfg.Catalogs, 1)
	assert.Equal(t, "local", cfg.Catalogs[0].Name)
	assert.Equal(t, CatalogTypeFilesystem, cfg.Catalogs[0].Type)
	assert.Equal(t, paths.CatalogDir(), cfg.Catalogs[0].Path)
	assert.Equal(t, 0, cfg.Logging.Level)
	assert.Equal(t, "local", cfg.Settings.DefaultCatalog)
	assert.False(t, cfg.Settings.NoColor)
	assert.False(t, cfg.Settings.Quiet)
}

func TestManager_Load_WithFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: test
    type: filesystem
    path: ./test
settings:
  defaultCatalog: test
logging:
  level: 1
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()

	require.NoError(t, err)
	assert.Len(t, cfg.Catalogs, 1)
	assert.Equal(t, "test", cfg.Catalogs[0].Name)
	assert.Equal(t, "filesystem", cfg.Catalogs[0].Type)
	assert.Equal(t, "./test", cfg.Catalogs[0].Path)
	assert.Equal(t, "test", cfg.Settings.DefaultCatalog)
	assert.Equal(t, 1, cfg.Logging.Level)
}

func TestManager_Load_WithFullConfig(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: local
    type: filesystem
    path: ./catalogs
  - name: remote
    type: oci
    url: oci://registry.example.com/catalog
    auth:
      type: token
      tokenEnvVar: REGISTRY_TOKEN
    metadata:
      owner: team-a
settings:
  defaultCatalog: local
  noColor: true
  quiet: false
logging:
  level: -1
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()

	require.NoError(t, err)
	assert.Len(t, cfg.Catalogs, 2)

	// Check first catalog
	assert.Equal(t, "local", cfg.Catalogs[0].Name)
	assert.Equal(t, "filesystem", cfg.Catalogs[0].Type)
	assert.Equal(t, "./catalogs", cfg.Catalogs[0].Path)

	// Check second catalog with auth
	assert.Equal(t, "remote", cfg.Catalogs[1].Name)
	assert.Equal(t, "oci", cfg.Catalogs[1].Type)
	assert.Equal(t, "oci://registry.example.com/catalog", cfg.Catalogs[1].URL)
	assert.NotNil(t, cfg.Catalogs[1].Auth)
	assert.Equal(t, "token", cfg.Catalogs[1].Auth.Type)
	assert.Equal(t, "REGISTRY_TOKEN", cfg.Catalogs[1].Auth.TokenEnvVar)
	assert.Equal(t, "team-a", cfg.Catalogs[1].Metadata["owner"])

	// Check settings
	assert.Equal(t, "local", cfg.Settings.DefaultCatalog)
	assert.True(t, cfg.Settings.NoColor)
	assert.False(t, cfg.Settings.Quiet)
	assert.Equal(t, -1, cfg.Logging.Level)
}

func TestManager_Load_InvalidYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
catalogs:
  - name: test
    type: filesystem
    path: ./test
  invalid yaml here
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()

	assert.Error(t, err)
}

func TestManager_Save(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	// Modify config
	cfg.Catalogs = append(cfg.Catalogs, CatalogConfig{
		Name: "new-catalog",
		Type: CatalogTypeFilesystem,
		Path: "./new",
	})
	cfg.Logging.Level = 2

	// Save
	err = mgr.Save()
	require.NoError(t, err)

	// Load again and verify
	mgr2 := NewManager(configPath)
	cfg2, err := mgr2.Load()
	require.NoError(t, err)

	// Should have default catalog + newly added catalog
	assert.Len(t, cfg2.Catalogs, 2)
	assert.Equal(t, "local", cfg2.Catalogs[0].Name)
	assert.Equal(t, "new-catalog", cfg2.Catalogs[1].Name)
	assert.Equal(t, 2, cfg2.Logging.Level)
}

func TestManager_SaveAs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	savePath := filepath.Join(tmpDir, "saved", "config.yaml")

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)

	cfg.Catalogs = append(cfg.Catalogs, CatalogConfig{
		Name: "test",
		Type: CatalogTypeFilesystem,
	})

	err = mgr.SaveAs(savePath)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(savePath)
	require.NoError(t, err)

	// Load from saved path
	mgr2 := NewManager(savePath)
	cfg2, err := mgr2.Load()
	require.NoError(t, err)

	// Should have default catalog + newly added catalog
	assert.Len(t, cfg2.Catalogs, 2)
	assert.Equal(t, "local", cfg2.Catalogs[0].Name)
	assert.Equal(t, "test", cfg2.Catalogs[1].Name)
}

func TestManager_GetSet(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	// Set and get
	mgr.Set("logging.level", 3)
	assert.Equal(t, 3, mgr.Get("logging.level"))

	mgr.Set("settings.noColor", true)
	assert.Equal(t, true, mgr.Get("settings.noColor"))
}

func TestManager_ConfigPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, configPath, mgr.ConfigPath())
}

func TestManager_IsSet(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
logging:
  level: 1
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	assert.True(t, mgr.IsSet("logging.level"))
	assert.False(t, mgr.IsSet("nonexistent.key"))
}

func TestConfig_GetCatalog(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Catalogs: []CatalogConfig{
			{Name: "foo", Type: CatalogTypeFilesystem, Path: "./foo"},
			{Name: "bar", Type: CatalogTypeOCI, URL: "oci://example.com/bar"},
		},
	}

	cat, ok := cfg.GetCatalog("foo")
	assert.True(t, ok)
	assert.Equal(t, "foo", cat.Name)
	assert.Equal(t, CatalogTypeFilesystem, cat.Type)

	cat, ok = cfg.GetCatalog("bar")
	assert.True(t, ok)
	assert.Equal(t, "bar", cat.Name)
	assert.Equal(t, CatalogTypeOCI, cat.Type)

	_, ok = cfg.GetCatalog("nonexistent")
	assert.False(t, ok)
}

func TestConfig_GetDefaultCatalog(t *testing.T) {
	t.Parallel()

	t.Run("returns_default_when_set", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "default", Type: CatalogTypeFilesystem},
			},
			Settings: Settings{
				DefaultCatalog: "default",
			},
		}

		cat, ok := cfg.GetDefaultCatalog()
		assert.True(t, ok)
		assert.Equal(t, "default", cat.Name)
	})

	t.Run("returns_false_when_empty", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		_, ok := cfg.GetDefaultCatalog()
		assert.False(t, ok)
	})

	t.Run("returns_false_when_not_found", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Settings: Settings{
				DefaultCatalog: "nonexistent",
			},
		}

		_, ok := cfg.GetDefaultCatalog()
		assert.False(t, ok)
	})
}

func TestConfig_AddCatalog(t *testing.T) {
	t.Parallel()

	t.Run("adds_new_catalog", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		err := cfg.AddCatalog(CatalogConfig{Name: "new", Type: CatalogTypeFilesystem})
		assert.NoError(t, err)
		assert.Len(t, cfg.Catalogs, 1)
		assert.Equal(t, "new", cfg.Catalogs[0].Name)
	})

	t.Run("errors_on_duplicate", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "existing", Type: CatalogTypeFilesystem},
			},
		}

		err := cfg.AddCatalog(CatalogConfig{Name: "existing", Type: CatalogTypeFilesystem})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestConfig_RemoveCatalog(t *testing.T) {
	t.Parallel()

	t.Run("removes_existing_catalog", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "foo", Type: CatalogTypeFilesystem},
				{Name: "bar", Type: CatalogTypeOCI},
			},
		}

		err := cfg.RemoveCatalog("foo")
		assert.NoError(t, err)
		assert.Len(t, cfg.Catalogs, 1)
		assert.Equal(t, "bar", cfg.Catalogs[0].Name)
	})

	t.Run("removes_last_catalog", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "only", Type: CatalogTypeFilesystem},
			},
		}

		err := cfg.RemoveCatalog("only")
		assert.NoError(t, err)
		assert.Empty(t, cfg.Catalogs)
	})

	t.Run("errors_on_nonexistent", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "foo", Type: CatalogTypeFilesystem},
			},
		}

		err := cfg.RemoveCatalog("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestConfig_SetDefaultCatalog(t *testing.T) {
	t.Parallel()

	t.Run("sets_existing_catalog_as_default", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Catalogs: []CatalogConfig{
				{Name: "foo", Type: CatalogTypeFilesystem},
			},
		}

		err := cfg.SetDefaultCatalog("foo")
		assert.NoError(t, err)
		assert.Equal(t, "foo", cfg.Settings.DefaultCatalog)
	})

	t.Run("clears_default_with_empty_string", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Settings: Settings{
				DefaultCatalog: "foo",
			},
		}

		err := cfg.SetDefaultCatalog("")
		assert.NoError(t, err)
		assert.Empty(t, cfg.Settings.DefaultCatalog)
	})

	t.Run("errors_on_nonexistent_catalog", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}

		err := cfg.SetDefaultCatalog("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestValidCatalogTypes(t *testing.T) {
	t.Parallel()

	types := ValidCatalogTypes()
	assert.Len(t, types, 3)
	assert.Contains(t, types, CatalogTypeFilesystem)
	assert.Contains(t, types, CatalogTypeOCI)
	assert.Contains(t, types, CatalogTypeHTTP)
}

func TestIsValidCatalogType(t *testing.T) {
	t.Parallel()

	assert.True(t, IsValidCatalogType(CatalogTypeFilesystem))
	assert.True(t, IsValidCatalogType(CatalogTypeOCI))
	assert.True(t, IsValidCatalogType(CatalogTypeHTTP))
	assert.False(t, IsValidCatalogType("invalid"))
	assert.False(t, IsValidCatalogType(""))
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	path, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.Contains(t, path, "scafctl")
	assert.Contains(t, path, DefaultConfigFileName)
	assert.Contains(t, path, "."+DefaultConfigFileType)
}

func TestCatalogTypeConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "filesystem", CatalogTypeFilesystem)
	assert.Equal(t, "oci", CatalogTypeOCI)
	assert.Equal(t, "http", CatalogTypeHTTP)
}

func TestManager_AllSettings(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
settings:
  logLevel: 2
  noColor: true
`
	err := os.WriteFile(configPath, []byte(configContent), 0o600)
	require.NoError(t, err)

	mgr := NewManager(configPath)
	_, err = mgr.Load()
	require.NoError(t, err)

	all := mgr.AllSettings()
	assert.NotNil(t, all)
	assert.NotEmpty(t, all)
}
