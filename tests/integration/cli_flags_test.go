// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_GlobalDebugCapitalD(t *testing.T) {
	t.Parallel()

	// Exercise global -D (--debug) alongside subcommand-local -d (--data) in one invocation.
	stdout, stderr, exitCode := runScafctl(t, "-D", "eval", "cel", "--expression", "int(_.a) + 1", "-d", `{"a": 41}`)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "42\n", stdout)
	assert.NotEmpty(t, stderr, "-D should emit debug logs to stderr")
}

func TestIntegration_NewSolution_PositionalNameAndShorthandD(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "my-deploy.yaml")

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "new", "solution", "my-deploy", "-d", "Deploy to Kubernetes", "--output-file", outFile)
	assert.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: my-deploy")
	assert.Contains(t, string(data), "Deploy to Kubernetes")

	// Verify plural alias 'solutions' and that '-o json' is rejected rather than writing './json'.
	_, stderr, exitCode = runScafctlInDir(t, tmpDir, "new", "solutions", "alias-app", "-d", "Test", "-o", "json")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown shorthand flag: 'o'")
	assert.NoFileExists(t, filepath.Join(tmpDir, "json"))

	// Conflicting positional name and --name must fail.
	_, stderr, exitCode = runScafctlInDir(t, tmpDir, "new", "solution", "one-name", "-n", "other-name", "-d", "Conflict")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "conflicting solution names")
}

func TestIntegration_ConfigInit_OutputFileFlag(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cfgFile := filepath.Join(tmpDir, "my-config.yaml")

	stdout, stderr, exitCode := runScafctl(t, "config", "init", "--output-file", cfgFile)
	assert.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.FileExists(t, cfgFile)

	// Re-running without --force hints at --output-file.
	_, stderr, exitCode = runScafctl(t, "config", "init", "--output-file", cfgFile)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "--output-file")
}

func TestIntegration_SecretsSetFileShorthandAndExportFlags(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	secretIn := filepath.Join(tmpDir, "secret.txt")
	exportOut := filepath.Join(tmpDir, "exported.json")
	require.NoError(t, os.WriteFile(secretIn, []byte("top-secret-val"), 0o600))

	// Base64-encoded 32-byte key for the environment keyring fallback.
	env := map[string]string{
		"XDG_DATA_HOME":      tmpDir,
		"XDG_CONFIG_HOME":    tmpDir,
		"XDG_CACHE_HOME":     tmpDir,
		"SCAFCTL_SECRET_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "secrets", "set", "api-token", "-f", secretIn)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	gotSecret, stderr, exitCode := runScafctlWithEnv(t, env, "secrets", "get", "api-token")
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "top-secret-val", strings.TrimSpace(gotSecret))

	// With -q, confirmation is skipped (default false -> cancelled cleanly, exit 0),
	// proving --output-file and -o json parse and pass validation.
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "secrets", "export", "--output-file", exportOut, "-o", "json", "-q")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)

	// Invalid -o format is rejected up front before opening store or prompting.
	_, stderr, exitCode = runScafctlWithEnv(t, env, "secrets", "export", "--output-file", exportOut, "-o", "table")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unsupported output format")

	// Missing --output-file and removed -f format shorthand must fail.
	_, stderr, exitCode = runScafctlWithEnv(t, env, "secrets", "export", "-o", "json")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, `required flag(s) "output-file" not set`)

	_, stderr, exitCode = runScafctlWithEnv(t, env, "secrets", "export", "--output-file", exportOut, "-f", "yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown shorthand flag: 'f'")
}

func TestIntegration_EvalShorthands_FileAndData(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	dataFile := filepath.Join(tmpDir, "context.json")
	require.NoError(t, os.WriteFile(dataFile, []byte(`{"env": "prod", "replicas": 3}`), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "eval", "cel", "--expression", "int(_.replicas) * 2", "-f", dataFile)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "6\n", stdout)

	stdout, stderr, exitCode = runScafctl(t, "eval", "cel", "--expression", "_.active", "-d", `{"active": true}`)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "true\n", stdout)

	stdout, stderr, exitCode = runScafctl(t, "eval", "template", "-t", "{{ .env }}-{{ .replicas }}", "-f", dataFile)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "prod-3\n", stdout)

	stdout, stderr, exitCode = runScafctl(t, "eval", "template", "-t", "{{ .region }}", "-d", `{"region": "us-east-1"}`)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Equal(t, "us-east-1\n", stdout)
}

func TestIntegration_StateSubcommands_FileShorthand(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	_, stderr, exitCode := runScafctl(t, "state", "set", "-f", stateFile, "--key", "env", "--value", "staging")
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	stdout, stderr, exitCode := runScafctl(t, "state", "get", "-f", stateFile, "--key", "env", "-o", "json")
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "staging")

	stdout, stderr, exitCode = runScafctl(t, "state", "show", "-f", stateFile, "-o", "json")
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "staging")

	// Mixing -f and --path must fail.
	_, stderr, exitCode = runScafctl(t, "state", "show", "-f", stateFile, "--path", stateFile)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "[file path]")
}

func TestIntegration_CatalogPushRootWithCatalogFlag(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Scaffold and package a solution into the isolated local catalog.
	solFile := filepath.Join(tmpDir, "solution.yaml")
	_, stderr, exitCode := runScafctlWithEnv(t, env, "new", "solution", "push-demo", "-d", "Push demo", "--version", "1.0.0", "--output-file", solFile)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	_, stderr, exitCode = runScafctlWithEnv(t, env, "package", "solution", "-f", solFile)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	// Dry-run root catalog push with short reference + -c, including --as rename.
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "push", "push-demo@1.0.0", "-c", "ghcr.io/example-org/catalog", "--as", "renamed-demo@2.0.0", "--dry-run")
	require.Equal(t, 0, exitCode, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout+stderr, "ghcr.io/example-org/catalog/solutions/renamed-demo")
	assert.Contains(t, stdout+stderr, "--as: would publish as renamed-demo@2.0.0")

	// Package a provider plugin under the same name to trigger multi-kind disambiguation.
	fakeBin := filepath.Join(tmpDir, "fake-plugin")
	require.NoError(t, os.WriteFile(fakeBin, []byte("binary"), 0o600))
	plat := runtime.GOOS + "/" + runtime.GOARCH + "=" + fakeBin
	_, stderr, exitCode = runScafctlWithEnv(t, env, "package", "plugin", "--name", "push-demo", "--kind", "provider", "--version", "1.0.0", "--platform", plat)
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	stdout, stderr, exitCode = runScafctlWithEnv(t, env, "catalog", "push", "push-demo@1.0.0", "-c", "ghcr.io/example-org/catalog", "--dry-run")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "specify kind:")
	assert.Contains(t, stdout+stderr, "<solution|provider>")

	// Full remote reference combined with -c is rejected.
	stdout, stderr, exitCode = runScafctlWithEnv(t, env, "catalog", "push", "ghcr.io/example-org/catalog/solutions/push-demo@1.0.0", "-c", "ghcr.io/example-org/catalog", "--dry-run")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "cannot use --catalog with a full remote reference")

	// Help text references canonical 'package' verb.
	helpOut, _, _ := runScafctl(t, "catalog", "push", "--help")
	assert.Contains(t, helpOut, "package")
	assert.NotContains(t, helpOut, "scafctl build")

	pluginsHelp, _, _ := runScafctl(t, "plugins", "list", "--help")
	assert.Contains(t, pluginsHelp, "package plugin")
	assert.NotContains(t, pluginsHelp, "build plugin")
}
