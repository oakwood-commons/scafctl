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

func TestValidSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    Source
		want bool
	}{
		{"default", SourceDefault, true},
		{"dropin", SourceDropIn, true},
		{"file", SourceFile, true},
		{"env", SourceEnv, true},
		{"unknown", Source("bogus"), false},
		{"empty", Source(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ValidSource(tt.s))
		})
	}
}

func TestRedactSensitiveEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"plain", "SCAFCTL_LOGGING_LEVEL", "debug", "debug"},
		{"token upper", "SCAFCTL_GITHUB_TOKEN", "abc123", RedactedValue},
		{"token lower substring", "MYCLI_ACCESS_token", "xyz", RedactedValue},
		{"password", "SCAFCTL_ADMIN_PASSWORD", "hunter2", RedactedValue},
		{"secret", "SCAFCTL_CLIENT_SECRET", "s", RedactedValue},
		{"credential", "SCAFCTL_CREDENTIAL_FILE", "/tmp/c", RedactedValue},
		{"apikey no underscore", "SCAFCTL_MYAPIKEY", "k", RedactedValue},
		{"api_key underscore", "SCAFCTL_MY_API_KEY", "k", RedactedValue},
		{"privatekey", "SCAFCTL_PRIVATEKEY", "p", RedactedValue},
		{"private_key underscore", "SCAFCTL_PRIVATE_KEY", "p", RedactedValue},
		{"unrelated", "SCAFCTL_NO_COLOR", "true", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RedactSensitiveEnv(tt.key, tt.value))
		})
	}
}

func TestManager_EnvOverrides(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")
	t.Setenv("SCAFCTL_GITHUB_TOKEN", "secret-value")
	t.Setenv("UNRELATED_VAR", "ignored")

	mgr := NewManager("")
	// Look up by key rather than by index: CI runners may set additional
	// SCAFCTL_* vars (e.g. SCAFCTL_SECRET_KEY) that would break a Len check.
	got := envOverridesByKey(mgr.EnvOverrides())

	level, ok := got["SCAFCTL_LOGGING_LEVEL"]
	require.True(t, ok, "SCAFCTL_LOGGING_LEVEL missing from overrides")
	assert.Equal(t, "debug", level)

	token, ok := got["SCAFCTL_GITHUB_TOKEN"]
	require.True(t, ok, "SCAFCTL_GITHUB_TOKEN missing from overrides")
	assert.Equal(t, RedactedValue, token, "sensitive value must be redacted")

	assert.NotContains(t, got, "UNRELATED_VAR", "non-prefixed vars must be excluded")
}

func TestManager_EnvOverrides_CustomPrefix(t *testing.T) {
	t.Setenv("MYCLI_LOGGING_LEVEL", "info")
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	mgr := NewManager("", WithEnvPrefix("MYCLI"))
	got := envOverridesByKey(mgr.EnvOverrides())

	level, ok := got["MYCLI_LOGGING_LEVEL"]
	require.True(t, ok, "MYCLI_LOGGING_LEVEL missing under embedder prefix")
	assert.Equal(t, "info", level)

	assert.NotContains(t, got, "SCAFCTL_LOGGING_LEVEL",
		"embedder prefix must ignore the default SCAFCTL_* namespace")
}

// envOverridesByKey rekeys a slice of EnvOverride for easy lookup by env var
// name.
func envOverridesByKey(list []EnvOverride) map[string]string {
	out := make(map[string]string, len(list))
	for _, e := range list {
		out[e.Key] = e.Value
	}
	return out
}

func TestManager_Sources(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(`
settings:
  defaultCatalog: from-file
logging:
  format: json
`), 0o600))

	// config.d fragment sets a distinct key
	require.NoError(t, os.Mkdir(filepath.Join(tmpDir, ConfigDirName), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, ConfigDirName, "10-dropin.yaml"),
		[]byte("logging:\n  timestamps: false\n"),
		0o600,
	))

	// Env override on a third key
	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	sources := mgr.Sources()

	// Note: viper lowercases every key, so `defaultCatalog` from the YAML file
	// surfaces here as `settings.defaultcatalog`. Tests must use the normalized form.
	assert.Equal(t, SourceEnv, sources["logging.level"], "logging.level from env")
	assert.Equal(t, SourceFile, sources["settings.defaultcatalog"], "settings.defaultCatalog from file")
	assert.Equal(t, SourceFile, sources["logging.format"], "logging.format from file")
	assert.Equal(t, SourceDropIn, sources["logging.timestamps"], "logging.timestamps from dropin")
	assert.Equal(t, SourceDefault, sources["settings.nocolor"], "settings.noColor left at default")
	assert.Equal(t, SourceDefault, sources["cel.costlimit"], "cel.costLimit left at default")
}

func TestManager_Sources_EmptyBeforeLoad(t *testing.T) {
	t.Parallel()

	mgr := NewManager("")
	got := mgr.Sources()
	// Before Load, viper has no defaults registered, so AllKeys is empty.
	assert.Empty(t, got)
}

func TestFilterMapBySource(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"defaultcatalog": "official",
		"nocolor":        true,
		"quiet":          false,
	}
	sources := map[string]Source{
		"settings.defaultcatalog": SourceFile,
		"settings.nocolor":        SourceEnv,
		"settings.quiet":          SourceDefault,
	}

	got := FilterMapBySource(data, sources, SourceFile, "settings")
	assert.Equal(t, map[string]any{"defaultcatalog": "official"}, got)

	got = FilterMapBySource(data, sources, SourceEnv, "settings")
	assert.Equal(t, map[string]any{"nocolor": true}, got)

	got = FilterMapBySource(data, sources, SourceDefault, "settings")
	assert.Equal(t, map[string]any{"quiet": false}, got)
}

func TestFilterMapBySource_Nested(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"cache": map[string]any{
			"enabled": true,
			"dir":     "/tmp/scafctl",
		},
		"timeout": "30s",
	}
	sources := map[string]Source{
		"httpclient.cache.enabled": SourceFile,
		"httpclient.cache.dir":     SourceDefault,
		"httpclient.timeout":       SourceEnv,
	}

	got := FilterMapBySource(data, sources, SourceFile, "httpclient")
	require.Contains(t, got, "cache")
	assert.Equal(t, map[string]any{"enabled": true}, got["cache"])
	assert.NotContains(t, got, "timeout")
}

func TestFilterMapBySource_EmptyResult(t *testing.T) {
	t.Parallel()

	data := map[string]any{"a": 1, "b": 2}
	sources := map[string]Source{
		"x.a": SourceFile,
		"x.b": SourceFile,
	}
	// Filtering for a source no key matches produces an empty (but non-nil) map.
	got := FilterMapBySource(data, sources, SourceEnv, "x")
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestManager_Sources_EnvWinsOverFile(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(
		"logging:\n  level: warn\n",
	), 0o600))

	t.Setenv("SCAFCTL_LOGGING_LEVEL", "debug")

	mgr := NewManager(configPath)
	_, err := mgr.Load()
	require.NoError(t, err)

	assert.Equal(t, SourceEnv, mgr.Sources()["logging.level"],
		"env override must win when both env and file supply the same key")
}

func TestAllSourcesOrdered(t *testing.T) {
	t.Parallel()
	// Locks precedence order so Sources() can be reasoned about.
	assert.Equal(t, []Source{SourceDefault, SourceDropIn, SourceFile, SourceEnv}, AllSources)
}
