// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func closedLoopbackAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func setupOfflineXDGEnv(t *testing.T, appName, catalogHost string) map[string]string {
	t.Helper()
	xdgRoot := t.TempDir()
	cfgHome := filepath.Join(xdgRoot, "config")
	dataHome := filepath.Join(xdgRoot, "data")
	cacheHome := filepath.Join(xdgRoot, "cache")

	appCfgDir := filepath.Join(cfgHome, appName)
	require.NoError(t, os.MkdirAll(appCfgDir, 0o700))
	require.NoError(t, os.MkdirAll(dataHome, 0o700))
	require.NoError(t, os.MkdirAll(cacheHome, 0o700))

	cfgYAML := "settings:\n  disableOfficialCatalog: true\n"
	if catalogHost != "" {
		cfgYAML += fmt.Sprintf(
			"  defaultCatalog: offline\ncatalogs:\n  - name: offline\n    type: oci\n    url: oci://%s/plugins\n    insecure: true\n",
			catalogHost,
		)
	}
	require.NoError(t, os.WriteFile(filepath.Join(appCfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	return map[string]string{
		"XDG_CONFIG_HOME": cfgHome,
		"XDG_DATA_HOME":   dataHome,
		"XDG_CACHE_HOME":  cacheHome,
	}
}

func TestCLI_ColdOfflineFetchAndSetupExitCodes(t *testing.T) {
	t.Parallel()

	closedAddr := closedLoopbackAddr(t)

	solWithExecPlugin := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cold-fetch-demo
  version: 1.0.0
bundle:
  plugins:
    - name: custom-exec
      kind: provider
      version: "^1.0.0"
spec:
  resolvers:
    out:
      resolve:
        with:
          - provider: custom-exec
            inputs:
              command: "echo hi"
`

	actionsWithPlugin := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cold-action-demo
  version: 1.0.0
bundle:
  plugins:
    - name: custom-exec
      kind: provider
      version: "^1.0.0"
spec:
  workflow:
    actions:
      build:
        provider: custom-exec
        inputs:
          command: "echo build"
`

	t.Run("cold offline run solution exits CatalogError with install hint", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solWithExecPlugin), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "unreachable")
		assert.Contains(t, stderr, "run 'scafctl plugins install' while online")
		assert.NotContains(t, stderr, "no published version")
		assert.NotContains(t, stderr, "not found in any catalog")
	})

	t.Run("cold offline run action exits CatalogError with install hint", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		actPath := filepath.Join(dir, "actions.yaml")
		require.NoError(t, os.WriteFile(actPath, []byte(actionsWithPlugin), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "action", "build", "-f", actPath)
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "unreachable")
		assert.Contains(t, stderr, "run 'scafctl plugins install' while online")
		assert.NotContains(t, stderr, "no published version")
	})

	t.Run("cold offline run provider exec exits CatalogError and preserves fetch error", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)

		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "provider", "exec", "command=echo hi")
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "unreachable")
		assert.Contains(t, stderr, "run 'scafctl plugins install' while online")
		assert.NotContains(t, stderr, `provider "exec" not found`)
	})

	t.Run("cold offline plugins install exits CatalogError", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solWithExecPlugin), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "plugins", "install", "-f", solPath)
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "unreachable")
		assert.Contains(t, stderr, "run 'scafctl plugins install' while online")
	})

	t.Run("proxy or gateway 503 on cold cache exits CatalogError as unreachable", func(t *testing.T) {
		t.Parallel()
		gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(gw.Close)
		gwHost := strings.TrimPrefix(gw.URL, "http://")

		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solWithExecPlugin), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", gwHost)
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "unreachable")
		assert.Contains(t, stderr, "run 'scafctl plugins install' while online")
	})

	t.Run("undeclared external provider exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		undeclared := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: undeclared-demo
  version: 1.0.0
spec:
  resolvers:
    out:
      resolve:
        with:
          - provider: exec
            inputs:
              command: "echo hi"
`
		require.NoError(t, os.WriteFile(solPath, []byte(undeclared), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", "")
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.InvalidInput, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "bundle.plugins")
	})

	t.Run("unknown provider in run provider exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := setupOfflineXDGEnv(t, "scafctl", "")

		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "provider", "unknown-provider-xyz")
		assert.Equal(t, exitcode.InvalidInput, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "not found")
	})

	t.Run("missing solution file exits FileNotFound across commands", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		missing := filepath.Join(dir, "missing.yaml")
		env := setupOfflineXDGEnv(t, "scafctl", "")

		for _, args := range [][]string{
			{"run", "solution", "-f", missing},
			{"plugins", "install", "-f", missing},
			{"lint", "-f", missing},
			{"validate", "solution", "-f", missing},
		} {
			_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, args...)
			assert.Equal(t, exitcode.FileNotFound, code, "args=%v stderr=%s", args, stderr)
		}
	})

	t.Run("unconfigured source registry exits ConfigError", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		unconfigured := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unconfigured-demo
  version: 1.0.0
bundle:
  plugins:
    - name: custom-exec
      kind: provider
      version: "^1.0.0"
      source:
        registry: registry.unconfigured.invalid/plugins
spec:
  resolvers:
    out:
      resolve:
        with:
          - provider: custom-exec
`
		require.NoError(t, os.WriteFile(solPath, []byte(unconfigured), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", "")
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.ConfigError, code, "stderr: %s", stderr)

		_, stderrInstall, codeInstall := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "plugins", "install", "-f", solPath)
		assert.Equal(t, exitcode.ConfigError, codeInstall, "stderr: %s", stderrInstall)
	})

	t.Run("embedder mycli offline hint uses embedder binary name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solWithExecPlugin), 0o600))

		env := setupOfflineXDGEnv(t, "mycli", closedAddr)
		_, stderr, code := runEmbedderMyCLI(t, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.CatalogError, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "run 'mycli plugins install' while online")
		assert.NotContains(t, stderr, "scafctl plugins install")
	})

	t.Run("cold offline lint and validate solution catalog ref exit CatalogError", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)

		_, stderrLint, codeLint := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "lint", "my-app@1.0.0", "-o", "quiet")
		assert.Equal(t, exitcode.CatalogError, codeLint, "stderr: %s", stderrLint)

		_, stderrVal, codeVal := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "validate", "solution", "my-app@1.0.0", "-o", "quiet")
		assert.Equal(t, exitcode.CatalogError, codeVal, "stderr: %s", stderrVal)
	})

	t.Run("malformed plugin version selector in plugins install exits InvalidInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := setupOfflineXDGEnv(t, "scafctl", closedAddr)

		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "plugins", "install", "exec@not-a-version")
		assert.Equal(t, exitcode.InvalidInput, code, "stderr: %s", stderr)
	})

	t.Run("malformed and stale solution.lock exit InvalidInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		lockPath := filepath.Join(dir, "solution.lock")
		require.NoError(t, os.WriteFile(solPath, []byte(solWithExecPlugin), 0o600))
		require.NoError(t, os.WriteFile(lockPath, []byte("version: ["), 0o600))

		env := setupOfflineXDGEnv(t, "scafctl", "")
		_, stderrMalformed, codeMalformed := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath)
		assert.Equal(t, exitcode.InvalidInput, codeMalformed, "stderr: %s", stderrMalformed)

		staleLock := `version: 1
plugins:
  - name: custom-exec
    kind: provider
    version: 0.2.0
    constraint: "^0.2.0"
    digest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
    resolvedFrom: local
`
		require.NoError(t, os.WriteFile(lockPath, []byte(staleLock), 0o600))
		for _, mode := range []string{"strict", "constrained"} {
			_, stderrStale, codeStale := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", "-f", solPath, "--lock-mode", mode)
			assert.Equal(t, exitcode.InvalidInput, codeStale, "mode=%s stderr: %s", mode, stderrStale)
		}
	})

	t.Run("URL solution fetch failure exits CatalogError while 404 exits FileNotFound", func(t *testing.T) {
		t.Parallel()
		srv503 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv503.Close)

		srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv404.Close)

		dir := t.TempDir()
		env := setupOfflineXDGEnv(t, "scafctl", "")
		cfgPath := filepath.Join(env["XDG_CONFIG_HOME"], "scafctl", "config.yaml")
		cfgYAML := "settings:\n  disableOfficialCatalog: true\nhttpClient:\n  retryMax: 0\n  allowedPrivateCIDRs:\n    - 127.0.0.0/8\n    - ::1/128\n"
		require.NoError(t, os.WriteFile(cfgPath, []byte(cfgYAML), 0o600))

		urlDialFail := "http://" + closedAddr + "/solution.yaml"
		for _, targetURL := range []string{srv503.URL + "/solution.yaml", urlDialFail} {
			_, stderrRun, codeRun := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "run", "solution", targetURL)
			assert.Equal(t, exitcode.CatalogError, codeRun, "url=%s stderr=%s", targetURL, stderrRun)

			_, stderrLint, codeLint := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "lint", targetURL, "-o", "quiet")
			assert.Equal(t, exitcode.CatalogError, codeLint, "url=%s stderr=%s", targetURL, stderrLint)

			_, stderrVal, codeVal := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "validate", "solution", targetURL, "-o", "quiet")
			assert.Equal(t, exitcode.CatalogError, codeVal, "url=%s stderr=%s", targetURL, stderrVal)
		}

		url404 := srv404.URL + "/missing.yaml"
		_, stderr404, code404 := runBinaryIsolatedEnv(t, binaryPath, dir, nil, env, "lint", url404, "-o", "quiet")
		assert.Equal(t, exitcode.FileNotFound, code404, "stderr=%s", stderr404)
	})
}
