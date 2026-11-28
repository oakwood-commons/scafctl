// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestIntegration_PluginsUpdate_AllUpToDate_OutputModes(t *testing.T) {
	t.Parallel()

	pluginHost, pluginReg := newMutablePluginRegistry(t)
	pluginReg.publishPlugin("provider", "exec", "1.0.0")

	xdgRoot := t.TempDir()
	cfgDir := filepath.Join(xdgRoot, "config", "scafctl")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	cfgYAML := fmt.Sprintf(`catalogs:
  - name: plugins-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
    artifactKinds:
      - provider
settings:
  disableOfficialCatalog: true
`, pluginHost)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	cacheDir := filepath.Join(xdgRoot, "cache", "scafctl", "plugins")
	_, err := plugin.NewCache(cacheDir).Put("exec", "1.0.0", "linux/amd64", []byte("fake-exec-1.0.0"))
	require.NoError(t, err)

	env := map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(xdgRoot, "config"),
		"XDG_DATA_HOME":   filepath.Join(xdgRoot, "data"),
		"XDG_CACHE_HOME":  filepath.Join(xdgRoot, "cache"),
	}

	// Default table/auto mode with --all prints status on stderr with no trailing kvx box on stdout.
	stdout, stderr, exitCode := runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir)
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "All plugins are up to date.")
	assert.Empty(t, stdout)
	assert.NotContains(t, stdout, "(value)")
	assert.NotContains(t, stdout, "[]")

	// Named plugin lookup confirms the seeded binary is found and verified current against the remote catalog.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "exec", "--platform", "linux/amd64", "--cache-dir", cacheDir)
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "All plugins are up to date.")
	assert.Empty(t, stdout)

	// Explicit -o table also keeps stdout empty when all plugins are current.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "table")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "All plugins are up to date.")
	assert.Empty(t, stdout)

	// Quiet mode suppresses both stdout and stderr.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "quiet")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	// Structured -o json still emits a valid empty array on stdout.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "json")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "All plugins are up to date.")
	assert.Equal(t, "[]", strings.TrimSpace(stdout))
	var jsonItems []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &jsonItems))
	assert.Empty(t, jsonItems)

	// Structured -o yaml still emits a valid empty array on stdout.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "yaml")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "All plugins are up to date.")
	assert.Equal(t, "[]", strings.TrimSpace(stdout))
	var yamlItems []map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(stdout), &yamlItems))
	assert.Empty(t, yamlItems)

	// Unsupported -o test and malformed --where still fail validation when no updates exist.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "test")
	assert.NotEqual(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "All plugins are up to date.")

	stdout, stderr, exitCode = runScafctlWithEnv(t, env,
		"plugins", "update", "--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-w", "invalid(((")
	assert.NotEqual(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "All plugins are up to date.")
}

func TestIntegration_PluginsUpdate_EmbedderBinaryName_AllUpToDate(t *testing.T) {
	t.Parallel()

	testBin, err := os.Executable()
	require.NoError(t, err)

	pluginHost, pluginReg := newMutablePluginRegistry(t)
	pluginReg.publishPlugin("provider", "exec", "1.0.0")

	xdgRoot := t.TempDir()
	cfgDir := filepath.Join(xdgRoot, "config", "mycli")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	cfgYAML := fmt.Sprintf(`catalogs:
  - name: plugins-remote
    type: oci
    url: oci://%s/scafctl
    insecure: true
    artifactKinds:
      - provider
settings:
  disableOfficialCatalog: true
`, pluginHost)
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	// Seed under XDG_CACHE_HOME/mycli/plugins and omit --cache-dir so the embedder's
	// binary name drives both config and default plugin cache path resolution.
	mycliCacheDir := filepath.Join(xdgRoot, "cache", "mycli", "plugins")
	_, err = plugin.NewCache(mycliCacheDir).Put("exec", "1.0.0", "linux/amd64", []byte("fake-exec-1.0.0"))
	require.NoError(t, err)

	overrides := map[string]string{
		"SCAFCTL_TEST_EMBEDDER_HELPER": "1",
		"XDG_CONFIG_HOME":              filepath.Join(xdgRoot, "config"),
		"XDG_DATA_HOME":                filepath.Join(xdgRoot, "data"),
		"XDG_CACHE_HOME":               filepath.Join(xdgRoot, "cache"),
	}
	var baseEnv []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if _, exists := overrides[key]; exists {
			continue
		}
		baseEnv = append(baseEnv, kv)
	}
	for k, v := range overrides {
		baseEnv = append(baseEnv, k+"="+v)
	}

	runMyCLI := func(args ...string) (string, string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmdArgs := append([]string{"--"}, args...)
		cmd := exec.CommandContext(ctx, testBin, cmdArgs...) // #nosec G204 -- invokes current test executable in embedder helper mode
		cmd.Dir = findProjectRoot()
		cmd.Env = baseEnv

		var outBuf, errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		require.NoError(t, cmd.Run(), "args=%v stdout=%s stderr=%s", args, outBuf.String(), errBuf.String())
		return outBuf.String(), errBuf.String()
	}

	helpOut, _ := runMyCLI("plugins", "update", "--help")
	assert.Contains(t, helpOut, "mycli plugins update --all")
	assert.NotContains(t, helpOut, "scafctl plugins update")

	stdout, stderr := runMyCLI("plugins", "update", "exec", "--platform", "linux/amd64")
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "All plugins are up to date.")
}
