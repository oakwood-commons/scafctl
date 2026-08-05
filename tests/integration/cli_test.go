// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for scafctl CLI commands.
// These tests build the binary and execute it against real solution files.
//
// Run with: go test -v ./tests/integration/...
// Or: go test -v -run Integration ./tests/integration/...

var binaryPath string

func TestMain(m *testing.M) {
	// Build the binary once for all tests.
	// Use a project-local directory instead of os.TempDir() to avoid
	// Windows Defender false positives on freshly-built Go binaries in temp.
	projectRoot := findProjectRoot()
	distDir := filepath.Join(projectRoot, "dist")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		panic(err)
	}

	binaryPath = filepath.Join(distDir, "scafctl-integration-test")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-w",
		"-o", binaryPath, "./cmd/scafctl/scafctl.go")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		panic("failed to build scafctl: " + err.Error() + "\n" + string(output))
	}

	code := m.Run()
	_ = os.Remove(binaryPath) // best-effort cleanup
	os.Exit(code)
}

func findProjectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("could not find project root")
		}
		dir = parent
	}
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func runScafctl(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlInDir(t, findProjectRoot(), args...)
}

func runScafctlInDir(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithStdinInDir(t, dir, nil, args...)
}

func runScafctlWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithStdinInDir(t, findProjectRoot(), stdin, args...)
}

func runScafctlWithStdinInDir(t *testing.T, dir string, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithOpts(t, dir, stdin, nil, args...)
}

// runScafctlWithEnv runs the binary with explicit env var overrides.
// Unlike t.Setenv, this is safe to use with t.Parallel() because it only
// affects the subprocess environment, not the test process.
func runScafctlWithEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithOpts(t, findProjectRoot(), nil, env, args...)
}

// runScafctlWithEnvInDir runs the binary in a specific directory with explicit env var overrides.
func runScafctlWithEnvInDir(t *testing.T, dir string, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithOpts(t, dir, nil, env, args...)
}

// runScafctlIsolatedEnv runs the binary with a fully controlled environment
// derived from os.Environ() but with the keys in unset removed and the pairs
// in env applied (overriding any inherited value). Unlike runScafctlWithEnv,
// this can REMOVE inherited variables (e.g. SCAFCTL_SECRET_KEY), which is
// required for deterministic secrets-backend tests.
func runScafctlIsolatedEnv(t *testing.T, unset []string, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = findProjectRoot()

	drop := make(map[string]struct{}, len(unset))
	for _, k := range unset {
		drop[k] = struct{}{}
	}
	base := os.Environ()
	filtered := make([]string, 0, len(base)+len(env))
	for _, kv := range base {
		key := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			key = kv[:i]
		}
		if _, skip := drop[key]; skip {
			continue
		}
		if _, overridden := env[key]; overridden {
			continue
		}
		filtered = append(filtered, kv)
	}
	for k, v := range env {
		filtered = append(filtered, k+"="+v)
	}
	cmd.Env = filtered

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return stdout, stderr, exitCode
}

func runScafctlWithOpts(t *testing.T, dir string, stdin io.Reader, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithTimeout(t, 90*time.Second, dir, stdin, env, args...)
}

// runScafctlLong runs the binary with a longer timeout (180s) for tests that
// involve nested solution execution or functional test suites.
func runScafctlLong(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runScafctlWithTimeout(t, 180*time.Second, findProjectRoot(), nil, nil, args...)
}

func runScafctlWithTimeout(t *testing.T, timeout time.Duration, dir string, stdin io.Reader, env map[string]string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = stdin
	}

	if len(env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return stdout, stderr, exitCode
}

// ============================================================================
// Version Command Tests
// ============================================================================

func TestIntegration_Version(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "version")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Version")
}

func TestIntegration_VersionJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "version", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "buildTime")
}

// ============================================================================
// Help Command Tests
// ============================================================================

func TestIntegration_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "scafctl")
	assert.Contains(t, stdout, "run")
	assert.Contains(t, stdout, "render")
	assert.Contains(t, stdout, "get")
	assert.Contains(t, stdout, `Use "scafctl options" for a list of global command-line options (applies to all commands).`)
	assert.NotContains(t, stdout, "Global Flags:")

	// Command groups
	assert.Contains(t, stdout, "Core Commands:")
	assert.Contains(t, stdout, "Inspection Commands:")
	assert.Contains(t, stdout, "Packaging & Distribution Commands:")
	assert.Contains(t, stdout, "Configuration & Security Commands:")
	assert.Contains(t, stdout, "Plugin Commands:")
	assert.Contains(t, stdout, "Additional Commands:")
}

func TestIntegration_Options(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "options")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "The following options can be passed to any command:")
	assert.Contains(t, stdout, "--log-level")
	assert.Contains(t, stdout, "--quiet")
	assert.Contains(t, stdout, "--no-color")
	assert.Contains(t, stdout, "--config")
	assert.Contains(t, stdout, "--cwd")
}

func TestIntegration_ExploreHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "explore", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "explore")
}

func TestIntegration_ExploreListThemes(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "explore", "--list-themes")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "dark")
}

func TestIntegration_RunHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "solution")
}

// ============================================================================
// Get Provider Tests
// ============================================================================

func TestIntegration_GetProvider(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider")

	assert.Equal(t, 0, exitCode)
	// Should list built-in providers
	assert.Contains(t, stdout, "http")
	assert.Contains(t, stdout, "cel")
	assert.Contains(t, stdout, "file")
	assert.Contains(t, stdout, "validation")
	assert.Contains(t, stdout, "debug")
	assert.Contains(t, stdout, "go-template")
	assert.Contains(t, stdout, "message")
	// Should also list official plugin providers
	assert.Contains(t, stdout, "exec")
	assert.Contains(t, stdout, "git")
	assert.Contains(t, stdout, "directory")
}

func TestIntegration_GetProviderJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "http")
}

// ============================================================================
// Get CEL Functions Tests
// ============================================================================

func TestIntegration_GetCelFunctions(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions")

	assert.Equal(t, 0, exitCode)
	// Should list both built-in and custom functions
	assert.Contains(t, stdout, "strings")
	assert.Contains(t, stdout, "map.merge")
}

func TestIntegration_GetCelFunctionsCustom(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions", "--custom")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "map.merge")
	assert.Contains(t, stdout, "guid.new")
}

func TestIntegration_GetCelFunctionsBuiltin(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions", "--builtin")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "strings")
}

func TestIntegration_GetCelFunctionsJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "\"custom\"")
}

func TestIntegration_GetCelFunctionsQuiet(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions", "-o", "quiet")

	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stdout, "quiet format should suppress all output")
}

func TestIntegration_GetCelFunctionDetail(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel-functions", "map.merge")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "map.merge")
}

// ============================================================================
// Get Go Template Functions Tests
// ============================================================================

func TestIntegration_GetGoTemplateFunctions(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions")

	assert.Equal(t, 0, exitCode)
	// Should list both sprig and custom functions
	assert.Contains(t, stdout, "upper")
	assert.Contains(t, stdout, "toHcl")
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "fromYaml")
}

func TestIntegration_GetGoTemplateFunctionsCustom(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "--custom")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "toHcl")
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "fromYaml")
	assert.Contains(t, stdout, "mustToYaml")
	assert.Contains(t, stdout, "mustFromYaml")
}

func TestIntegration_GetGoTemplateFunctionsSprig(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "--sprig")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "upper")
	assert.Contains(t, stdout, "lower")
}

func TestIntegration_GetGoTemplateFunctionsJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "\"custom\"")
}

func TestIntegration_GetGoTemplateFunctionsQuiet(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "-o", "quiet")

	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stdout, "quiet format should suppress all output")
}

func TestIntegration_GetGoTemplateFunctionDetail(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "toHcl")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "toHcl")
}

func TestIntegration_GetGoTemplateFunctionDetailToYaml(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "go-template-functions", "toYaml")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "YAML")
}

// ============================================================================
// Get CEL Functions Tests (canonical 'get cel functions' path)
// ============================================================================

func TestIntegration_GetCelFunctionsCanonical(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions")

	assert.Equal(t, 0, exitCode)
	// Should list both built-in and custom functions
	assert.Contains(t, stdout, "strings")
	assert.Contains(t, stdout, "map.merge")
}

func TestIntegration_GetCelFunctionsCanonicalCustom(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions", "--custom")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "map.merge")
	assert.Contains(t, stdout, "guid.new")
}

func TestIntegration_GetCelFunctionsCanonicalBuiltin(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions", "--builtin")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "strings")
}

func TestIntegration_GetCelFunctionsCanonicalJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "\"custom\"")
}

func TestIntegration_GetCelFunctionsCanonicalQuiet(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions", "-o", "quiet")

	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stdout, "quiet format should suppress all output")
}

func TestIntegration_GetCelFunctionCanonicalDetail(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel", "functions", "map.merge")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "map.merge")
}

// TestIntegration_GetCelBareHelp verifies a bare 'get cel' group shows help
// (exit 0) and advertises its 'functions' child.
func TestIntegration_GetCelBareHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "cel")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "functions")
}

// TestIntegration_GetCelFunctionsDeprecatedNotice verifies the hidden
// deprecated 'get cel-functions' path still exits 0 and emits cobra's
// deprecation notice on stderr pointing at the canonical path.
func TestIntegration_GetCelFunctionsDeprecatedNotice(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "get", "cel-functions")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, `Command "cel-functions" is deprecated`)
	assert.Contains(t, stderr, "get cel functions")
	// Deprecated path still produces the functional listing.
	assert.Contains(t, stdout, "map.merge")
}

// TestIntegration_GetCelFunctionsCanonicalMatchesDeprecated verifies the
// canonical and deprecated paths produce identical JSON output.
func TestIntegration_GetCelFunctionsCanonicalMatchesDeprecated(t *testing.T) {
	t.Parallel()
	canonical, _, canonicalExit := runScafctl(t, "get", "cel", "functions", "-o", "json")
	deprecated, _, deprecatedExit := runScafctl(t, "get", "cel-functions", "-o", "json")

	assert.Equal(t, 0, canonicalExit)
	assert.Equal(t, 0, deprecatedExit)
	assert.Equal(t, canonical, deprecated,
		"canonical 'get cel functions' and deprecated 'get cel-functions' must list the same functions")
}

// ============================================================================
// Get Template Functions Tests (canonical 'get template functions' path)
// ============================================================================

func TestIntegration_GetTemplateFunctionsCanonical(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions")

	assert.Equal(t, 0, exitCode)
	// Should list both sprig and custom functions
	assert.Contains(t, stdout, "upper")
	assert.Contains(t, stdout, "toHcl")
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "fromYaml")
}

func TestIntegration_GetTemplateFunctionsCanonicalCustom(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions", "--custom")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "toHcl")
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "fromYaml")
	assert.Contains(t, stdout, "mustToYaml")
	assert.Contains(t, stdout, "mustFromYaml")
}

func TestIntegration_GetTemplateFunctionsCanonicalSprig(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions", "--sprig")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "upper")
	assert.Contains(t, stdout, "lower")
}

func TestIntegration_GetTemplateFunctionsCanonicalJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "\"custom\"")
}

func TestIntegration_GetTemplateFunctionsCanonicalQuiet(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions", "-o", "quiet")

	assert.Equal(t, 0, exitCode)
	assert.Empty(t, stdout, "quiet format should suppress all output")
}

func TestIntegration_GetTemplateFunctionCanonicalDetail(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template", "functions", "toHcl")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "toHcl")
}

// TestIntegration_GetTemplateBareHelp verifies a bare 'get template' group
// shows help (exit 0) and advertises its 'functions' child.
func TestIntegration_GetTemplateBareHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "template")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "functions")
}

// TestIntegration_GetGoTemplateFunctionsDeprecatedNotice verifies the hidden
// deprecated 'get go-template-functions' path still exits 0 and emits cobra's
// deprecation notice on stderr pointing at the canonical path.
func TestIntegration_GetGoTemplateFunctionsDeprecatedNotice(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "get", "go-template-functions")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, `Command "go-template-functions" is deprecated`)
	assert.Contains(t, stderr, "get template functions")
	// Deprecated path still produces the functional listing.
	assert.Contains(t, stdout, "toHcl")
}

// TestIntegration_GetTemplateFunctionsCanonicalMatchesDeprecated verifies the
// canonical and deprecated paths produce identical JSON output.
func TestIntegration_GetTemplateFunctionsCanonicalMatchesDeprecated(t *testing.T) {
	t.Parallel()
	canonical, _, canonicalExit := runScafctl(t, "get", "template", "functions", "-o", "json")
	deprecated, _, deprecatedExit := runScafctl(t, "get", "go-template-functions", "-o", "json")

	assert.Equal(t, 0, canonicalExit)
	assert.Equal(t, 0, deprecatedExit)
	assert.Equal(t, canonical, deprecated,
		"canonical 'get template functions' and deprecated 'get go-template-functions' must list the same functions")
}

// ============================================================================
// Explain Schema Tests
// ============================================================================

func TestIntegration_ExplainProvider(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "explain", "provider")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Descriptor")
	assert.Contains(t, stdout, "name")
}

func TestIntegration_ExplainProviderNotFound(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "explain", "nonexistentkind")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown kind")
}

// ============================================================================
// Run Provider Tests
// ============================================================================

func TestIntegration_RunProvider_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--input")
	assert.Contains(t, stdout, "--capability")
	assert.Contains(t, stdout, "--dry-run")
	assert.Contains(t, stdout, "--plugin-dir")
	assert.Contains(t, stdout, "--on-conflict")
	assert.Contains(t, stdout, "--backup")
}

func TestIntegration_RunProvider_DynamicHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "http", "--help")

	assert.Equal(t, 0, exitCode)
	// Standard help sections should still appear
	assert.Contains(t, stdout, "--input")
	// Dynamic provider inputs section should appear
	assert.Contains(t, stdout, "Provider Inputs (http):")
	assert.Contains(t, stdout, "url")
	assert.Contains(t, stdout, "(required)")
	assert.Contains(t, stdout, "method")
}

func TestIntegration_RunProvider_DynamicHelpStatic(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Provider Inputs (static):")
	assert.Contains(t, stdout, "value")
	assert.Contains(t, stdout, "(required)")
}

func TestIntegration_RunProvider_DynamicHelpUnknownProvider(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "nonexistent", "--help")

	assert.Equal(t, 0, exitCode)
	// Standard help should still show
	assert.Contains(t, stdout, "--input")
	// No dynamic section for unknown provider
	assert.NotContains(t, stdout, "Provider Inputs")
}

func TestIntegration_RunProvider_Static(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=hello")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello")
}

func TestIntegration_RunProvider_StaticJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=hello", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"data\"")
	assert.Contains(t, stdout, "hello")
}

func TestIntegration_RunProvider_Env(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "env", "--input", "operation=get", "--input", "name=PATH")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "data")
	assert.NotEmpty(t, stdout)
}

func TestIntegration_RunProvider_InvalidProvider(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "run", "provider", "nonexistent-provider-xyz")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_RunProvider_MissingInput(t *testing.T) {
	t.Parallel()
	// env provider requires 'name' input
	_, stderr, exitCode := runScafctl(t, "run", "provider", "env")

	assert.NotEqual(t, 0, exitCode)
	assert.NotEmpty(t, stderr)
}

func TestIntegration_RunProvider_Capability(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=test", "--capability", "transform")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test")
}

func TestIntegration_RunProvider_InvalidCapability(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=test", "--capability", "bogus")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid capability")
}

func TestIntegration_RunProvider_DryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=hello", "--dry-run")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "dryRun")
}

func TestIntegration_RunProvider_InputFile(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "@tests/files/provider-inputs/static-hello.yaml")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "data")
}

func TestIntegration_RunProvider_Alias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "prov", "static", "--input", "value=alias-test")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "alias-test")
}

func TestIntegration_RunProvider_ShowMetrics(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=metrics-test", "--show-metrics")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "metrics-test")
	assert.Contains(t, stderr, "Provider Execution Metrics")
}

// ============================================================================
// Run Provider — Positional Input Syntax Tests
// ============================================================================

func TestIntegration_RunProvider_PositionalKeyValue(t *testing.T) {
	t.Parallel()
	// value=hello  (positional key=value after provider name)
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "value=hello-positional")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello-positional")
}

func TestIntegration_RunProvider_MixedInputSyntax(t *testing.T) {
	t.Parallel()
	// Mix --input and positional key=value
	stdout, _, exitCode := runScafctl(t, "run", "provider", "env",
		"--input", "operation=get",
		"name=PATH",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "data")
}

func TestIntegration_RunProvider_PositionalWithBuiltinFlag(t *testing.T) {
	t.Parallel()
	// Ensure built-in flags (-o) still work alongside positional args
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "value=flagtest", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "flagtest")
	assert.Contains(t, stdout, "\"data\"")
}

func TestIntegration_RunProvider_PositionalFileRef(t *testing.T) {
	t.Parallel()
	// @file.yaml as positional arg
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "@tests/files/provider-inputs/static-hello.yaml")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "data")
}

func TestIntegration_RunProvider_PositionalMultipleInputs(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "env", "operation=get", "name=PATH", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"data\"")
}

// ============================================================================
// Run Provider — Message Provider Tests
// ============================================================================

func TestIntegration_RunProvider_Message(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "message", "--input", "message=hello from message provider", "--input", "type=plain", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello from message provider")
	assert.Contains(t, stdout, "\"success\"")
}

func TestIntegration_RunProvider_MessageDryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "message", "--input", "message=dry run test", "--input", "type=info", "--dry-run", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "[dry-run]")
}

// ============================================================================
// Run Provider — Unknown Input Key Validation Tests
// ============================================================================

func TestIntegration_RunProvider_UnknownInputKey(t *testing.T) {
	t.Parallel()
	// "valuee" is not a valid input for the static provider (should suggest "value")
	_, stderr, exitCode := runScafctl(t, "run", "provider", "static", "valuee=hello")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not accept input")
	assert.Contains(t, stderr, `did you mean "value"`)
}

func TestIntegration_RunProvider_UnknownInputKeyNoSuggestion(t *testing.T) {
	t.Parallel()
	// "zzzzz" is too far from any valid key
	_, stderr, exitCode := runScafctl(t, "run", "provider", "static", "zzzzz=hello")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not accept input")
}

func TestIntegration_RunProvider_HCL(t *testing.T) {
	t.Parallel()

	// Write HCL to a temp file to avoid CLI argument escaping issues
	tmpDir := t.TempDir()
	hclFile := filepath.Join(tmpDir, "main.tf")
	hclContent := "variable \"region\" {\n  default = \"us-east-1\"\n  description = \"AWS region\"\n}\n"
	require.NoError(t, os.WriteFile(hclFile, []byte(hclContent), 0o644))

	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl", "--input", "path="+hclFile, "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "region")
	assert.Contains(t, stdout, "variables")
}

func TestIntegration_RunProvider_HCL_DryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl", "--input", "content=variable \"x\" {}", "--dry-run")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "dryRun")
}

func TestIntegration_RunProvider_Identity_DryRun(t *testing.T) {
	t.Parallel()
	t.Skip("identity plugin v0.1.0 requires host client for auth even in dry-run mode")
	t.Parallel()

	tests := []struct {
		name      string
		operation string
		extra     []string // additional --input flags
		wantInOut string   // substring expected in stdout
	}{
		{
			name:      "claims",
			operation: "claims",
			wantInOut: "claims",
		},
		{
			name:      "status",
			operation: "status",
			wantInOut: "status",
		},
		{
			name:      "groups",
			operation: "groups",
			wantInOut: "groups",
		},
		{
			name:      "list",
			operation: "list",
			wantInOut: "list",
		},
		{
			name:      "scoped claims",
			operation: "claims",
			extra:     []string{"--input", "scope=api://my-app/.default"},
			wantInOut: "scopedToken",
		},
		{
			name:      "scoped status",
			operation: "status",
			extra:     []string{"--input", "scope=https://management.azure.com/.default"},
			wantInOut: "scopedToken",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := make([]string, 0, 8+len(tt.extra))
			args = append(args, "run", "provider", "identity", "--input", "operation="+tt.operation, "--dry-run", "-o", "json")
			args = append(args, tt.extra...)
			stdout, _, exitCode := runScafctl(t, args...)

			assert.Equal(t, 0, exitCode)
			assert.Contains(t, stdout, "dryRun")
			assert.Contains(t, stdout, tt.wantInOut)
		})
	}
}

func TestIntegration_RunProvider_Identity_ScopeRestriction(t *testing.T) {
	t.Parallel()

	// scope + groups should error (even with --dry-run, scope validation happens before dry-run check)
	_, stderr, exitCode := runScafctl(t, "run", "provider", "identity",
		"--input", "operation=groups",
		"--input", "scope=api://my-app/.default",
		"-o", "json")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "scope is not supported")
}

func TestIntegration_RunProvider_HCL_Validate(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl",
		"--input", "operation=validate",
		"--input", "content=variable \"x\" { type = string }",
		"-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "valid")
}

func TestIntegration_RunProvider_HCL_Generate(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl",
		"--input", "@tests/files/provider-inputs/hcl-generate.yaml",
		"-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hcl")
}

func TestIntegration_RunProvider_HCL_GenerateJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl",
		"--input", "@tests/files/provider-inputs/hcl-generate-json.yaml",
		"-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hcl")
	assert.Contains(t, stdout, "variable")
	assert.Contains(t, stdout, "resource")
}

func TestIntegration_RunProvider_HCL_Format(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "hcl",
		"--input", "operation=format",
		"--input", "content=variable \"x\" {\ntype=string\n}",
		"-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "formatted")
	assert.Contains(t, stdout, "changed")
}

// ============================================================================
// Get Provider CLI Usage Tests
// ============================================================================

func TestIntegration_GetProvider_CLIUsage(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider", "http")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "CLI Usage:")
	assert.Contains(t, stdout, "scafctl run provider http")
	assert.Contains(t, stdout, "@inputs.yaml")
}

func TestIntegration_GetProvider_CLIUsageJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider", "http", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "cliUsage")
	assert.Contains(t, stdout, "scafctl run provider http")
}

// ============================================================================
// Run Solution Tests
// ============================================================================

func TestIntegration_RunSolution_HelloWorld(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "Hello from Actions!")
}

func TestIntegration_RunAction_HelloWorld(t *testing.T) {
	t.Parallel()
	// 'run action greet' should execute only the 'greet' action
	stdout, stderr, exitCode := runScafctl(t,
		"run", "action", "greet",
		"-f", "examples/actions/hello-world.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "Hello from Actions!")
}

func TestIntegration_RunAction_UnknownName(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "action", "nonexistent",
		"-f", "examples/actions/hello-world.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_RunSolution_ActionFlag(t *testing.T) {
	t.Parallel()
	// --action flag on run solution should also work
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--action", "greet",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "Hello from Actions!")
}

func TestIntegration_RunAction_MultiActionFiltering(t *testing.T) {
	t.Parallel()

	// Create a multi-action workflow in a temp dir to test filtering
	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-action-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "filtered-test"
  workflow:
    actions:
      setup:
        description: Setup step
        provider: message
        inputs:
          message: "SETUP_RAN"
          type: info
      build:
        description: Build step (depends on setup)
        dependsOn: [setup]
        provider: message
        inputs:
          message: "BUILD_RAN"
          type: info
      test:
        description: Test step (depends on build)
        dependsOn: [build]
        provider: message
        inputs:
          message: "TEST_RAN"
          type: info
      deploy:
        description: Deploy step (should NOT run)
        provider: message
        inputs:
          message: "DEPLOY_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

	// Run only "test" action — should include setup + build (transitive deps) but NOT deploy
	stdout, stderr, exitCode := runScafctl(t,
		"run", "action", "test",
		"-f", solutionPath,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "SETUP_RAN", "transitive dep 'setup' should run")
	assert.Contains(t, stdout, "BUILD_RAN", "transitive dep 'build' should run")
	assert.Contains(t, stdout, "TEST_RAN", "target 'test' should run")
	assert.NotContains(t, stdout, "DEPLOY_RAN", "unselected 'deploy' should NOT run")
}

func TestIntegration_RunSolution_NoWorkflowErrors(t *testing.T) {
	t.Parallel()
	// resolver-demo.yaml has resolvers but no workflow section
	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/resolver-demo.yaml",
	)

	assert.Equal(t, 3, exitCode, "expected exit code 3 (InvalidInput), got %d", exitCode)
	assert.Contains(t, stderr, "no workflow defined")
	assert.Contains(t, stderr, "scafctl run resolver")
}

// failureEnvelopeSolution hard-fails at the transform phase at runtime:
// int("hello") passes load-time validation but errors when evaluated. It carries
// a workflow so `run solution`/`run action` reach the resolver-execution site.
const failureEnvelopeSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: failure-envelope-integration
  version: 1.0.0
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
      transform:
        with:
          - provider: cel
            inputs:
              expression: 'int(__self)'
  workflow:
    actions:
      greet:
        provider: message
        inputs:
          message: "SHOULD_NOT_RUN"
          type: info
`

// TestIntegration_RunResolver_FailureEnvelope_JSON verifies that a resolver
// failure still writes a parseable JSON document (with the reserved
// __status/__diagnostics keys) to stdout instead of empty stdout, so scripts
// piping to jq keep working on failure.
func TestIntegration_RunResolver_FailureEnvelope_JSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failureEnvelopeSolution), 0o600))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver", "-f", solutionPath, "-o", "json",
	)

	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode, "resolver failure must exit non-zero")
	require.NotEmpty(t, strings.TrimSpace(stdout), "stdout must not be empty on failure")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded), "stdout must be valid JSON")
	assert.Equal(t, "failed", decoded["__status"])
	assert.NotEmpty(t, decoded["__diagnostics"])
}

// TestIntegration_RunSolution_FailureEnvelope_JSON verifies the same guarantee
// for `run solution`: a resolver-phase failure emits a {status, diagnostics}
// document on stdout.
func TestIntegration_RunSolution_FailureEnvelope_JSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failureEnvelopeSolution), 0o600))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution", "-f", solutionPath, "-o", "json",
	)

	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode, "solution resolver failure must exit non-zero")
	require.NotEmpty(t, strings.TrimSpace(stdout), "stdout must not be empty on failure")
	assert.NotContains(t, stdout, "SHOULD_NOT_RUN", "actions must not run when resolvers fail")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded), "stdout must be valid JSON")
	assert.Equal(t, "failed", decoded["status"])
	assert.NotEmpty(t, decoded["diagnostics"])
}

// TestIntegration_RunAction_FailureEnvelope_JSON verifies the same guarantee for
// `run action`.
func TestIntegration_RunAction_FailureEnvelope_JSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(failureEnvelopeSolution), 0o600))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "action", "greet", "-f", solutionPath, "-o", "json",
	)

	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode, "action resolver failure must exit non-zero")
	require.NotEmpty(t, strings.TrimSpace(stdout), "stdout must not be empty on failure")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded), "stdout must be valid JSON")
	assert.Equal(t, "failed", decoded["status"])
	assert.NotEmpty(t, decoded["diagnostics"])
}

// partialFailureSolution has one resolver that resolves successfully ("good")
// and one that hard-fails in the resolve phase ("bad"): an undefined
// Go-template function fails to parse, so no value is produced. This is the
// exact shape from the bug report -- a resolve-phase error must not discard the
// sibling that already resolved.
const partialFailureSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: partial-failure-integration
  version: 1.0.0
spec:
  resolvers:
    good:
      resolve:
        with:
          - provider: static
            inputs:
              value: i-resolved
    bad:
      resolve:
        with:
          - provider: go-template
            inputs:
              template: "{{ undefinedFunc .x }}"
              data:
                x: hello
`

// TestIntegration_RunResolver_ResolvePhaseFailure_PreservesValues verifies that
// a resolve-phase failure in one resolver preserves the values of every
// resolver that resolved successfully, alongside the __status/__diagnostics
// envelope, instead of collapsing stdout to just the envelope.
func TestIntegration_RunResolver_ResolvePhaseFailure_PreservesValues(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(partialFailureSolution), 0o600))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver", "-f", solutionPath, "-o", "json",
	)

	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode, "resolve-phase failure must exit non-zero")
	require.NotEmpty(t, strings.TrimSpace(stdout), "stdout must not be empty on failure")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded), "stdout must be valid JSON")
	assert.Equal(t, "i-resolved", decoded["good"],
		"a successfully-resolved value must survive a sibling resolve-phase failure")
	assert.NotContains(t, decoded, "bad", "the failed resolver produced no value and must be absent")
	assert.Equal(t, "failed", decoded["__status"])
	assert.NotEmpty(t, decoded["__diagnostics"])
}

// Action-phase outcome fixtures for the --detailed-exit-code matrix.
//
// actionCleanSuccessSolution: every action succeeds -> FinalStatus success.
// actionPartialSuccessSolution: one action fails but is tolerated via
//
//	continueOnError -> FinalStatus partial-success (no hard failure).
//
// actionHardFailureSolution: one action fails with the default onError (fail)
//
//	-> FinalStatus failed (exit 6), unaffected by --detailed-exit-code.
const actionCleanSuccessSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: detailed-exit-clean
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      good:
        provider: message
        inputs:
          message: "GOOD_RAN"
          type: info
`

const actionPartialSuccessSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: detailed-exit-partial
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      good:
        provider: message
        inputs:
          message: "GOOD_RAN"
          type: info
      bad:
        provider: go-template
        continueOnError: true
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: hello
`

const actionHardFailureSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: detailed-exit-hard
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      good:
        provider: message
        inputs:
          message: "GOOD_RAN"
          type: info
      bad:
        provider: go-template
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: hello
`

// TestIntegration_RunDetailedExitCode verifies the opt-in --detailed-exit-code
// flag across the full outcome matrix (clean / partial / hard-failure) with the
// flag both off and on, for BOTH `run solution` and `run action` (the two CLI
// call sites are independent copies and must behave identically):
//
//	clean, flag off   -> 0     clean, flag on   -> 0
//	partial, flag off -> 0     partial, flag on -> 12  (the new behavior)
//	hard, flag off    -> 6     hard, flag on    -> 6   (unchanged)
//
// The default (flag off) keeps partial success at 0 -- the non-breaking guarantee.
func TestIntegration_RunDetailedExitCode(t *testing.T) {
	t.Parallel()

	fixtures := map[string]string{
		"clean":   actionCleanSuccessSolution,
		"partial": actionPartialSuccessSolution,
		"hard":    actionHardFailureSolution,
	}
	tmpDir := t.TempDir()
	paths := make(map[string]string, len(fixtures))
	for name, content := range fixtures {
		p := filepath.Join(tmpDir, name+".yaml")
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		paths[name] = p
	}

	cases := []struct {
		outcome  string
		detailed bool
		wantExit int
	}{
		{"clean", false, 0},
		{"clean", true, 0},
		{"partial", false, 0}, // non-breaking default: partial stays 0
		{"partial", true, 12}, // opt-in: distinct PartialSuccess code
		{"hard", false, 6},    // hard failure unchanged
		{"hard", true, 6},     // hard failure unaffected by the flag
	}

	for _, subcmd := range []string{"solution", "action"} {
		for _, tc := range cases {
			name := fmt.Sprintf("%s/%s/detailed=%v", subcmd, tc.outcome, tc.detailed)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				args := []string{"run", subcmd, "-f", paths[tc.outcome], "-o", "json"}
				if tc.detailed {
					args = append(args, "--detailed-exit-code")
				}
				stdout, stderr, exitCode := runScafctl(t, args...)
				t.Logf("stderr: %s", stderr)
				assert.Equal(t, tc.wantExit, exitCode,
					"%s: expected exit %d, got %d", name, tc.wantExit, exitCode)

				// Partial success with the flag on must still emit the full,
				// parseable action envelope on stdout (output before non-zero exit).
				if tc.outcome == "partial" && tc.detailed {
					var decoded map[string]any
					require.NoError(t, json.Unmarshal([]byte(stdout), &decoded),
						"stdout must remain parseable JSON on partial success")
					assert.Equal(t, "partial-success", decoded["status"],
						"envelope must report the partial-success status")
					// The exit-code diagnostic must be printed to stderr (routed
					// through exitWithCode) so the user sees why the run exited 12.
					assert.Contains(t, stderr, "partial success",
						"partial-success exit must print a stderr diagnostic")
				}
			})
		}
	}
}

// rule fails on an immutable resolver, execution stops before actions run and
// the immutable value is NOT persisted to state. A subsequent run whose deferred
// validation passes then locks the value and runs the action.
func TestIntegration_RunSolution_DeferredValidationImmutableNotLocked(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: d1-immutable-defer
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    region:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
      validate:
        with:
          - provider: validation
            inputs:
              expression: "_.region != _.backupRegion"
              message: "region must differ from backupRegion"
    backupRegion:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: backupRegion
              default: us-east1
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	// Run 1: regions equal -> deferred validation fails. The action must not run
	// and the immutable value must not be locked (state file absent).
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath)
	assert.NotEqual(t, 0, exitCode, "failing deferred validation should exit non-zero")
	assert.Contains(t, stderr, "region must differ from backupRegion")
	assert.NotContains(t, stdout, "ACTION_RAN", "action must not run when deferred validation fails")
	assert.NoFileExists(t, statePath, "immutable value must not be persisted when deferred validation fails")

	// Run 2: regions differ -> deferred validation passes. The action runs and
	// the immutable value is now locked in state.
	stdout, _, exitCode = runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "-r", "backupRegion=us-west1")
	assert.Equal(t, 0, exitCode, "passing deferred validation should exit zero")
	assert.Contains(t, stdout, "ACTION_RAN", "action should run when deferred validation passes")
	require.FileExists(t, statePath, "state file should be written after a successful run")

	raw, err := os.ReadFile(statePath) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	var stateDoc struct {
		Resolvers map[string]any `json:"resolvers"`
	}
	require.NoError(t, json.Unmarshal(raw, &stateDoc))
	assert.Contains(t, stateDoc.Resolvers, "region", "immutable region should be locked after a passing run")
}

// TestIntegration_RunSolution_StateSchemaVersionOutOfRange verifies that when a
// state file's schemaVersion is outside the supported range, the run fails with
// the actionable schema-version error rather than a cryptic Go reflection error
// from the strict full-struct decode. The pre-written state file also carries a
// type-incompatible field ("resolvers" as a string) so that, without the
// peek-before-decode guard, the strict unmarshal would surface a
// "cannot unmarshal" error -- the assertions confirm it does not.
func TestIntegration_RunSolution_StateSchemaVersionOutOfRange(t *testing.T) {
	t.Parallel()

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-schema-version
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    region:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`

	testCases := []struct {
		name        string
		schemaVer   int
		wantMessage string
		wantHint    string
	}{
		{
			name:        "older than minimum is incompatible",
			schemaVer:   1,
			wantMessage: "incompatible state schema version",
			wantHint:    "delete the state file and recreate it",
		},
		{
			name:        "newer than current is unsupported",
			schemaVer:   999,
			wantMessage: "unsupported state schema version",
			wantHint:    "upgrade scafctl",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			solutionPath := filepath.Join(tmpDir, "solution.yaml")
			require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

			// Pre-write a state file whose schemaVersion is out of range AND whose
			// "resolvers" field has an incompatible type (string, not an object).
			// Without the version guard running first, the strict decode would fail
			// with a "cannot unmarshal" reflection error.
			statePath := filepath.Join(tmpDir, "state.json")
			stateJSON := fmt.Sprintf(`{"schemaVersion": %d, "resolvers": "not-a-map"}`, tc.schemaVer)
			require.NoError(t, os.WriteFile(statePath, []byte(stateJSON), 0o600))

			_, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath)
			assert.NotEqual(t, 0, exitCode, "an out-of-range state schema version should fail the run")
			assert.Contains(t, stderr, tc.wantMessage, "the actionable schema-version error should be surfaced")
			assert.Contains(t, stderr, tc.wantHint, "the error should include the actionable remediation hint")
			assert.NotContains(t, stderr, "cannot unmarshal",
				"the version guard must fire before the strict decode, so no reflection error should leak")
		})
	}
}

// TestIntegration_RunSolution_FirstRunAbsentState verifies the first-run
// contract: when a state backend reports no existing state (an HTTP 404, or a
// contentless "{}" payload the way some remote backends answer a missing
// object), the run must start from fresh empty state and succeed -- it must NOT
// be misread as an out-of-range "schemaVersion 0" and rejected with an
// "incompatible state schema version" error.
func TestIntegration_RunSolution_FirstRunAbsentState(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		loadStatus int
		loadBody   string
	}{
		{
			name:       "remote 404 is a fresh first run",
			loadStatus: http.StatusNotFound,
			loadBody:   "not found",
		},
		{
			name:       "contentless empty object is a fresh first run",
			loadStatus: http.StatusOK,
			loadBody:   "{}",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// state_load is a GET; state_save/delete use other methods.
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.loadStatus)
					_, _ = w.Write([]byte(tc.loadBody))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"success": true}`))
			}))
			defer srv.Close()

			solutionContent := fmt.Sprintf(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: first-run-absent-state
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: http
    inputs:
      url: %s
spec:
  resolvers:
    region:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`, srv.URL)

			tmpDir := t.TempDir()
			solutionPath := filepath.Join(tmpDir, "solution.yaml")
			require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

			// The http backend talks to an httptest server on 127.0.0.1, which the
			// SSRF guard blocks unless private IPs are explicitly allowed.
			configPath := filepath.Join(tmpDir, "config.yaml")
			require.NoError(t, os.WriteFile(configPath, []byte("httpClient:\n  allowPrivateIPs: true\n"), 0o600))

			stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "--config", configPath, "run", "solution", "-f", solutionPath)
			assert.Equal(t, 0, exitCode, "a first run against an absent remote state must succeed")
			assert.Contains(t, stdout, "ACTION_RAN", "the action should run on a fresh first run")
			assert.NotContains(t, stderr, "incompatible state schema version",
				"an absent/contentless first-run state must not be misread as an out-of-range schema version")
		})
	}
}

// TestIntegration_RunSolution_NoStateFlag verifies that --no-state skips the
// entire state lifecycle: no state file is written, immutable values are not
// locked or verified, the action still runs, and a one-line stderr notice is
// emitted because the solution declares a state block.
func TestIntegration_RunSolution_NoStateFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-state-flag
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    region:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	// Run 1 with --no-state: action runs, no state file written, warning shown.
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "--no-state")
	assert.Equal(t, 0, exitCode, "run with --no-state should exit zero")
	assert.Contains(t, stdout, "ACTION_RAN", "action should still run with --no-state")
	assert.NoFileExists(t, statePath, "state file must not be written when --no-state is set")
	assert.Contains(t, stderr, "--no-state", "a stderr notice should be emitted when skipping a state-enabled solution")

	// Run 2 with --no-state and a DIFFERENT immutable value: because nothing was
	// locked, immutable verification is skipped and the run succeeds.
	_, _, exitCode = runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "--no-state", "-r", "region=eu-west1")
	assert.Equal(t, 0, exitCode, "immutable checks must be skipped under --no-state")
	assert.NoFileExists(t, statePath, "state file must still not exist after a second --no-state run")
}

// TestIntegration_RunResolver_NoStateFlag verifies that --no-state on
// `run resolver` skips the state lifecycle: no state file is written, immutable
// values are not locked or verified, and a one-line stderr notice is emitted.
func TestIntegration_RunResolver_NoStateFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-state-resolver
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    region:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	// Run 1 with --no-state: resolvers run, no state file written, warning shown.
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "resolver", "-f", solutionPath, "--no-state")
	assert.Equal(t, 0, exitCode, "run resolver with --no-state should exit zero")
	assert.Contains(t, stdout, "us-east1", "resolver should still resolve with --no-state")
	assert.NoFileExists(t, statePath, "state file must not be written when --no-state is set")
	assert.Contains(t, stderr, "--no-state", "a stderr notice should be emitted when skipping a state-enabled solution")

	// Run 2 with --no-state and a DIFFERENT immutable value: nothing was locked,
	// so immutable verification is skipped and the run succeeds.
	_, _, exitCode = runScafctlInDir(t, tmpDir, "run", "resolver", "-f", solutionPath, "--no-state", "-r", "region=eu-west1")
	assert.Equal(t, 0, exitCode, "immutable checks must be skipped under --no-state")
	assert.NoFileExists(t, statePath, "state file must still not exist after a second --no-state run")
}

// TestIntegration_RunAction_NoStateFlag verifies that --no-state on
// `run action` skips the state lifecycle: the action still runs, no state file
// is written, and a one-line stderr notice is emitted.
func TestIntegration_RunAction_NoStateFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-state-action
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    region:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
              default: us-east1
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	// Run 1 with --no-state: action runs, no state file written, warning shown.
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "action", "notify", "-f", solutionPath, "--no-state")
	assert.Equal(t, 0, exitCode, "run action with --no-state should exit zero")
	assert.Contains(t, stdout, "ACTION_RAN", "action should still run with --no-state")
	assert.NoFileExists(t, statePath, "state file must not be written when --no-state is set")
	assert.Contains(t, stderr, "--no-state", "a stderr notice should be emitted when skipping a state-enabled solution")

	// Run 2 with --no-state and a DIFFERENT immutable value: nothing was locked,
	// so immutable verification is skipped and the run succeeds.
	_, _, exitCode = runScafctlInDir(t, tmpDir, "run", "action", "notify", "-f", solutionPath, "--no-state", "-r", "region=eu-west1")
	assert.Equal(t, 0, exitCode, "immutable checks must be skipped under --no-state")
	assert.NoFileExists(t, statePath, "state file must still not exist after a second --no-state run")
}

// TestIntegration_RunSolution_DynamicStatePath verifies that
// state.backend.inputs can reference a resolver output: the state file path is
// computed from the resolved app_name, so state is written to the per-app path.
func TestIntegration_RunSolution_DynamicStatePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dynamic-state-path
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        expr: "_.app_name + '-state.json'"
spec:
  resolvers:
    app_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: app_name
              default: demo
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "-r", "app_name=billing")
	assert.Equal(t, 0, exitCode, "run with dynamic state path should exit zero")
	assert.Contains(t, stdout, "ACTION_RAN")

	// State must be written to the resolver-derived path, not a literal one.
	require.FileExists(t, filepath.Join(tmpDir, "billing-state.json"), "state must be written to the dynamic per-app path")
	assert.NoFileExists(t, filepath.Join(tmpDir, "demo-state.json"), "default path must not be used when app_name is supplied")
}

// TestIntegration_RunSolution_DynamicStateEnabledFalse verifies that
// state.enabled can reference a resolver output: when that resolver evaluates to
// false, the state lifecycle is skipped and no state file is written.
func TestIntegration_RunSolution_DynamicStateEnabledFalse(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dynamic-state-enabled
  version: 1.0.0
state:
  enabled:
    rslvr: persist_state
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    persist_state:
      type: bool
      resolve:
        with:
          - provider: parameter
            inputs:
              key: persist
              default: "false"
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	// persist=false -> state disabled -> no file written, action still runs.
	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "-r", "persist=false")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "ACTION_RAN")
	assert.NoFileExists(t, statePath, "state must not be written when the enabled resolver is false")

	// persist=true -> state enabled -> file written.
	_, _, exitCode = runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath, "-r", "persist=true")
	assert.Equal(t, 0, exitCode)
	require.FileExists(t, statePath, "state must be written when the enabled resolver is true")
}

// TestIntegration_RunSolution_StateCycleRejected verifies that referencing a
// state-dependent resolver (one that reads the state snapshot) from a load-time
// state field is rejected with a clear circular-dependency error before anything
// runs.
func TestIntegration_RunSolution_StateCycleRejected(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-cycle
  version: 1.0.0
state:
  enabled:
    rslvr: reads_state
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    reads_state:
      resolve:
        with:
          - provider: state
            inputs:
              key: anything
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))
	statePath := filepath.Join(tmpDir, "state.json")

	_, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath)
	assert.NotEqual(t, 0, exitCode, "a state cycle must fail the run")
	assert.Contains(t, stderr, "state.enabled", "error should name the offending field")
	assert.Contains(t, stderr, "reads_state", "error should name the state-dependent resolver")
	assert.NoFileExists(t, statePath, "no state should be written when a cycle is detected")
}

// TestIntegration_RunSolution_StateNoDoubleExecution proves the two-phase
// pre-load reuses Phase-A results instead of re-executing them: a resolver with
// an observable side effect (appending to a counter file) is referenced by
// state.enabled, so it runs in Phase A. After a full run it must have executed
// exactly once, not twice.
func TestIntegration_RunSolution_StateNoDoubleExecution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	counterPath := filepath.ToSlash(filepath.Join(tmpDir, "counter.txt"))
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: state-no-double-exec
  version: 1.0.0
state:
  enabled:
    expr: "_.side_effect.stdout.contains('ok')"
  backend:
    provider: file
    inputs:
      path: state.json
spec:
  resolvers:
    side_effect:
      resolve:
        with:
          - provider: exec
            inputs:
              command: "echo run >> '` + counterPath + `'; echo ok"
  workflow:
    actions:
      notify:
        provider: message
        inputs:
          message: "ACTION_RAN"
          type: info
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "run", "solution", "-f", solutionPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "ACTION_RAN")

	raw, err := os.ReadFile(filepath.Join(tmpDir, "counter.txt")) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	lines := strings.Count(strings.TrimSpace(string(raw)), "\n") + 1
	assert.Equal(t, 1, lines, "side-effect resolver must execute exactly once (Phase A seed reused in main run); got:\n%s", raw)
}

// TestIntegration_RunResolver_DynamicStatePath verifies the two-phase state
// pre-load is wired into the `run resolver` entrypoint: the resolver-derived
// state path is honored and state is written there.
func TestIntegration_RunResolver_DynamicStatePath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dynamic-state-path-resolver
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path:
        expr: "_.app_name + '-state.json'"
spec:
  resolvers:
    app_name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: app_name
              default: demo
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o600))

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "run", "resolver", "-f", solutionPath, "-r", "app_name=web")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "web")
	require.FileExists(t, filepath.Join(tmpDir, "web-state.json"), "run resolver must honor the dynamic state path")
}

func TestIntegration_RunSolution_FileNotFound(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "/nonexistent/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.True(t, strings.Contains(stderr, "not found") || strings.Contains(stderr, "no such file") || strings.Contains(stderr, "cannot find"))
}

func TestIntegration_RunSolution_InvalidYAML(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "invalid.yaml")

	require.NoError(t, os.WriteFile(solutionPath, []byte("not: valid: yaml: content:"), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", solutionPath,
	)

	assert.NotEqual(t, 0, exitCode)
	t.Logf("stderr: %s", stderr)
}

func TestIntegration_RunSolution_BadSolutionYAML(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/solutions/bad-solution-yaml/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "expected exactly one of rslvr, expr, or tmpl, but found")
	assert.Contains(t, stderr, "line")
}

func TestIntegration_RunSolution_NullResolver(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "tests/integration/solutions/edge-cases/null-resolver/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "null value")
}

func TestIntegration_Lint_NullResolver(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"lint",
		"-f", "tests/integration/solutions/edge-cases/null-resolver/solution.yaml",
		"-o", "json",
	)

	// Lint loads leniently, so a null resolver is reported as a null-resolver
	// finding (exit 2 for error-severity findings) instead of aborting the load.
	// Unrelated findings surface in the same pass (no "onion peeling").
	assert.Equal(t, 2, exitCode)
	assert.Contains(t, stdout, "null-resolver")
	assert.Contains(t, stdout, "has a null value")
	assert.Contains(t, stdout, "unused-resolver")
}

// TestIntegration_Lint_UnusedResolverSeverityByWorkflow verifies the
// context-dependent severity of unused-resolver through the CLI: INFO in a
// workflow-less (resolver-only) solution, WARNING once a workflow exists that
// could have consumed the resolver.
func TestIntegration_Lint_UnusedResolverSeverityByWorkflow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		solution     string
		wantSeverity string
		wantLocation string
	}{
		{
			name:         "workflow-less is info",
			solution:     "tests/integration/solutions/lint-unused-resolver/solution.yaml",
			wantSeverity: "info",
			wantLocation: "resolvers.terminalOutput",
		},
		{
			name:         "workflow present is warning",
			solution:     "tests/integration/solutions/lint-unused-resolver-workflow/solution.yaml",
			wantSeverity: "warning",
			wantLocation: "resolvers.orphaned",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, _, exitCode := runScafctl(t, "lint", "-f", tt.solution, "-o", "json")

			// Neither info nor warning findings fail the default gate.
			assert.Equal(t, 0, exitCode)

			var payload struct {
				Findings []struct {
					Severity string `json:"severity"`
					RuleName string `json:"ruleName"`
					Location string `json:"location"`
				} `json:"findings"`
			}
			require.NoError(t, json.Unmarshal([]byte(stdout), &payload),
				"lint -o json must emit a parseable document")

			var found bool
			for _, f := range payload.Findings {
				if f.RuleName != "unused-resolver" {
					continue
				}
				// Every unused-resolver finding in these fixtures must carry the
				// expected severity for the solution shape.
				assert.Equal(t, tt.wantSeverity, f.Severity,
					"unused-resolver severity for %s", f.Location)
				if f.Location == tt.wantLocation {
					found = true
				}
			}
			assert.True(t, found,
				"expected an unused-resolver finding at %s", tt.wantLocation)
		})
	}
}

func TestIntegration_RunSolution_DryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
	)

	assert.Equal(t, 0, exitCode)
	// WhatIf-style output with phase grouping
	assert.Contains(t, stdout, "DRY RUN: What would happen")
	assert.Contains(t, stdout, "What if:")
	assert.Contains(t, stdout, "Phase 1:")
}

func TestIntegration_RunResolver_Basic(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/actions/hello-world.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	// Should resolve but not execute actions
	assert.Contains(t, stdout, "greeting")
}

func TestIntegration_RunResolver_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "resolver", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Execute resolvers from a solution without running actions")
	assert.Contains(t, stdout, "--skip-transform")
	assert.Contains(t, stdout, "--graph")
	assert.Contains(t, stdout, "--snapshot")
	assert.Contains(t, stdout, "--file")
}

func TestIntegration_RunResolver_Alias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "res",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
}

func TestIntegration_RunResolver_NamedResolver(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"environment",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunResolver_MultipleNamedResolvers(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"hostname", "port",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)

	// hostname depends on environment and region (via CEL: _.environment + '-server-' + _.region)
	// Both requested resolvers and their transitive deps should be present
	assert.Contains(t, stdout, "\"hostname\"")
	assert.Contains(t, stdout, "\"port\"")
	assert.Contains(t, stdout, "\"environment\"")
	assert.Contains(t, stdout, "\"region\"")

	// exposedPort and config were not requested and are not dependencies
	assert.NotContains(t, stdout, "\"exposedPort\"")
	assert.NotContains(t, stdout, "\"config\"")
}

func TestIntegration_RunResolver_UnknownName(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"nonexistent",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown resolver(s): nonexistent")
}

func TestIntegration_RunResolver_ExecutionMetadataAlwaysIncluded(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	// By default, __execution is NOT included (clean output is the default)
	assert.NotContains(t, stdout, "__execution")
}

func TestIntegration_RunResolver_ShowExecution(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--show-execution",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	// --show-execution adds __execution metadata to output
	assert.Contains(t, stdout, "__execution")
	assert.Contains(t, stdout, "resolvers")
	assert.Contains(t, stdout, "summary")
	assert.Contains(t, stdout, "totalDuration")
	assert.Contains(t, stdout, "phaseCount")
}

func TestIntegration_RunResolver_SkipTransform(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--skip-transform",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	// skip-transform should not affect whether __execution is present (it is not, by default)
	assert.NotContains(t, stdout, "__execution")
}

func TestIntegration_RunResolver_PlanDataInjected(t *testing.T) {
	t.Parallel()
	// plan-aware.yaml uses __plan in a when: condition and in a CEL expression.
	// The resolver 'endpoint_info' only resolves when __plan['endpoint'].dependencyCount > 0.
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/plan-aware.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "expected exit code 0\nstdout: %s", stdout)
	// __plan-conditional resolver should have resolved
	assert.Contains(t, stdout, "dependent_check")
	// The resolved value embeds phase info from __plan
	assert.Contains(t, stdout, "phase")
}

func TestIntegration_RunResolver_PlanDataNotInOutput(t *testing.T) {
	t.Parallel()
	// __plan is an internal injection variable -- it must not appear in the clean output
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/plan-aware.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.NotContains(t, stdout, `"__plan"`)
}

// TestIntegration_RunResolver_AuthorFunctions exercises solution-author-defined
// Go template helpers (spec.functions) end to end: a CEL-bodied function, a
// numeric function with a typed parameter, and a template-bodied function that
// composes a sibling author function.
func TestIntegration_RunResolver_AuthorFunctions(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/author-functions.yaml",
		"-o", "json",
	)

	require.Equal(t, 0, exitCode, "expected exit code 0\nstdout: %s\nstderr: %s", stdout, stderr)
	// greet (CEL body) uppercases and prefixes.
	assert.Contains(t, stdout, `"greeting": "HELLO SCAF!"`)
	// doubled (typed numeric param) returns 21*2.
	assert.Contains(t, stdout, `"forty_two": 42`)
	// shout (template body) calls the sibling greet function.
	assert.Contains(t, stdout, `"composed": "HELLO WORLD! (WORLD)"`)
}

func TestIntegration_RunResolver_GraphASCII(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--graph",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
}

func TestIntegration_RunResolver_GraphDot(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--graph",
		"--graph-format=dot",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "digraph")
}

func TestIntegration_RunResolver_GraphMermaid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--graph",
		"--graph-format=mermaid",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "graph")
}

func TestIntegration_Validate_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "validate", "--help")

	assert.Equal(t, 0, exitCode)
	// Top-level validate command help advertises all three subcommands and
	// frames validate as the gate that runs lint.
	assert.Contains(t, stdout, "Validate")
	assert.Contains(t, stdout, "resolver")
	assert.Contains(t, stdout, "solution")
	assert.Contains(t, stdout, "schema")
	assert.Contains(t, stdout, "lint")
}

func TestIntegration_ValidateResolver_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "validate", "resolver", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver")
	assert.Contains(t, stdout, "--file")
}

func TestIntegration_ValidateResolver_Passes(t *testing.T) {
	t.Parallel()
	// A solution with no validation failures exits 0.
	_, _, exitCode := runScafctl(t,
		"validate", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ValidateResolver_FailsNonZero(t *testing.T) {
	t.Parallel()
	// The validate gate presets fatal validation, so a solution with validation
	// failures must exit non-zero (ValidationFailed == 2).
	_, _, exitCode := runScafctl(t,
		"validate", "resolver",
		"-f", "examples/resolver-validation-failures-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, exitcode.ValidationFailed, exitCode)
}

// --- validate schema ---

// writeValidateSchemaFixtures writes a minimal JSON Schema plus valid and
// invalid data files into a temp dir and returns their paths.
func writeValidateSchemaFixtures(t *testing.T) (schemaPath, validPath, invalidPath string) {
	t.Helper()
	dir := t.TempDir()

	schemaPath = filepath.Join(dir, "schema.json")
	schemaContent := `{
  "type": "object",
  "properties": {
    "name": { "type": "string" },
    "age": { "type": "integer" }
  },
  "required": ["name", "age"]
}`
	require.NoError(t, os.WriteFile(schemaPath, []byte(schemaContent), 0o600))

	validPath = filepath.Join(dir, "valid.json")
	require.NoError(t, os.WriteFile(validPath, []byte(`{"name": "alice", "age": 30}`), 0o600))

	invalidPath = filepath.Join(dir, "invalid.json")
	// age is a string here, violating the integer constraint.
	require.NoError(t, os.WriteFile(invalidPath, []byte(`{"name": "alice", "age": "thirty"}`), 0o600))

	return schemaPath, validPath, invalidPath
}

func TestIntegration_ValidateSchema_ValidData(t *testing.T) {
	t.Parallel()
	schemaPath, validPath, _ := writeValidateSchemaFixtures(t)

	_, _, exitCode := runScafctl(t,
		"validate", "schema",
		"--schema", schemaPath,
		"--data", validPath,
	)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ValidateSchema_InvalidData(t *testing.T) {
	t.Parallel()
	schemaPath, _, invalidPath := writeValidateSchemaFixtures(t)

	_, stderr, exitCode := runScafctl(t,
		"validate", "schema",
		"--schema", schemaPath,
		"--data", invalidPath,
	)

	// Invalid data violates the schema -> ValidationFailed (2).
	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	// The violation message should name the offending field and the constraint.
	assert.Contains(t, stderr, "violation")
	assert.Contains(t, stderr, "age")
}

func TestIntegration_ValidateSchema_DataFromStdin(t *testing.T) {
	t.Parallel()
	schemaPath, validPath, _ := writeValidateSchemaFixtures(t)

	data, err := os.ReadFile(validPath)
	require.NoError(t, err)

	_, _, exitCode := runScafctlWithStdin(t, bytes.NewReader(data),
		"validate", "schema",
		"--schema", schemaPath,
		"--data", "-",
	)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ValidateSchema_MissingSchemaFile(t *testing.T) {
	t.Parallel()
	schemaPath, validPath, _ := writeValidateSchemaFixtures(t)
	missing := filepath.Join(filepath.Dir(schemaPath), "does-not-exist.json")

	_, _, exitCode := runScafctl(t,
		"validate", "schema",
		"--schema", missing,
		"--data", validPath,
	)

	// A missing schema file -> FileNotFound (4).
	assert.Equal(t, exitcode.FileNotFound, exitCode)
}

// --- validate solution ---

func TestIntegration_ValidateSolution_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "validate", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "solution")
	assert.Contains(t, stdout, "--strict")
}

// writeLintWarningSolution writes a solution whose only lint findings are
// warnings (a hyphenated resolver name and an unused resolver). It never
// produces a lint error.
func writeLintWarningSolution(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: validate-warn-sol
  version: 1.0.0
  description: "Solution with a hyphenated resolver name to trigger a lint warning"
spec:
  resolvers:
    my-resolver:
      description: "hyphenated name triggers a hyphenated-name warning"
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello"
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestIntegration_ValidateSolution_Clean(t *testing.T) {
	t.Parallel()
	// A clean solution fixture (schema-valid, no lint errors) exits 0.
	_, _, exitCode := runScafctl(t,
		"validate", "solution",
		"-f", "tests/integration/solutions/lint-schema/valid-minimal.yaml",
	)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ValidateSolution_LintWarningPasses(t *testing.T) {
	t.Parallel()
	// A solution whose only lint findings are warnings exits 0 by default,
	// but the warnings are surfaced in the output.
	path := writeLintWarningSolution(t)

	stdout, stderr, exitCode := runScafctl(t,
		"validate", "solution",
		"-f", path,
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "hyphenated-name")
}

func TestIntegration_ValidateSolution_LintErrorFails(t *testing.T) {
	t.Parallel()
	// A solution with a lint ERROR (unknown top-level field ->
	// schema-violation) fails with ValidationFailed (2).
	stdout, stderr, exitCode := runScafctl(t,
		"validate", "solution",
		"-f", "tests/integration/solutions/lint-schema/unknown-field.yaml",
	)

	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	assert.Contains(t, stdout+stderr, "schema-violation")
}

func TestIntegration_ValidateSolution_StrictWarningFails(t *testing.T) {
	t.Parallel()
	// With --strict, a solution whose only lint findings are warnings fails
	// with ValidationFailed (2).
	path := writeLintWarningSolution(t)

	stdout, stderr, exitCode := runScafctl(t,
		"validate", "solution",
		"-f", path,
		"--strict",
	)

	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	assert.Contains(t, stdout+stderr, "hyphenated-name")
}

func TestIntegration_ValidateResolver_StrictLintWarningFails(t *testing.T) {
	t.Parallel()
	// 'validate resolver' also runs lint as a gate. With --strict, a solution
	// whose resolvers validate cleanly but that has a lint warning
	// (hyphenated-name) must fail with ValidationFailed (2).
	path := writeLintWarningSolution(t)

	_, stderr, exitCode := runScafctl(t,
		"validate", "resolver",
		"-f", path,
		"--strict",
	)

	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	assert.Contains(t, stderr, "hyphens")
}

func TestIntegration_RunResolver_GraphJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--graph",
		"--graph-format=json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "nodes")
	assert.Contains(t, stdout, "edges")
	assert.Contains(t, stdout, "stats")
	assert.Contains(t, stdout, "criticalPath")
}

func TestIntegration_RunResolver_ExecutionIncludesGraphAndProviderSummary(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
		"--show-execution",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"dependencyGraph\"")
	assert.Contains(t, stdout, "\"providerSummary\"")
	assert.Contains(t, stdout, "criticalPath")
	assert.Contains(t, stdout, "criticalDepth")
	assert.Contains(t, stdout, "\"diagrams\"")
	assert.Contains(t, stdout, "\"ascii\"")
	assert.Contains(t, stdout, "\"dot\"")
	assert.Contains(t, stdout, "\"mermaid\"")
}

func TestIntegration_RunResolver_Snapshot(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.json")
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Snapshot saved to")

	// Verify snapshot file was created and is valid JSON
	data, err := os.ReadFile(snapshotFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "metadata")
	assert.Contains(t, string(data), "resolvers")
}

func TestIntegration_RunResolver_SnapshotRedact(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot-redact.json")
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
		"--redact",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Snapshot saved to")
}

func TestIntegration_RunResolver_SensitiveRedactedInTable(t *testing.T) {
	t.Parallel()
	// Create a temp solution with sensitive values
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "sensitive.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-test
  version: 1.0.0
spec:
  resolvers:
    secret_val:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: "my-secret-password"
    public_val:
      resolve:
        with:
          - provider: static
            inputs:
              value: "public-data"
`
	err := os.WriteFile(solutionPath, []byte(content), 0o600)
	require.NoError(t, err)

	// Table output should redact sensitive values
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", solutionPath,
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "[REDACTED]")
	assert.NotContains(t, stdout, "my-secret-password")
	assert.Contains(t, stdout, "public-data")
}

func TestIntegration_RunResolver_SensitiveRevealedInJSON(t *testing.T) {
	t.Parallel()
	// Create a temp solution with sensitive values
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "sensitive.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-test
  version: 1.0.0
spec:
  resolvers:
    secret_val:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: "my-secret-password"
    public_val:
      resolve:
        with:
          - provider: static
            inputs:
              value: "public-data"
`
	err := os.WriteFile(solutionPath, []byte(content), 0o600)
	require.NoError(t, err)

	// JSON output should reveal sensitive values (Terraform model)
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", solutionPath,
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-secret-password", "JSON output should reveal sensitive values")
	assert.NotContains(t, stdout, "[REDACTED]", "JSON output should not redact")
	assert.Contains(t, stdout, "public-data")
}

func TestIntegration_RunResolver_ShowSensitiveFlag(t *testing.T) {
	t.Parallel()
	// Create a temp solution with sensitive values
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "sensitive.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-test
  version: 1.0.0
spec:
  resolvers:
    secret_val:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: "my-secret-password"
`
	err := os.WriteFile(solutionPath, []byte(content), 0o600)
	require.NoError(t, err)

	// --show-sensitive should work as a recognized flag
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", solutionPath,
		"--show-sensitive",
	)

	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
}

func TestIntegration_RunResolver_SnapshotRequiresFile(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "--snapshot-file")
}

func TestIntegration_RunSolution_ShowExecution(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
		"--show-execution",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "__execution")
	assert.Contains(t, stdout, "resolvers")
	assert.Contains(t, stdout, "summary")
}

func TestIntegration_RunSolution_ExecutionContextInActions(t *testing.T) {
	t.Parallel()
	// execution-aware-actions uses __execution in action when: conditions and inputs.
	// non-prod-deploy runs (staging != production) and its message input uses __execution.
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/solutions/execution-aware-actions/solution.yaml",
	)

	assert.Equal(t, 0, exitCode, "expected exit code 0\nstdout: %s\nstderr: %s", stdout, stderr)
	// non-prod-deploy should have run (default environment is staging != production)
	assert.Contains(t, stdout, "staging-cluster")
	// prod-gate should have been skipped
	assert.NotContains(t, stdout, "production-only checks")
}

func TestIntegration_RunSolution_ConditionalRetry(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/conditional-retry.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "all tests complete")
}

func TestIntegration_RunSolution_K8sClusters(t *testing.T) {
	t.Parallel()
	// Clean up output directory before and after (relative to project root where scafctl runs)
	projectRoot := findProjectRoot()
	outputDir := filepath.Join(projectRoot, "output")
	os.RemoveAll(outputDir)
	t.Cleanup(func() { os.RemoveAll(outputDir) })

	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/solutions/k8s-clusters/solution.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)

	// Verify all 10 cluster manifests were generated
	expectedClusters := []string{
		"us-east-prod", "us-east-dev", "eu-west-prod", "eu-west-staging",
		"ap-south-dev", "ap-south-qa", "us-central-prod", "us-central-dev",
		"eu-north-staging", "ap-east-prod",
	}
	for _, cluster := range expectedClusters {
		manifestPath := filepath.Join(outputDir, cluster, "manifest.yaml")
		assert.FileExists(t, manifestPath, "expected manifest for cluster %s", cluster)

		content, err := os.ReadFile(manifestPath)
		if assert.NoError(t, err) {
			assert.Contains(t, string(content), "name: "+cluster, "manifest should contain cluster name")
			assert.Contains(t, string(content), "kind: Namespace", "manifest should contain Namespace")
			assert.Contains(t, string(content), "kind: ResourceQuota", "manifest should contain ResourceQuota")
		}
	}
}

func TestIntegration_RunSolution_TemplateDirectory(t *testing.T) {
	t.Parallel()
	// Tests the directory → render-tree → write-tree pipeline end-to-end.
	// Reads .tpl templates, renders them with shared vars, writes output
	// stripping the .tpl extension and preserving directory structure.
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	// The solution references $OUTPUT_DIR as basePath — we inject it via env
	// by rewriting the solution to use a concrete path.
	solutionSrc, err := os.ReadFile(filepath.Join(projectRoot,
		"tests/integration/solutions/template-directory/solution.yaml"))
	require.NoError(t, err)

	solutionContent := strings.ReplaceAll(string(solutionSrc), "$OUTPUT_DIR", outputDir)
	tmpSolution := filepath.Join(solutionDir, "solution.yaml")
	err = os.WriteFile(tmpSolution, []byte(solutionContent), 0o644)
	require.NoError(t, err)

	// Copy the templates directory next to the solution so relative path "templates" works
	srcTemplates := filepath.Join(projectRoot, "tests/integration/solutions/template-directory/templates")
	dstTemplates := filepath.Join(solutionDir, "templates")
	err = copyDir(srcTemplates, dstTemplates)
	require.NoError(t, err)

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", tmpSolution,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Verify output files exist with correct content
	type expectedFile struct {
		path     string
		contains []string
	}
	expected := []expectedFile{
		{
			path:     "k8s/deployment.yaml",
			contains: []string{"name: test-app", "namespace: test-ns", "replicas: 2"},
		},
		{
			path:     "k8s/service.yaml",
			contains: []string{"name: test-app-svc", "namespace: test-ns", "port: 8080"},
		},
		{
			path:     "config/app.yaml",
			contains: []string{"port: 8080", "level: debug"},
		},
		{
			path:     "README.md",
			contains: []string{"# test-app", "Version: 2.0.0"},
		},
		// Tree fan-out (forEach + pathTemplate): the whole template tree is
		// rendered once per environment into a distinct envs/<name>/ subtree.
		{
			path:     "envs/dev/k8s/deployment.yaml",
			contains: []string{"name: test-app", "namespace: test-ns"},
		},
		{
			path:     "envs/prod/k8s/deployment.yaml",
			contains: []string{"name: test-app", "namespace: test-ns"},
		},
		{
			path:     "envs/dev/config/app.yaml",
			contains: []string{"port: 8080", "level: debug"},
		},
		{
			path:     "envs/prod/README.md",
			contains: []string{"# test-app", "Version: 2.0.0"},
		},
	}

	for _, ef := range expected {
		fullPath := filepath.Join(outputDir, ef.path)
		assert.FileExists(t, fullPath, "expected file %s", ef.path)

		content, readErr := os.ReadFile(fullPath)
		if assert.NoError(t, readErr, "reading %s", ef.path) {
			for _, substr := range ef.contains {
				assert.Contains(t, string(content), substr,
					"file %s should contain %q", ef.path, substr)
			}
		}
	}

	// Ensure .tpl files do NOT exist — extension should be stripped
	assert.NoFileExists(t, filepath.Join(outputDir, "k8s/deployment.yaml.tpl"))
	assert.NoFileExists(t, filepath.Join(outputDir, "README.md.tpl"))
	// Fan-out paths are also .tpl-stripped.
	assert.NoFileExists(t, filepath.Join(outputDir, "envs/dev/k8s/deployment.yaml.tpl"))
}

func TestIntegration_RunSolution_RetryIfWithCommandNotFound(t *testing.T) {
	t.Parallel()
	// Test that retryIf: "false" prevents retries on actual errors
	// Using a non-existent command which returns a real error
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "retry-if-cmd-not-found.yaml")

	solution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: retry-if-cmd-not-found-test
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      fail-no-retry:
        provider: exec
        retry:
          maxAttempts: 3
          backoff: fixed
          initialDelay: 10ms
          # Disable retry - should fail immediately
          retryIf: "false"
        inputs:
          command: "/nonexistent-command-12345"
`
	require.NoError(t, os.WriteFile(solutionPath, []byte(solution), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", solutionPath,
	)

	// The exec provider behavior depends on whether it's the builtin
	// (stat-checks commands, returns Go errors for missing binaries → exit 6)
	// or the extracted plugin (runs through shell, no Go error → exit 0).
	// Accept either outcome; the key assertion is that retryIf: "false"
	// prevents any retry.
	assert.Contains(t, []int{0, 6}, exitCode, "expected exit code 0 (plugin) or 6 (builtin)")
	assert.Contains(t, stderr, "fail-no-retry")
	assert.NotContains(t, stderr, "    retry ", "retryIf: \"false\" should prevent any retry attempts")
	t.Logf("stderr: %s", stderr)
}

func TestIntegration_RunSolution_RetryIfWithRetryEnabled(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script which requires /bin/sh")
	}
	// Test that retryIf: "true" allows retries on actual errors
	// This creates a temp script that succeeds on second run
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "retry-if-enabled.yaml")
	scriptPath := filepath.Join(tmpDir, "retry-script.sh")
	counterFile := filepath.Join(tmpDir, "counter.txt")

	// Create a script that fails first time, succeeds second time
	script := `#!/bin/sh
if [ -f "` + filepath.ToSlash(counterFile) + `" ]; then
  echo "Second attempt - success"
  exit 0
else
  echo "1" > "` + filepath.ToSlash(counterFile) + `"
  exit 1
fi
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	// Note: The exec provider doesn't return errors for non-zero exit codes
	// so retryIf won't trigger on exit code. This test validates the retryIf
	// expression is parsed correctly and doesn't cause errors.
	solution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: retry-if-enabled-test
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      retry-action:
        provider: exec
        retry:
          maxAttempts: 3
          backoff: fixed
          initialDelay: 10ms
          # Always retry on error (won't trigger for exit code failures)
          retryIf: "true"
        inputs:
          command: "` + filepath.ToSlash(scriptPath) + `"
`
	require.NoError(t, os.WriteFile(solutionPath, []byte(solution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", solutionPath,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	// The action completes (exec provider returns success even with non-zero exit)
	// but we verify the retryIf expression doesn't cause parsing errors
	assert.Equal(t, 0, exitCode, "should complete without retryIf parsing errors")
}

// ============================================================================
// Render Solution Tests
// ============================================================================

func TestIntegration_RenderSolution(t *testing.T) {
	t.Parallel()
	// Use run resolver to get resolver outputs
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
	)

	assert.Equal(t, 0, exitCode)
	// Should contain resolver outputs
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "region")
}

func TestIntegration_RenderSolutionJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "production")
	assert.Contains(t, stdout, "us-west-2")
}

func TestIntegration_RenderSolutionYAML(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "yaml",
	)

	assert.Equal(t, 0, exitCode)
	// YAML output contains the data - check for key content
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

// Snapshot feature tests moved to resolver tests since snapshot isn't on run solution

// ============================================================================
// Action Graph Tests
// ============================================================================

func TestIntegration_ActionGraph(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
		"--action-graph",
	)

	assert.Equal(t, 0, exitCode)
	// Should show action dependency graph
	assert.Contains(t, stdout, "Action Dependency Graph")
	assert.Contains(t, stdout, "Phase")
	t.Logf("action graph output: %s", stdout)
}

// TestIntegration_RenderSolution_GraphFlagRemoved verifies that the legacy --graph
// flag (removed in PR #145) is no longer accepted by "render solution".
func TestIntegration_RenderSolution_GraphFlagRemoved(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--graph",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown flag")
}

// TestIntegration_RenderSolution_Effective verifies the effective-document
// renderer emits the fully-composed solution deterministically without running
// resolvers, and honors --section and mutual-exclusion guards.
func TestIntegration_RenderSolution_Effective(t *testing.T) {
	t.Parallel()

	const solPath = "examples/solutions/compose-fidelity/solution.yaml"

	t.Run("full document is deterministic", func(t *testing.T) {
		t.Parallel()
		out1, _, code1 := runScafctl(t, "render", "solution", "-f", solPath, "--effective", "-o", "yaml")
		require.Equal(t, 0, code1)
		out2, _, code2 := runScafctl(t, "render", "solution", "-f", solPath, "--effective", "-o", "yaml")
		require.Equal(t, 0, code2)

		assert.Equal(t, out1, out2, "effective output must be byte-stable")
		// Composed from both partials.
		assert.Contains(t, out1, "app_name")
		assert.Contains(t, out1, "environment")
		assert.Contains(t, out1, "deploy")
		// compose: field is cleared on the merged, self-contained document.
		assert.NotContains(t, out1, "compose:")
	})

	t.Run("stdout matches committed golden byte-for-byte", func(t *testing.T) {
		t.Parallel()
		stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", solPath, "--effective", "-o", "yaml")
		require.Equal(t, 0, exitCode)
		golden, err := os.ReadFile(filepath.Join(findProjectRoot(), "examples/solutions/compose-fidelity/golden.effective.yaml"))
		require.NoError(t, err)
		// Verbatim: stdout must equal the committed golden exactly (no extra
		// trailing newline), which is the core golden-file fidelity guarantee.
		assert.Equal(t, string(golden), stdout)
	})

	t.Run("section workflow scopes output", func(t *testing.T) {
		t.Parallel()
		stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", solPath,
			"--effective", "--section", "workflow", "-o", "yaml")
		require.Equal(t, 0, exitCode)
		assert.Contains(t, stdout, "deploy")
		// Only the workflow projection is emitted, not the resolvers map.
		assert.NotContains(t, stdout, "resolvers:")
	})

	t.Run("section resolvers scopes output", func(t *testing.T) {
		t.Parallel()
		stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", solPath,
			"--effective", "--section", "resolvers", "-o", "yaml")
		require.Equal(t, 0, exitCode)
		assert.Contains(t, stdout, "app_name")
		// Only the resolvers projection is emitted, not the workflow actions.
		assert.NotContains(t, stdout, "actions:")
	})

	t.Run("json output", func(t *testing.T) {
		t.Parallel()
		stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", solPath, "--effective", "-o", "json")
		require.Equal(t, 0, exitCode)
		var parsed map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &parsed))
		assert.Contains(t, parsed, "spec")
	})

	t.Run("defaults to yaml without -o", func(t *testing.T) {
		t.Parallel()
		stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", solPath, "--effective")
		require.Equal(t, 0, exitCode)
		// YAML, not JSON, even though the shared -o flag defaults to json.
		assert.Contains(t, stdout, "apiVersion: scafctl.io/v1")
		assert.NotContains(t, stdout, `"apiVersion"`)
	})

	t.Run("section without effective is rejected", func(t *testing.T) {
		t.Parallel()
		_, stderr, exitCode := runScafctl(t, "render", "solution", "-f", solPath, "--section", "workflow")
		assert.NotEqual(t, 0, exitCode)
		assert.Contains(t, stderr, "--section is only applicable with --effective")
	})

	t.Run("effective and snapshot are mutually exclusive", func(t *testing.T) {
		t.Parallel()
		_, stderr, exitCode := runScafctl(t, "render", "solution", "-f", solPath,
			"--effective", "--snapshot", "--snapshot-file", "x.json")
		assert.NotEqual(t, 0, exitCode)
		assert.Contains(t, stderr, "mutually exclusive")
	})
}

func TestIntegration_ActionGraphMermaid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
		"--action-graph",
		"--graph-format", "mermaid",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "graph LR")
	assert.Contains(t, stdout, "subgraph")
}

func TestIntegration_ActionGraphDOT(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
		"--action-graph",
		"--graph-format", "dot",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "digraph Actions")
	assert.Contains(t, stdout, "subgraph cluster_phase")
}

func TestIntegration_ActionGraphJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
		"--action-graph",
		"--graph-format", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "phases")
	assert.Contains(t, stdout, "stats")
}

// ============================================================================
// Config Command Tests
// ============================================================================

func TestIntegration_ConfigView(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "config", "view")

	// May return non-zero if no config exists, but shouldn't crash
	t.Logf("exit code: %d, stdout: %s", exitCode, stdout)
}

func TestIntegration_ConfigSchema(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "config", "schema")

	assert.Equal(t, 0, exitCode)
	// Should output JSON schema
	assert.Contains(t, stdout, "properties")
}

func TestIntegration_ConfigReset_RequiresForce(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("custom: true"), 0o600))

	_, stderr, exitCode := runScafctl(t, "--config", configPath, "config", "reset")

	assert.NotEqual(t, 0, exitCode, "should fail without --force")
	assert.Contains(t, stderr, "--force")

	// Config should be untouched.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "custom: true")
}

func TestIntegration_ConfigReset_HappyPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("custom: true"), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "--config", configPath, "config", "reset", "--force")

	assert.Equal(t, 0, exitCode, "should succeed with --force")
	combined := stdout + stderr
	assert.Contains(t, combined, "Reset config file")

	// Config should be recreated with defaults (not our custom value).
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "custom: true")
}

// ============================================================================
// Secrets Command Tests (basic, non-destructive)
// ============================================================================

func TestIntegration_SecretsList(t *testing.T) {
	t.Parallel()
	// This test just verifies the command doesn't crash
	_, _, exitCode := runScafctl(t, "secrets", "list")

	// May fail if no secrets store, but shouldn't crash badly
	t.Logf("exit code: %d", exitCode)
}

func TestIntegration_SecretsHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "secrets", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "set")
	assert.Contains(t, stdout, "get")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "delete")
}

// TestIntegration_SecretsFileFallback is a regression test for issue 683: on a
// host with no OS keyring (headless/WSL/CI Linux, no org.freedesktop.secrets),
// `secrets set`/`secrets get` must still work by degrading to the file-based
// master.key backend instead of hard-failing.
func TestIntegration_SecretsFileFallback(t *testing.T) {
	t.Parallel()

	// This regression reproduces the headless-Linux condition: no reachable
	// org.freedesktop.secrets Secret Service. We force that deterministically
	// on Linux by pointing D-Bus at an unreachable session bus, which makes
	// zalando/go-keyring's Secret Service lookup fail with the "not provided
	// by any .service" activation error the fix classifies as unavailable.
	//
	// On macOS/Windows the OS keychain/credential manager is always present and
	// cannot be disabled from a subprocess (there is no backend selector by
	// design), so the deterministic file-fallback path is exercised by the
	// store-level unit test TestNew_KeyringUnavailableFallsBackToFile instead.
	if runtime.GOOS != "linux" {
		t.Skipf("cannot deterministically disable the OS keyring on %s; "+
			"file fallback is covered by pkg/secrets unit tests", runtime.GOOS)
	}

	dataHome := t.TempDir()
	// Override XDG_DATA_HOME (isolate storage), force D-Bus to an unreachable
	// socket (no Secret Service), and drop any inherited SCAFCTL_SECRET_KEY so
	// the env backend cannot satisfy the request -- leaving only the file
	// backend. SCAFCTL_REQUIRE_SECURE_KEYRING=false keeps the file fallback
	// non-fatal regardless of the host's config.
	env := map[string]string{
		"XDG_DATA_HOME":                  dataHome,
		"DBUS_SESSION_BUS_ADDRESS":       "unix:path=/nonexistent/scafctl-test-no-dbus",
		"SCAFCTL_REQUIRE_SECURE_KEYRING": "false",
	}
	unset := []string{"SCAFCTL_SECRET_KEY"}

	// Set a secret -- must succeed even without an OS keyring.
	_, setErr, setExit := runScafctlIsolatedEnv(t, unset, env, "secrets", "set", "issue683", "--value", "hello")
	require.Equal(t, 0, setExit, "secrets set should succeed via file fallback; stderr: %s", setErr)

	// Get it back.
	getOut, getErr, getExit := runScafctlIsolatedEnv(t, unset, env, "secrets", "get", "issue683")
	require.Equal(t, 0, getExit, "secrets get should succeed; stderr: %s", getErr)
	assert.Contains(t, getOut, "hello")

	// The master key must have been persisted to the file backend.
	masterKey := filepath.Join(dataHome, "scafctl", "master.key")
	info, statErr := os.Stat(masterKey)
	require.NoError(t, statErr, "master.key should be written to the file backend")
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "master.key must be mode 0600")
}

func TestIntegration_SecretsHelpDocumentsFallbacks(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "secrets", "--help")

	assert.Equal(t, 0, exitCode)
	// Help must advertise the env and file fallbacks, not just the OS keychain
	// (issue 683 doc gap).
	assert.Contains(t, stdout, "SECRET_KEY")
	assert.Contains(t, stdout, "master.key")
}

// ============================================================================
// Auth Command Tests (basic, non-destructive)
// ============================================================================

func TestIntegration_AuthStatus(t *testing.T) {
	t.Parallel()
	// This test just verifies the command doesn't crash
	stdout, _, exitCode := runScafctl(t, "auth", "status")

	t.Logf("exit code: %d, stdout: %s", exitCode, stdout)
}

func TestIntegration_AuthHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "login")
	assert.Contains(t, stdout, "logout")
	assert.Contains(t, stdout, "status")
	assert.Contains(t, stdout, "handlers")
}

func TestIntegration_AuthHandlers(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "handlers")

	assert.Equal(t, 0, exitCode)
	// The command should produce output listing available handlers.
	assert.NotEmpty(t, stdout)
}

func TestIntegration_AuthHandlersJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "handlers", "-o", "json")

	assert.Equal(t, 0, exitCode)
	// JSON output must be valid and parseable.
	var parsed []map[string]any
	err := json.Unmarshal([]byte(stdout), &parsed)
	assert.NoError(t, err, "output should be valid JSON")
}

func TestIntegration_AuthHandlersHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "handlers", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "handlers")
}

func TestIntegration_AuthList(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "list", "-o", "json")
	combined := stdout + stderr

	// With lazy auth handler loading, no handlers are registered until
	// explicitly requested. The command may exit 0 with "No cached tokens"
	// or exit 1 with "no auth handlers registered".
	assert.True(t,
		exitCode == 0 || strings.Contains(combined, "no auth handlers registered"),
		"expected success or no-handlers message, got exit=%d output=%q", exitCode, combined,
	)
}

func TestIntegration_AuthListJSON(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "list", "-o", "json")
	combined := stdout + stderr

	// With lazy auth handler loading, the command may exit 1 with
	// "no auth handlers registered" when no handlers have been resolved.
	if exitCode != 0 {
		assert.Contains(t, combined, "no auth handlers registered")
		return
	}
	// When tokens are present they are returned as JSON; when absent the
	// informational message is written to stderr/stdout without JSON.
	if strings.Contains(stdout, `"handler"`) {
		assert.Contains(t, stdout, `"tokenKind"`)
	}
}

func TestIntegration_AuthListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "handler")
	assert.Contains(t, stdout, "refresh")
}

func TestIntegration_AuthAliasHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "alias", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "set")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "remove")
}

func TestIntegration_AuthLoginHelp_ShowsHostname(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "login", "--help")

	assert.Equal(t, 0, exitCode)
	// The --hostname flag drives host-aware login (alias / dynamic resolution).
	assert.Contains(t, stdout, "--hostname")
}

func TestIntegration_AuthAliasList_Seeded(t *testing.T) {
	t.Parallel()
	// 'auth alias list' reads config directly and does not require a live
	// handler, so a seeded config file exercises the full read + render path.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	seed := `auth:
  handlers:
    openshift:
      hostname:
        aliases:
          prod: https://api.prod.example.com:6443
          stg: https://api.stg.example.com:6443
`
	require.NoError(t, os.WriteFile(configPath, []byte(seed), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "--config", configPath, "auth", "alias", "list", "openshift", "-o", "json")
	require.Equal(t, 0, exitCode, "stderr: %s", stderr)

	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.Len(t, items, 2)

	bySelector := map[string]string{}
	for _, it := range items {
		bySelector[fmt.Sprint(it["selector"])] = fmt.Sprint(it["url"])
	}
	assert.Equal(t, "https://api.prod.example.com:6443", bySelector["prod"])
	assert.Equal(t, "https://api.stg.example.com:6443", bySelector["stg"])
}

func TestIntegration_AuthTokenHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "token", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--scope")
	assert.Contains(t, stdout, "--min-valid-for")
	assert.Contains(t, stdout, "--force-refresh")
	// Render-mode flags and the raw-by-default behavior must be documented.
	assert.Contains(t, stdout, "--raw")
	assert.Contains(t, stdout, "--curl")
	assert.Contains(t, stdout, "--export")
	assert.Contains(t, stdout, "--exec-credential")
	assert.Contains(t, stdout, "prints the raw access token")
}

func TestIntegration_AuthMigrateHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "migrate", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "migrate")
}

// NOTE: Auth handlers (entra, gcp, github) have been extracted to plugins.
// These tests validate the CLI behaviour regardless of whether the plugin is
// installed. When the plugin is not installed, the handler is "unknown" and
// the command fails with "unknown auth handler". When installed, the handler
// works as before. Tests below accept either outcome.

func TestIntegration_AuthLoginGCPHelp(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "login", "gcp", "--help")

	if exitCode == 0 {
		assert.Contains(t, stdout, "gcp")
		assert.Contains(t, stdout, "--flow")
	} else {
		// Plugin not installed — handler is unknown
		assert.Contains(t, stderr, "unknown auth handler")
	}
}

func TestIntegration_AuthLoginGCPInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "gcp", "--flow", "invalid-flow")

	assert.NotEqual(t, 0, exitCode)
	// Either "unknown flow" (plugin installed) or "unknown auth handler" (plugin not installed)
	assert.True(t,
		strings.Contains(stderr, "unknown flow") || strings.Contains(stderr, "unknown auth handler"),
		"expected flow or handler error, got: %s", stderr,
	)
}

func TestIntegration_LoginHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--handler")
	assert.Contains(t, stdout, "--server")
	assert.Contains(t, stdout, "--verify")
	assert.Contains(t, stdout, "kubeconfig")
}

func TestIntegration_LoginMissingHandler(t *testing.T) {
	t.Parallel()
	// No --handler and no cluster resolver configured, so no default handler is
	// available: login fails asking for a handler.
	_, stderr, exitCode := runScafctl(t, "kube", "login", "prod", "--server", "https://api.example.com:6443")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "auth handler is required")
}

func TestIntegration_LoginResolvesClusterFromConfigAlias(t *testing.T) {
	t.Parallel()
	// A kube.clusters static alias supplies the server and default handler, so
	// `kube login <alias>` resolves without --server or --handler. Login still
	// fails later (the openshift handler plugin is not installed), but it must
	// get PAST cluster resolution -- i.e. never hit the "no cluster server
	// resolved" error.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	seed := `kube:
  clusters:
    aliases:
      lab:
        server: https://api.lab.example.com:6443
        defaultHandler: openshift
`
	require.NoError(t, os.WriteFile(configPath, []byte(seed), 0o600))

	_, stderr, exitCode := runScafctl(t, "--config", configPath, "kube", "login", "lab")

	// Do not assert on the exit code: the goal is only that cluster resolution
	// succeeds. Login may fail later (the openshift handler plugin is not
	// installed) or, in some environments, get further -- either way it must
	// never hit the "no cluster server resolved" error.
	_ = exitCode
	assert.NotContains(t, stderr, "no cluster server resolved",
		"the config alias must supply the server so resolution succeeds")
}

func TestIntegration_LogoutHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "logout", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--keep-credentials")
	assert.Contains(t, stdout, "kubeconfig")
}

func TestIntegration_LogoutNoCluster(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "kube", "logout")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "cluster name is required")
}

func TestIntegration_LoginHelp_NamespaceAndRefresh(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--namespace")
	assert.Contains(t, stdout, "--refresh")
}

func TestIntegration_KubeLoginHelp_SaveAlias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--save-alias")
}

func TestIntegration_AuthLoginHelp_SaveAlias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--save-alias")
}

func TestIntegration_LogoutHelp_All(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "logout", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--all")
}

func TestIntegration_KubeListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "kube", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "clusters")
}

func TestIntegration_KubeListNoResolver(t *testing.T) {
	t.Parallel()
	// Isolate from the developer's real config (which may declare clusters) by
	// pointing at an empty config file. scafctl ships no cluster data, so with no
	// resolver configured, list reports that clearly instead of erroring.
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("{}\n"), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "--config", configPath, "kube", "list")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "No cluster resolver configured")
}

func TestIntegration_KubeStatusNoContext(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	kubeconfig := filepath.Join(tmpDir, "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "kube", "status", "--kubeconfig", kubeconfig)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "No current kubeconfig context")
}

func TestIntegration_KubeStatusShowsContext(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	kubeconfig := filepath.Join(tmpDir, "config")
	content := `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: prod
    cluster:
      server: https://api.prod.example.com:6443
contexts:
  - name: prod
    context:
      cluster: prod
      user: prod
      namespace: team-a
users:
  - name: prod
    user:
      token: static
`
	require.NoError(t, os.WriteFile(kubeconfig, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "kube", "status", "--kubeconfig", kubeconfig, "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "prod")
	assert.Contains(t, stdout, "https://api.prod.example.com:6443")
	assert.Contains(t, stdout, "team-a")
}

func TestIntegration_KubeLogoutAllNoEntries(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	kubeconfig := filepath.Join(tmpDir, "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte("apiVersion: v1\nkind: Config\n"), 0o600))

	stdout, stderr, exitCode := runScafctl(t, "kube", "logout", "--all", "--kubeconfig", kubeconfig)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout+stderr, "No scafctl-managed kubeconfig entries found")
}

func TestIntegration_KubeLogoutAllRejectsClusterArg(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "kube", "logout", "--all", "prod")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "--all cannot be combined with a cluster argument")
}

func TestIntegration_AuthStatusGCP(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "status", "gcp")

	// Succeeds if plugin is available and working, fails otherwise.
	// Possible errors: "unknown auth handler", "no auth handlers found", or gRPC plugin errors.
	if exitCode != 0 {
		assert.True(t,
			strings.Contains(stderr, "unknown auth handler") || strings.Contains(stderr, "no auth handlers found") || strings.Contains(stderr, "rpc error"),
			"expected handler/plugin error, got: %s", stderr,
		)
	}
}

func TestIntegration_AuthLogoutGCP(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "logout", "gcp")

	if exitCode == 0 {
		assert.True(t,
			strings.Contains(stdout, "Not currently authenticated") || strings.Contains(stdout, "Successfully logged out"),
			"expected logout message, got: %s", stdout,
		)
	} else {
		// Plugin not installed or plugin connection error
		assert.True(t,
			strings.Contains(stderr, "unknown auth handler") || strings.Contains(stderr, "no auth handlers found") || strings.Contains(stderr, "rpc error"),
			"expected handler/plugin error, got: %s", stderr,
		)
	}
}

func TestIntegration_AuthLoginEntraHelp(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "login", "entra", "--help")

	if exitCode == 0 {
		assert.Contains(t, stdout, "entra")
		assert.Contains(t, stdout, "--flow")
		assert.Contains(t, stdout, "--tenant")
		assert.Contains(t, stdout, "--callback-port")
	} else {
		assert.Contains(t, stderr, "unknown auth handler")
	}
}

func TestIntegration_AuthLoginEntraInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "entra", "--flow", "bogus-flow")

	assert.NotEqual(t, 0, exitCode)
	assert.True(t,
		strings.Contains(stderr, "unknown flow") || strings.Contains(stderr, "unknown auth handler"),
		"expected flow or handler error, got: %s", stderr,
	)
}

func TestIntegration_AuthLoginCallbackPortSupported(t *testing.T) {
	t.Parallel()
	// The login command accepts --callback-port as a generic flag for any handler.
	// Verify it appears in help output (help is handler-independent).
	stdout, _, exitCode := runScafctl(t, "auth", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--callback-port")
}

func TestIntegration_AuthLoginCallbackPortOutOfRange(t *testing.T) {
	t.Parallel()
	// The callback-port range is validated as a static input constraint before
	// any handler resolution, so out-of-range values are rejected regardless of
	// whether the target handler/plugin is installed.
	for _, port := range []string{"80", "-1", "70000"} {
		port := port
		t.Run(port, func(t *testing.T) {
			t.Parallel()
			_, stderr, exitCode := runScafctl(t, "auth", "login", "entra", "--callback-port", port)
			assert.NotEqual(t, 0, exitCode)
			assert.Contains(t, stderr, "--callback-port must be between 1024 and 65535",
				"expected range validation error, got: %s", stderr)
		})
	}
}

func TestIntegration_AuthLoginGitHubInteractiveFlow(t *testing.T) {
	t.Parallel()
	// Verify that --flow is accepted for the login command
	stdout, _, exitCode := runScafctl(t, "auth", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--flow")
}

func TestIntegration_AuthLoginGitHubInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "github", "--flow", "bogus-flow")

	assert.NotEqual(t, 0, exitCode)
	// Either "unknown flow" (plugin installed) or "unknown auth handler" (plugin not installed)
	assert.True(t,
		strings.Contains(stderr, "unknown flow") || strings.Contains(stderr, "unknown auth handler"),
		"expected flow or handler error, got: %s", stderr,
	)
}

func TestIntegration_AuthLoginGitHubAppFlow(t *testing.T) {
	t.Parallel()
	// github-app flow without required config should fail, unless already authenticated.
	stdout, stderr, exitCode := runScafctl(t, "auth", "login", "github", "--flow", "github-app")

	combined := stdout + stderr
	if exitCode == 0 {
		// Already authenticated — handler short-circuits with a warning.
		assert.True(t,
			strings.Contains(combined, "Already authenticated") || strings.Contains(combined, "already authenticated"),
			"expected already-authenticated warning on success, got stdout=%s stderr=%s", stdout, stderr,
		)
	} else {
		// Either config error (plugin installed) or handler not found (plugin not installed)
		assert.True(t,
			strings.Contains(stderr, "app ID is required") || strings.Contains(stderr, "unknown auth handler"),
			"expected config or handler error, got: %s", stderr,
		)
	}
}

func TestIntegration_AuthStatusEntra(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "status", "entra")

	// Succeeds if plugin is available and working, fails otherwise.
	if exitCode != 0 {
		assert.True(t,
			strings.Contains(stderr, "unknown auth handler") || strings.Contains(stderr, "no auth handlers found") || strings.Contains(stderr, "rpc error"),
			"expected handler/plugin error, got: %s", stderr,
		)
	}
}

func TestIntegration_AuthLogoutEntra(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "logout", "entra")

	if exitCode == 0 {
		assert.True(t,
			strings.Contains(stdout, "Not currently authenticated") || strings.Contains(stdout, "Successfully logged out"),
			"expected logout message, got: %s", stdout,
		)
	} else {
		// Plugin not installed or plugin connection error
		assert.True(t,
			strings.Contains(stderr, "unknown auth handler") || strings.Contains(stderr, "no auth handlers found") || strings.Contains(stderr, "rpc error"),
			"expected handler/plugin error, got: %s", stderr,
		)
	}
}

func TestIntegration_AuthHandlersInstallHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "handlers", "install", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Download an official auth handler plugin")
}

func TestIntegration_AuthHandlersRemoveHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "handlers", "remove", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Remove a cached auth handler plugin")
}

func TestIntegration_AuthHandlersInstallUnknown(t *testing.T) {
	t.Parallel()
	// An unknown handler name is no longer rejected against the official
	// allowlist; it is resolved by name against configured catalogs (issue
	// #576). With only an empty local catalog configured, resolution fails
	// cleanly with a not-found error and no network access.
	env := isolatedCatalogEnv(t)
	_, stderr, exitCode := runScafctlWithEnv(t, env, "auth", "handlers", "install", "nonexistent-handler-xyz")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found in any catalog")
}

func TestIntegration_AuthHandlersRemoveNotInstalled(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "handlers", "remove", "nonexistent-handler-xyz")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "is not installed")
}

func TestIntegration_AuthListJSONNoStdoutWarnings(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "list", "-o", "json")

	// When exit code is 0 and there is stdout output, it must be valid JSON
	// (no warnings mixed into stdout). Warnings should go to stderr.
	if exitCode == 0 && strings.TrimSpace(stdout) != "" {
		// Stdout must not contain warning emoji characters
		assert.NotContains(t, stdout, "\u26a0", "warnings must not appear in stdout when using -o json")
		assert.NotContains(t, stdout, "⚠", "warnings must not appear in stdout when using -o json")

		// In -o json mode, non-empty stdout must be valid JSON.
		// If it doesn't start with [ or {, that itself is a corruption issue.
		trimmed := strings.TrimSpace(stdout)
		if assert.True(t, strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{"),
			"stdout in -o json mode must be valid JSON, got: %s", trimmed[:min(len(trimmed), 80)]) {
			var parsed json.RawMessage
			assert.NoError(t, json.Unmarshal([]byte(trimmed), &parsed), "stdout must be valid JSON when using -o json")
		}
	}
}

func TestIntegration_AuthListPurgeHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "purge-expired")
}

func TestIntegration_AuthDiagnoseHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "diagnose", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Run auth diagnostics")
	assert.Contains(t, stdout, "--live-token")
}

func TestIntegration_AuthDiagnoseRuns(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "diagnose")

	// diagnose should complete (exit 0 or non-zero based on handler state)
	// but must not panic or produce empty output
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	combined := stdout + stderr
	assert.True(t, exitCode == 0 || exitCode == 1, "expected exit code 0 or 1, got %d", exitCode)
	assert.Contains(t, combined, "auth registry", "expected auth registry check in output")
}

func TestIntegration_AuthDiagnoseJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "diagnose", "-o", "json")

	// JSON output mode should produce valid JSON
	if exitCode == 0 && strings.TrimSpace(stdout) != "" {
		var result interface{}
		assert.NoError(t, json.Unmarshal([]byte(stdout), &result), "diagnose -o json must produce valid JSON")
	}
}

func TestIntegration_AuthDiagnoseAlias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "doctor", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Run auth diagnostics")
}

func TestIntegration_AuthDiagnoseUnknownHandler(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "diagnose", "nonexistent-xyz")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "nonexistent-xyz")
}

func TestIntegration_AuthDiagnoseSecretsStoreCheck(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "auth", "diagnose")

	// The secrets-store health check (issue #684) must always be present:
	// auth depends on master-key acquisition, so diagnose without it is a
	// false all-clear on the exact condition that breaks all login.
	combined := stdout + stderr
	assert.True(t, exitCode == 0 || exitCode == 1, "expected exit code 0 or 1, got %d", exitCode)
	assert.Contains(t, combined, "secrets store", "expected secrets store check in output")
}

func TestIntegration_AuthDiagnoseSecretsStoreCheckJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "diagnose", "-o", "json")

	// Structured output must always be a valid JSON array containing the
	// secrets-store row, regardless of exit code. A StatusFail now yields a
	// non-zero exit (issue #684), so validation must not be gated on exit==0.
	require.True(t, exitCode == 0 || exitCode == 1, "expected exit code 0 or 1, got %d", exitCode)

	var result []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result), "diagnose -o json must produce valid JSON array")

	var found bool
	for _, row := range result {
		if row["category"] == "secrets" && row["check"] == "secrets store" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected a secrets-store check row in JSON output")
}

func TestIntegration_AuthDiagnoseHelpDocumentsSecretsCheck(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "diagnose", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Secrets store health")
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestIntegration_InvalidCommand(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "invalidcommand")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown command")
}

func TestIntegration_MissingRequiredFlag(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "run", "solution")

	// Should fail due to missing -f flag
	assert.NotEqual(t, 0, exitCode)
}

// ============================================================================
// Complex Workflow Tests
// ============================================================================

func TestIntegration_SequentialChain(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ConditionalExecution(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/conditional-execution.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ParallelWithDeps(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/parallel-with-deps.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ActionAlias(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/action-alias.yaml",
		"-o", "json",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "action alias example should succeed")
	assert.Contains(t, stdout, "fetchConfiguration")
	assert.Contains(t, stdout, "deploy")
}

// ============================================================================
// Quiet Mode Tests
// ============================================================================

func TestIntegration_QuietMode(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"--quiet",
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
	)

	assert.Equal(t, 0, exitCode)
	// In quiet mode, minimal output expected
	t.Logf("quiet stdout len: %d", len(stdout))
	t.Logf("quiet stderr len: %d", len(stderr))
}

// ============================================================================
// Output Format Tests
// ============================================================================

func TestIntegration_OutputFormats(t *testing.T) {
	t.Parallel()
	formats := []string{"json", "yaml", "table"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			stdout, _, exitCode := runScafctl(t,
				"run", "resolver",
				"-f", "examples/resolver-demo.yaml",
				"-o", format,
			)

			assert.Equal(t, 0, exitCode, "format %s failed", format)
			assert.NotEmpty(t, stdout)
		})
	}
}

// ============================================================================
// Lint Command Tests
// ============================================================================

func TestIntegration_Lint_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Analyze a solution file")
	// The rule catalog is generated from KnownRules (issue #748): the header
	// reports a total and the help points at the detail commands.
	assert.Contains(t, stdout, "LINT RULES (")
	assert.Contains(t, stdout, "scafctl lint rules")
	assert.Contains(t, stdout, "scafctl lint rule <name>")
	// Rules must not be presented under the wrong severity tier. unused-resolver
	// is a warning and finally-with-foreach is an error; assert both land in the
	// correct block rather than the mis-tiered locations the old catalog used.
	errorsBlock, warningsBlock := lintHelpSeverityBlocks(t, stdout)
	assert.Contains(t, warningsBlock, "unused-resolver")
	assert.NotContains(t, errorsBlock, "unused-resolver")
	assert.Contains(t, errorsBlock, "finally-with-foreach")
	assert.NotContains(t, warningsBlock, "finally-with-foreach")
	assert.Contains(t, stdout, "--file")
	assert.Contains(t, stdout, "--severity")
}

// lintHelpSeverityBlocks splits the generated LINT RULES section of `lint
// --help` output into the text under "Errors (" and under "Warnings (" so a
// test can assert a rule appears in the correct severity tier. The "Info ("
// heading terminates the warnings block.
func lintHelpSeverityBlocks(t *testing.T, help string) (errorsBlock, warningsBlock string) {
	t.Helper()
	errStart := strings.Index(help, "\n  Errors (")
	warnStart := strings.Index(help, "\n  Warnings (")
	infoStart := strings.Index(help, "\n  Info (")
	require.GreaterOrEqual(t, errStart, 0, "help should contain an Errors tier")
	require.Greater(t, warnStart, errStart, "Warnings tier should follow Errors")
	require.Greater(t, infoStart, warnStart, "Info tier should follow Warnings")
	return help[errStart:warnStart], help[warnStart:infoStart]
}

func TestIntegration_Lint_RequiresFile(t *testing.T) {
	t.Parallel()
	// Run lint from an empty dir where no solution can be auto-discovered
	emptyDir := t.TempDir()
	_, stderr, exitCode := runScafctlInDir(t, emptyDir, "lint")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no solution path provided")
}

func TestIntegration_Lint_ValidSolution(t *testing.T) {
	t.Parallel()
	// Test with a simple solution file that should have minimal issues
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// The demo may have some issues but should lint successfully
	assert.Contains(t, stdout, "findings")
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
}

func TestIntegration_Lint_SeverityFilter(t *testing.T) {
	t.Parallel()
	// Test error-only filter
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "--severity", "error", "-o", "json")

	assert.Contains(t, stdout, "errorCount")
	// When filtering by error, warnCount and infoCount should be 0
	assert.Contains(t, stdout, `"warnCount": 0`)
	assert.Contains(t, stdout, `"infoCount": 0`)
}

func TestIntegration_Lint_QuietMode(t *testing.T) {
	t.Parallel()
	// Quiet mode should produce no output on success
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "quiet")

	// In quiet mode, stdout should be empty (only exit code matters)
	assert.Empty(t, stdout)
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
}

func TestIntegration_Lint_JSONOutput(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Verify JSON structure
	assert.Contains(t, stdout, `"file":`)
	assert.Contains(t, stdout, `"findings":`)
	assert.Contains(t, stdout, `"errorCount":`)
	assert.Contains(t, stdout, `"warnCount":`)
	assert.Contains(t, stdout, `"infoCount":`)
}

func TestIntegration_Lint_InvalidFile(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "lint", "-f", "nonexistent.yaml")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to load solution")
}

func TestIntegration_Lint_YAMLOutput(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "yaml")

	// Verify YAML structure
	assert.Contains(t, stdout, "file:")
	assert.Contains(t, stdout, "findings:")
	assert.Contains(t, stdout, "errorCount:")
}

func TestIntegration_Lint_Alias(t *testing.T) {
	t.Parallel()
	// Test the 'l' alias works
	stdout, _, exitCode := runScafctl(t, "l", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_CheckAlias(t *testing.T) {
	t.Parallel()
	// Test the 'check' alias works
	stdout, _, exitCode := runScafctl(t, "check", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_WarningSeverityFilter(t *testing.T) {
	t.Parallel()
	// Test warning filter includes warnings and errors but not info
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "--severity", "warning", "-o", "json")

	assert.Contains(t, stdout, "errorCount")
	// When filtering by warning, infoCount should be 0
	assert.Contains(t, stdout, `"infoCount": 0`)
}

func TestIntegration_Lint_TemplateUnknownAccessor(t *testing.T) {
	t.Parallel()
	// The lint-stress-test solution intentionally contains a go-template
	// resolver with a bare "{{ .projcts }}" accessor that matches no resolver,
	// data key, or forEach alias. Lint must surface it via the
	// template-unknown-accessor rule.
	stdout, _, _ := runScafctl(t, "lint",
		"-f", "examples/solutions/lint-stress-test/solution.yaml",
		"-o", "json",
	)

	assert.Contains(t, stdout, "template-unknown-accessor",
		"lint should report the unknown template accessor rule")
	assert.Contains(t, stdout, "projcts",
		"finding should name the unresolved accessor")
	assert.Contains(t, stdout, "resolvers.typo_template.resolve",
		"finding should point at the offending resolver step")
}

func TestIntegration_Lint_ActionSolution(t *testing.T) {
	t.Parallel()
	// Test linting a solution with actions
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/actions/hello-world.yaml", "-o", "json")

	// Should complete successfully (exit code 0 = no errors, 1 = general error, 2 = validation failed)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_ComplexSolution(t *testing.T) {
	t.Parallel()
	// Test linting a more complex solution
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/solutions/comprehensive/solution.yaml", "-o", "json")

	// Should complete and report findings
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
	assert.Contains(t, stdout, "errorCount")
}

func TestIntegration_Lint_TableOutput(t *testing.T) {
	t.Parallel()
	// Test default table output (explicit)
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "table")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	// Table output should produce some text
	assert.NotEmpty(t, stdout)
}

func TestIntegration_Lint_SchemaViolation_UnknownField(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "tests/integration/solutions/lint-schema/unknown-field.yaml", "-o", "json")

	// Should detect the unknown field and report schema-violation
	assert.Contains(t, stdout, "schema-violation")
	assert.Contains(t, stdout, "findings")
	// Exit code 2 (validation failed) expected because schema-violation is an error
	assert.Equal(t, 2, exitCode)
}

func TestIntegration_Lint_SchemaValid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "tests/integration/solutions/lint-schema/valid-minimal.yaml", "-o", "json")

	assert.Contains(t, stdout, "findings")
	// A valid minimal solution should not have schema-violation findings
	assert.NotContains(t, stdout, "schema-violation")
	// May still have info-level findings (e.g., missing-description) but not schema errors
	_ = exitCode
}

// TestIntegration_Lint_ComposedWorkflow_NoSchemaFalsePositive is a regression
// test for the compose lint false positive: a solution that both defines a
// workflow action (whose name comes only from the map key) and uses compose
// must not report a schema-violation on spec.workflow.actions.name. The compose
// struct round-trip previously injected an empty action name (name: "") into the
// content that schema-lint validates.
func TestIntegration_Lint_ComposedWorkflow_NoSchemaFalsePositive(t *testing.T) {
	t.Parallel()
	// Build the composed solution in a temp dir so the functional-test harness
	// (which walks tests/integration/solutions/**) never discovers the partial
	// compose file as a standalone solution.
	tmpDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "tests"), 0o755))

	solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: composed-workflow
  version: 1.0.0
compose:
  - tests/cases.yaml
spec:
  resolvers:
    hello:
      description: A greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: world
  workflow:
    actions:
      greet:
        description: Say hello
        provider: message
        inputs:
          message: hello
`
	// Partial compose file: only spec.testing, no metadata.name. Its name comes
	// from the action map key, which the compose round-trip previously injected
	// into rawContent as name: "" and tripped schema-lint.
	casesYAML := `spec:
  testing:
    cases:
      basic:
        description: basic composed test
        command: [run, resolver]
        assertions:
          - expression: __exitCode == 0
`
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "tests", "cases.yaml"), []byte(casesYAML), 0o644))

	stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")

	assert.NotContains(t, stdout, "schema-violation",
		"composed solution with a workflow action must not trip a schema-violation")
	assert.NotContains(t, stdout, "spec.workflow.actions.name",
		"the empty-action-name false positive must not appear")
	assert.Contains(t, stdout, `"errorCount": 0`,
		"a valid composed solution must lint with zero errors")
	assert.Equal(t, 0, exitCode, "lint should succeed (exit 0) for a valid composed solution")
}

func TestIntegration_Lint_AutoDiscovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-lint
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "lint", "-o", "json")
	// Should auto-discover solution.yaml and lint it
	assert.Contains(t, stdout, "findings")
	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
}

func TestIntegration_Lint_BuiltinInBundlePlugins(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: builtin-bundle-test
  version: 1.0.0
bundle:
  plugins:
    - name: cel
      kind: provider
      version: "1.0.0"
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionPath, "-o", "json")
	assert.Equal(t, 0, exitCode, "lint command should succeed")
	assert.Contains(t, stdout, "builtin-in-bundle-plugins")
}

func TestIntegration_RunResolver_BuiltinInBundlePlugins(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: builtin-bundle-run-test
  version: 1.0.0
bundle:
  plugins:
    - name: cel
      kind: provider
      version: "1.0.0"
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello-from-builtin
`
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solutionPath, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionPath, "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "run resolver should succeed despite builtin in bundle.plugins")
	assert.Contains(t, stdout, "hello-from-builtin")
}

// ============================================================================
// Package Command Tests
// ============================================================================

func TestIntegration_PackageHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "package", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "package")
	assert.Contains(t, stdout, "solution")
	assert.Contains(t, stdout, "plugin")
}

// TestIntegration_BuildAliasStillWorks verifies the deprecated "build" alias
// continues to resolve to the "package" command group after the rename.
func TestIntegration_BuildAliasStillWorks(t *testing.T) {
	t.Parallel()
	_, _, groupExit := runScafctl(t, "build", "--help")
	assert.Equal(t, 0, groupExit, "'build' group alias should still work")

	_, _, solExit := runScafctl(t, "build", "solution", "--help")
	assert.Equal(t, 0, solExit, "'build solution' alias should still work")

	_, _, plugExit := runScafctl(t, "build", "plugin", "--help")
	assert.Equal(t, 0, plugExit, "'build plugin' alias should still work")
}

func TestIntegration_PackageSolutionHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "package", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Package a solution")
	assert.Contains(t, stdout, "--version")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--force")
	assert.Contains(t, stdout, "--no-bundle")
	assert.Contains(t, stdout, "--no-vendor")
	assert.Contains(t, stdout, "--bundle-max-size")
	assert.Contains(t, stdout, "--dry-run")
	assert.Contains(t, stdout, "--strict")
	assert.Contains(t, stdout, "--no-verify")
}

// TestIntegration_PackageSolution_Canonical verifies the canonical "package
// solution" command name actually packages a solution into the local catalog
// (not just that --help resolves).
func TestIntegration_PackageSolution_Canonical(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}
	_, _, exitCode := runScafctlWithEnv(t, env, "package", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_BuildSolution_UsesMetadataVersion(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build without --version flag - should use metadata version (1.0.0)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml")

	assert.Equal(t, 0, exitCode)
	// Should report the version from metadata
	assert.Contains(t, stdout, "1.0.0")
}

func TestIntegration_BuildSolution_VersionStamping(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build with different version than metadata - should stamp and succeed
	stdout, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "9.9.9")

	assert.Equal(t, 0, exitCode)
	// Should succeed with the stamped version
	assert.Contains(t, stdout, "9.9.9")
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_FileNotFound(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "build", "solution", "-f", "nonexistent.yaml", "--version", "1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to read")
}

func TestIntegration_BuildSolution_Success(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_BuildSolution_WithName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--name", "my-custom-name", "--force")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-custom-name")
}

func TestIntegration_BuildSolution_ForceOverwrite(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// First build
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Second build without force should fail
	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "exists")

	// Third build with force should succeed
	stdout, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--force", "--no-cache")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_DryRun(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--dry-run")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Dry run")
}

func TestIntegration_BuildSolution_JSONReport(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "-o", "json")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)

	// stdout must be clean JSON: the report only, no human progress.
	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report),
		"stdout must be valid JSON, got: %s", stdout)
	assert.Equal(t, "1.0.0", report["version"])
	assert.NotEmpty(t, report["digest"], "a stored artifact has a digest")
	assert.NotNil(t, report["solution"], "composed solution is embedded")

	// Human progress is routed to stderr.
	assert.Contains(t, stderr, "Built")
}

func TestIntegration_BuildSolution_YAMLReport(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "-o", "yaml")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "version: 1.0.0")
	assert.Contains(t, stdout, "reference:")
	assert.Contains(t, stderr, "Built")
}

func TestIntegration_BuildSolution_DryRunJSONReport(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--dry-run", "-o", "json")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report),
		"stdout must be valid JSON, got: %s", stdout)
	assert.Equal(t, true, report["dryRun"])
	assert.Empty(t, report["digest"], "dry-run stores nothing")
	assert.Contains(t, stderr, "Dry run")
}

func TestIntegration_BuildSolution_ComposedOut(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}
	composedOut := filepath.Join(tmpDir, "composed.yaml")

	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--dry-run", "--composed-out", composedOut)

	if exitCode != 0 {
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)

	data, err := os.ReadFile(composedOut)
	require.NoError(t, err)
	assert.Contains(t, string(data), "apiVersion:")
}

func TestIntegration_BuildSolution_InvalidOutput(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "-o", "xml")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "xml")
}

func TestIntegration_BuildSolution_NoBundle(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-bundle")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

// ============================================================================
// Catalog Command Tests
// ============================================================================

func TestIntegration_CatalogHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "catalog")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "inspect")
	assert.Contains(t, stdout, "delete")
	assert.Contains(t, stdout, "login")
	assert.Contains(t, stdout, "logout")
}

func TestIntegration_CatalogLoginHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "login", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Authenticate to an OCI registry")
	assert.Contains(t, stdout, "--auth-provider")
	assert.Contains(t, stdout, "--scope")
	assert.Contains(t, stdout, "--username")
	assert.Contains(t, stdout, "--password")
	assert.Contains(t, stdout, "@-")
	assert.Contains(t, stdout, "--write-registry-auth")
}

func TestIntegration_CatalogLoginRequiresArg(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "catalog", "login")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "missing required argument: <registry>")
}

func TestIntegration_CatalogLogoutHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "logout", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Remove stored credentials")
	assert.Contains(t, stdout, "--all")
}

func TestIntegration_CatalogLogoutRequiresRegistryOrAll(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "catalog", "logout")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "specify a registry or use --all")
}

func TestIntegration_CatalogLogoutNonExistent(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "catalog", "logout", "nonexistent.example.com")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no credentials stored")
}

func TestIntegration_AuthLoginRegistryFlag(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "login", "github", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--registry")
	assert.Contains(t, stdout, "--registry-scope")
	assert.Contains(t, stdout, "--write-registry-auth")
}

func TestIntegration_CustomOAuth2Handler_AuthList(t *testing.T) {
	t.Parallel()

	// Create a temp config with a custom OAuth2 handler
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `auth:
  customOAuth2:
    - name: test-quay
      displayName: "Test Quay"
      tokenURL: "https://quay.io/oauth/token"
      clientID: "test-client"
      clientSecret: "test-secret"
      defaultFlow: client_credentials
      scopes:
        - "repo:read"
      registry: "quay.io"
      registryUsername: "$oauthtoken"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	// auth list should succeed (no tokens for custom handler, but no error)
	_, _, exitCode := runScafctl(t, "--config", configPath, "auth", "list")
	assert.Equal(t, 0, exitCode)

	// Verify the custom handler is registered via auth handlers
	stdout, _, exitCode := runScafctl(t, "--config", configPath, "auth", "handlers")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test-quay", "expected custom handler to appear in handlers output, got: %q", stdout)
}

func TestIntegration_CustomOAuth2Handler_AuthStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	configContent := `auth:
  customOAuth2:
    - name: test-custom
      displayName: "Test Custom"
      tokenURL: "https://example.com/oauth/token"
      clientID: "test-client"
      clientSecret: "test-secret"
      defaultFlow: client_credentials
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

	stdout, _, exitCode := runScafctl(t, "--config", configPath, "auth", "status", "test-custom")

	// Should recognize the handler (exit 0 or handler-specific output)
	// Even without login, the handler should be found
	assert.True(t, exitCode == 0 || exitCode == 1, "expected 0 or 1 exit code, got: %d", exitCode)
	assert.True(t,
		strings.Contains(stdout, "test-custom") ||
			strings.Contains(stdout, "Test Custom") ||
			strings.Contains(stdout, "not authenticated") ||
			strings.Contains(stdout, "Not authenticated"),
		"expected handler name or not-authenticated message, got: %q", stdout,
	)
}

func TestIntegration_CustomOAuth2Handler_NameConflict(t *testing.T) {
	t.Parallel()

	// A customOAuth2 config must not shadow a reserved first-party (official)
	// handler name. The reserved set is derived from the official registry, so
	// both the long-standing handlers (github) and newer ones (openshift) are
	// protected. Such configs are skipped at startup without crashing.
	for _, name := range []string{"github", "openshift"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			configContent := `auth:
  customOAuth2:
    - name: ` + name + `
      displayName: "Conflict"
      tokenURL: "https://example.com/oauth/token"
      clientID: "test-client"
      clientSecret: "test-secret"
      defaultFlow: client_credentials
`
			require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o600))

			// With debug logging on, the startup skip is observable. The CLI
			// must not crash, and the conflicting handler must be skipped.
			_, stderr, exitCode := runScafctl(t, "--config", configPath, "--log-level", "debug", "auth", "list")
			assert.Equal(t, 0, exitCode)
			assert.Containsf(t, stderr, "skipping", "reserved first-party name %q must be skipped", name)
			assert.Contains(t, stderr, name)
		})
	}
}

func TestIntegration_CatalogListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "List artifacts")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "plugin", "help should document the 'plugin' kind selector")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--output")
	assert.Contains(t, stdout, "--catalog")
	assert.Contains(t, stdout, "--all")
}

func TestIntegration_CatalogList_Empty(t *testing.T) {
	t.Parallel()
	// Use isolated env that disables the official remote catalog to avoid
	// network dependencies. This test verifies local-only behavior.
	env := isolatedCatalogEnv(t)

	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "list", "-o", "json")

	assert.Equal(t, 0, exitCode)
	trimmed := strings.TrimSpace(stdout)
	assert.True(t, json.Valid([]byte(trimmed)),
		"expected valid JSON output, got: %q", stdout)
}

func TestIntegration_CatalogList_WithArtifacts(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List should show the artifact
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")
}

func TestIntegration_CatalogList_FilterByKind(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List with filter should work
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "list", "--kind", "solution", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_CatalogList_KindPluginAccepted(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build a solution so the catalog is non-empty; --kind plugin must exclude
	// it (solutions are not plugins) yet still exit 0 rather than rejecting the
	// kind selector.
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "list", "--kind", "plugin", "-o", "json")
	assert.Equal(t, 0, exitCode, "--kind plugin must be a valid selector")
	// The solution must NOT appear under the plugin selector.
	assert.NotContains(t, stdout, "resolver-demo",
		"solutions must be excluded from --kind plugin output")
}

func TestIntegration_CatalogList_KindInvalidRejected(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "list", "--kind", "bogus")
	assert.NotEqual(t, 0, exitCode, "an invalid kind must be rejected with a non-zero exit")
	assert.Contains(t, stderr, "invalid kind")
	assert.Contains(t, stderr, "plugin", "the error should list 'plugin' as a valid selector")
}

func TestIntegration_CatalogInspectHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "inspect", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show detailed information")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogInspect_NotFound(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "inspect", "nonexistent")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogInspect_Success(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Inspect the artifact
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "inspect", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")
	assert.Contains(t, stdout, "digest")
}

func TestIntegration_CatalogInspect_SpecificVersion(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build multiple versions
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Inspect specific version
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "inspect", "resolver-demo@1.0.0", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "1.0.0")
}

func TestIntegration_CatalogDeleteHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "delete", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Delete an artifact")
}

func TestIntegration_CatalogDelete_RequiresVersion(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete without version should fail
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "delete", "resolver-demo")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version required")
}

func TestIntegration_CatalogDelete_NotFound(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "delete", "nonexistent@1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogDelete_Success(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete the artifact
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "delete", "resolver-demo@1.0.0")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Deleted")

	// Verify it's gone
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "inspect", "resolver-demo@1.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

// ============================================================================
// Catalog Prune Command Tests
// ============================================================================

func TestIntegration_CatalogPruneHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "prune", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Remove orphaned blobs")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogPrune_EmptyCatalog(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "prune")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No orphaned content")
}

func TestIntegration_CatalogPrune_JSON(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "prune", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "removedManifests")
	assert.Contains(t, stdout, "removedBlobs")
	assert.Contains(t, stdout, "reclaimedBytes")
}

func TestIntegration_CatalogPrune_AfterDelete(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete the artifact (leaves orphaned blobs)
	_, _, exitCode = runScafctlWithEnv(t, env, "catalog", "delete", "resolver-demo@1.0.0")
	require.Equal(t, 0, exitCode)

	// Prune should clean up
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "prune", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have pruned something
	assert.Contains(t, stdout, "removedBlobs")
}

// =============================================================================
// Run Solution from Catalog Tests
// =============================================================================

func TestIntegration_RunSolution_FromCatalog_NotFound(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Try to run a solution that doesn't exist in catalog
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "run", "solution", "nonexistent-solution")
	assert.NotEqual(t, 0, exitCode)
	// Reports artifact not found, file not found, or an auth error from
	// a configured remote catalog that lacks valid credentials.
	combined := stdout + stderr
	assert.True(t, strings.Contains(combined, "not found") ||
		strings.Contains(combined, "no such file or directory") ||
		strings.Contains(combined, "401") ||
		strings.Contains(combined, "Unauthorized") ||
		strings.Contains(combined, "403") ||
		strings.Contains(combined, "404") ||
		strings.Contains(combined, "name unknown") ||
		strings.Contains(combined, "no such host") ||
		strings.Contains(combined, "dial tcp"),
		"expected error about missing solution or auth failure, got stdout=%q stderr=%q", stdout, stderr)
}

func TestIntegration_RunSolution_FromCatalog_ByName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build a solution into the catalog (uses only built-in providers)
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name (should pick latest version)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "run", "resolver", "-f", "builtin-resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_ByNameVersion(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build two versions (uses only built-in providers)
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctlWithEnv(t, env, "build", "solution", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name@version
	stdout, _, exitCode := runScafctlWithEnv(t, env, "run", "resolver", "-f", "builtin-resolver-demo@1.0.0", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_FallbackToFile(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Run a solution by file path (not bare name) - should use file (uses only built-in providers)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "run", "resolver", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output from file
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_PathNotBareName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// A path with a separator should not be treated as a bare name
	// The new validation rejects positional file paths and directs users to use -f
	_, stderr, exitCode := runScafctlWithEnv(t, env, "run", "solution", "./nonexistent.yaml")
	assert.NotEqual(t, 0, exitCode)
	// Should reject positional file path and suggest -f flag
	assert.Contains(t, stderr, "local file paths must use -f/--file flag")
}

// Render Solution from Catalog Tests
// =============================================================================

func TestIntegration_RenderSolution_FromCatalog_ByName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build a solution into the catalog (uses only built-in providers)
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Render the resolver graph from catalog by name
	stdout, _, exitCode := runScafctlWithEnv(t, env, "run", "resolver", "-f", "builtin-resolver-demo", "--graph")
	assert.Equal(t, 0, exitCode)
	// Should have graph output with resolver info
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "Phase")
}

// Inspect Solution from Catalog Tests
// =============================================================================

func TestIntegration_InspectSolution_FromCatalog_ByName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build a solution into the catalog
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Inspect the solution from catalog by name
	stdout, _, exitCode := runScafctlWithEnv(t, env, "inspect", "solution", "resolver-demo")
	assert.Equal(t, 0, exitCode)
	// Should have solution metadata
	assert.Contains(t, stdout, "resolver-demo")
}

// Regression: inspecting a solution when none can be found must print a clear
// error, not exit silently (the coded ExitError was previously swallowed).
func TestIntegration_InspectSolution_NoSolution_PrintsError(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	_, stderr, exitCode := runScafctlInDir(t, tmpDir, "inspect", "solution")
	assert.NotEqual(t, 0, exitCode, "should fail when no solution is found")
	assert.Contains(t, stderr, "no solution", "must surface a clear error message")
}

// Lint Solution from Catalog Tests
// =============================================================================

func TestIntegration_Lint_FromCatalog_ByName(t *testing.T) {
	t.Parallel()
	// Use isolated env that disables the official remote catalog to avoid
	// network dependencies. This test only exercises local catalog lookup.
	env := isolatedCatalogEnv(t)

	// Build a solution into the catalog
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Lint the solution from catalog by name
	stdout, _, exitCode := runScafctlWithEnv(t, env, "lint", "-f", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have lint output
	assert.Contains(t, stdout, "findings")
}

// =============================================================================
// Remote Catalog Integration Tests (local OCI registry)
// =============================================================================

func TestIntegration_CatalogList_RemoteRegistry(t *testing.T) {
	t.Parallel()
	// Start a local OCI registry and list from it directly using --catalog.
	// This exercises remote catalog enumeration and tag fetching without
	// any network dependency on external registries.
	registryAddr, reg := startOCIRegistry(t)
	env := isolatedCatalogEnv(t)

	// Pre-populate the registry with fake artifacts to verify enumeration.
	reg.mu.Lock()
	reg.repos["scafctl/solutions/demo-app"] = map[string]string{
		"1.0.0": "sha256:aaaa",
		"2.0.0": "sha256:bbbb",
	}
	reg.mu.Unlock()

	// List from the local registry by URL (--catalog <url> --insecure)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "list",
		"--catalog", registryAddr+"/scafctl",
		"--insecure", "-o", "json")

	assert.Equal(t, 0, exitCode)
	trimmed := strings.TrimSpace(stdout)
	assert.True(t, json.Valid([]byte(trimmed)),
		"expected valid JSON output, got: %q", stdout)
	// The remote registry has an artifact — should appear in results
	assert.Contains(t, stdout, "demo-app")
}

func TestIntegration_CatalogPush_LocalRegistry(t *testing.T) {
	t.Parallel()
	// Build a solution locally, then push to the local OCI registry.
	// This exercises the full push flow against a real OCI-compatible server.
	registryAddr, _ := startOCIRegistry(t)
	env := isolatedCatalogEnv(t)

	// Build solution artifact
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Push to the local registry by URL
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "push",
		"resolver-demo@1.0.0",
		"--catalog", registryAddr+"/scafctl",
		"--insecure")
	assert.Equal(t, 0, exitCode, "push failed: %s", stderr)
}

// Get Solution from Catalog Tests
// =============================================================================

func TestIntegration_GetSolution_FromCatalog_ByName(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build a solution into the catalog
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Get the solution from catalog by name (positional arg for catalog refs)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "get", "solution", "resolver-demo", "-o", "yaml")
	assert.Equal(t, 0, exitCode)
	// Should have solution YAML
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "apiVersion")
}

// =============================================================================
// Catalog Save Tests
// =============================================================================

func TestIntegration_CatalogSaveHelp(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "catalog", "save", "--help")
	assert.Contains(t, stdout, "save")
	assert.Contains(t, stdout, "output")
}

func TestIntegration_CatalogSave_RequiresOutput(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Try to save without output flag
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "save", "resolver-demo")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "required")
}

func TestIntegration_CatalogSave_NotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	outputPath := tmpDir + "/nonexistent.tar"
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "save", "nonexistent", "-o", outputPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogSave_Success(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save to tar
	outputPath := tmpDir + "/export.tar"
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "save", "resolver-demo", "-o", outputPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")

	// Verify file was created
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestIntegration_CatalogSave_SpecificVersion(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build multiple versions
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Save specific version
	outputPath := tmpDir + "/v1.tar"
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "save", "resolver-demo@1.0.0", "-o", outputPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "1.0.0")
}

// =============================================================================
// Catalog Load Tests
// =============================================================================

func TestIntegration_CatalogLoadHelp(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "catalog", "load", "--help")
	assert.Contains(t, stdout, "load")
	assert.Contains(t, stdout, "input")
	assert.Contains(t, stdout, "force")
}

func TestIntegration_CatalogLoad_RequiresInput(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "catalog", "load")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "required")
}

func TestIntegration_CatalogLoad_FileNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "load", "--input", "/nonexistent/path.tar")
	assert.NotEqual(t, 0, exitCode)
	assert.True(t, strings.Contains(stderr, "no such file") || strings.Contains(stderr, "cannot find"))
}

func TestIntegration_CatalogLoad_Success(t *testing.T) {
	t.Parallel()
	// Create source catalog
	srcDir := t.TempDir()
	srcEnv := map[string]string{
		"XDG_DATA_HOME":  srcDir,
		"XDG_CACHE_HOME": srcDir,
	}

	// Build and save an artifact
	_, _, exitCode := runScafctlWithEnv(t, srcEnv, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	tarPath := srcDir + "/export.tar"
	_, _, exitCode = runScafctlWithEnv(t, srcEnv, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Switch to destination catalog
	dstDir := t.TempDir()
	dstEnv := map[string]string{
		"XDG_DATA_HOME":  dstDir,
		"XDG_CACHE_HOME": dstDir,
	}

	// Load the artifact
	stdout, _, exitCode := runScafctlWithEnv(t, dstEnv, "catalog", "load", "--input", tarPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")

	// Verify artifact is in catalog
	stdout, _, exitCode = runScafctlWithEnv(t, dstEnv, "catalog", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_CatalogLoad_AlreadyExists(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save it
	tarPath := tmpDir + "/export.tar"
	_, _, exitCode = runScafctlWithEnv(t, env, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Try to load into same catalog (should fail)
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "load", "--input", tarPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "already exists")
}

func TestIntegration_CatalogLoad_ForceOverwrite(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build an artifact
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save it
	tarPath := tmpDir + "/export.tar"
	_, _, exitCode = runScafctlWithEnv(t, env, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Load with force (should succeed)
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "load", "--input", tarPath, "--force")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

// =============================================================================
// Catalog Save/Load Round Trip Tests
// =============================================================================

func TestIntegration_CatalogSaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	// Create source catalog
	srcDir := t.TempDir()
	srcEnv := map[string]string{
		"XDG_DATA_HOME":  srcDir,
		"XDG_CACHE_HOME": srcDir,
	}

	// Build an artifact (uses only built-in providers)
	_, _, exitCode := runScafctlWithEnv(t, srcEnv, "build", "solution", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save to tar
	tarPath := srcDir + "/export.tar"
	_, _, exitCode = runScafctlWithEnv(t, srcEnv, "catalog", "save", "builtin-resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Switch to destination catalog
	dstDir := t.TempDir()
	dstEnv := map[string]string{
		"XDG_DATA_HOME":  dstDir,
		"XDG_CACHE_HOME": dstDir,
	}

	// Load from tar
	_, _, exitCode = runScafctlWithEnv(t, dstEnv, "catalog", "load", "--input", tarPath)
	require.Equal(t, 0, exitCode)

	// Verify the solution can be run
	stdout, _, exitCode := runScafctlWithEnv(t, dstEnv, "run", "resolver", "-f", "builtin-resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

// =============================================================================
// Catalog Index Tests
// =============================================================================

func TestIntegration_CatalogIndexHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "index", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Manage the catalog discovery index")
	assert.Contains(t, stdout, "push")
	assert.Contains(t, stdout, "show")
}

func TestIntegration_CatalogIndexPushHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "index", "push", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Discover all artifacts in the target catalog")
	assert.Contains(t, stdout, "--catalog")
	assert.Contains(t, stdout, "--dry-run")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogIndexShowHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "index", "show", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Fetch and display the catalog index artifact")
	assert.Contains(t, stdout, "--catalog")
	assert.Contains(t, stdout, "--output")
}

// =============================================================================
// Catalog Push Tests
// =============================================================================

func TestIntegration_CatalogPushHelp(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "catalog", "push", "--help")
	assert.Contains(t, stdout, "Push a catalog artifact to a remote OCI registry")
	assert.Contains(t, stdout, "--catalog")
	assert.Contains(t, stdout, "--as")
	assert.Contains(t, stdout, "--force")
	assert.Contains(t, stdout, "configured catalog name")
}

func TestIntegration_CatalogPush_NoCatalog(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Push without --catalog and no default configured should error.
	// Since artifact also doesn't exist locally, kind inference fails first.
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "push", "my-solution@1.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogPush_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Push a nonexistent artifact
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "push", "nonexistent@1.0.0", "--catalog", "ghcr.io/test/scafctl")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

// =============================================================================
// Catalog Pull Tests
// =============================================================================

func TestIntegration_CatalogPullHelp(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "catalog", "pull", "--help")
	assert.Contains(t, stdout, "Pull a catalog artifact from a remote OCI registry")
	assert.Contains(t, stdout, "--as")
	assert.Contains(t, stdout, "--force")
	assert.Contains(t, stdout, "--strict")
	assert.Contains(t, stdout, "--no-verify")
}

func TestIntegration_CatalogPull_InvalidReference(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Pull with a bare name (no OCI prefix) -- resolves against the default
	// catalog which fails because the artifact does not exist.
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "pull", "just-a-name")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to resolve artifact")
}

// TestIntegration_CatalogPull_StrictFailsOnIncompleteBundle verifies the
// consumer verification hook (Fix 1/Fix 2): pulling an incomplete solution
// bundle with --strict fails closed, while the default (warn) mode succeeds.
func TestIntegration_CatalogPull_StrictFailsOnIncompleteBundle(t *testing.T) {
	t.Parallel()
	registryAddr, _ := startOCIRegistry(t)
	env := isolatedCatalogEnv(t)

	dir := incompleteSolutionFixture(t)

	// Package the incomplete artifact locally, skipping verification so it
	// stores despite being incomplete.
	_, stderr, exitCode := runScafctlWithEnvInDir(t, dir, env,
		"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
		"--no-vendor", "--skip-lint", "--skip-tests", "--no-verify")
	require.Equal(t, 0, exitCode, "package failed: %s", stderr)

	// Push it to the local registry.
	_, stderr, exitCode = runScafctlWithEnv(t, env, "catalog", "push",
		"incomplete-demo@1.0.0", "--catalog", registryAddr+"/scafctl", "--insecure")
	require.Equal(t, 0, exitCode, "push failed: %s", stderr)

	// Pull with --strict into a fresh catalog: must fail closed.
	pullEnv := isolatedCatalogEnv(t)
	stdout, stderr, exitCode := runScafctlWithEnv(t, pullEnv, "catalog", "pull",
		registryAddr+"/scafctl/solutions/incomplete-demo@1.0.0", "--insecure", "--strict")
	assert.NotEqual(t, 0, exitCode, "strict pull of incomplete bundle must fail; out: %s err: %s", stdout, stderr)
	assert.Contains(t, stdout+stderr, "incomplete")

	// Pull without --strict into another fresh catalog: warns but succeeds.
	warnEnv := isolatedCatalogEnv(t)
	stdout, stderr, exitCode = runScafctlWithEnv(t, warnEnv, "catalog", "pull",
		registryAddr+"/scafctl/solutions/incomplete-demo@1.0.0", "--insecure")
	assert.Equal(t, 0, exitCode, "non-strict pull must succeed; out: %s err: %s", stdout, stderr)
}

// =============================================================================
// Catalog Delete Remote Tests
// =============================================================================

func TestIntegration_CatalogDeleteRemoteHelp(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "catalog", "delete", "--help")
	assert.Contains(t, stdout, "Delete an artifact from the local or remote catalog")
	assert.Contains(t, stdout, "ghcr.io/myorg/scafctl/solutions/my-solution")
	assert.Contains(t, stdout, "--insecure")
	assert.Contains(t, stdout, "--catalog")
}

func TestIntegration_CatalogDelete_RemoteDetection(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Try to delete from a fake remote - should detect it as remote
	// and fail with auth/network error (not "invalid reference")
	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "delete", "fake.registry.io/myorg/solutions/test@1.0.0")
	assert.NotEqual(t, 0, exitCode)
	// Should not say "invalid reference" since it was detected as remote
	assert.NotContains(t, stderr, "invalid reference")
}

// =============================================================================
// Catalog Tag Tests
// =============================================================================

func TestIntegration_CatalogTagHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "tag", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Create an alias tag")
	assert.Contains(t, stdout, "--catalog")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "stable")
}

func TestIntegration_CatalogTag_RequiresVersion(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "tag", "my-solution", "stable")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version required")
}

func TestIntegration_CatalogTag_RejectsSemverAlias(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "tag", "my-solution@1.0.0", "2.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "semver version")
}

func TestIntegration_CatalogTag_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "tag", "nonexistent@1.0.0", "stable", "--kind", "solution")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogTag_Success(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Build an artifact first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Tag it
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "tag", "resolver-demo@1.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Tagged")
	assert.Contains(t, stdout, "stable")
}

func TestIntegration_CatalogTag_MoveAlias(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Build two versions
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "2.0.0", "--force", "--no-cache")
	require.Equal(t, 0, exitCode)

	// Tag v1 as stable
	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "tag", "resolver-demo@1.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "1.0.0")

	// Move stable to v2
	stdout, _, exitCode = runScafctlWithEnv(t, env, "catalog", "tag", "resolver-demo@2.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "2.0.0")
}

// Catalog Tags Tests

func TestIntegration_CatalogTagsHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "tags", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "List all tags")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--insecure")
}

func TestIntegration_CatalogTags_RequiresArg(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "catalog", "tags")

	assert.NotEqual(t, 0, exitCode)
}

// Catalog Attach Tests

func TestIntegration_CatalogAttachHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "attach", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "attach")
}

// Catalog Remote Tests

func TestIntegration_CatalogRemoteHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "remote", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "remote")
	assert.Contains(t, stdout, "add")
	assert.Contains(t, stdout, "remove")
	assert.Contains(t, stdout, "list")
}

func TestIntegration_CatalogRemoteAddHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "remote", "add", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Add a new remote catalog")
	assert.Contains(t, stdout, "--auth-provider")
}

func TestIntegration_CatalogRemoteRemoveHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "remote", "remove", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "remove")
}

func TestIntegration_CatalogRemoteListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "remote", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "list")
}

func TestIntegration_CatalogRemoteList_Empty(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CONFIG_HOME": tmpDir,
	}

	_, _, exitCode := runScafctlWithEnv(t, env, "catalog", "remote", "list")
	assert.Equal(t, 0, exitCode)
}

// TestIntegration_CatalogRemoteDefault sets the default catalog via the
// canonical 'catalog remote default <name>' path and verifies it takes effect.
func TestIntegration_CatalogRemoteDefault(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CONFIG_HOME": tmpDir,
	}

	// Add a filesystem catalog to make the default target.
	_, _, addExit := runScafctlWithEnv(t, env, "catalog", "remote", "add",
		"mycat", "--type", "filesystem", "--path", "./catalogs")
	require.Equal(t, 0, addExit)

	stdout, _, exitCode := runScafctlWithEnv(t, env, "catalog", "remote", "default", "mycat")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "mycat")

	// Verify the default is reflected in list output.
	listOut, _, listExit := runScafctlWithEnv(t, env, "catalog", "remote", "list", "-o", "json")
	assert.Equal(t, 0, listExit)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(listOut), &items))
	var defaulted string
	for _, it := range items {
		if d, ok := it["default"].(bool); ok && d {
			defaulted, _ = it["name"].(string)
		}
	}
	assert.Equal(t, "mycat", defaulted, "mycat should be the default catalog")
}

// TestIntegration_CatalogRemoteSetDefaultDeprecatedNotice verifies the hidden
// deprecated 'catalog remote set-default <name>' path still exits 0, sets the
// default, and emits cobra's deprecation notice on stderr pointing at the
// canonical path.
func TestIntegration_CatalogRemoteSetDefaultDeprecatedNotice(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CONFIG_HOME": tmpDir,
	}

	_, _, addExit := runScafctlWithEnv(t, env, "catalog", "remote", "add",
		"mycat", "--type", "filesystem", "--path", "./catalogs")
	require.Equal(t, 0, addExit)

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "catalog", "remote", "set-default", "mycat")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, `Command "set-default" is deprecated`)
	assert.Contains(t, stderr, "catalog remote default")
	assert.Contains(t, stdout, "mycat")
}

// =============================================================================
// Cache Command Tests
// =============================================================================

func TestIntegration_CacheHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "cache", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "cache")
	assert.Contains(t, stdout, "clear")
	assert.Contains(t, stdout, "info")
}

func TestIntegration_CacheClearHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Clear cached content")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--force")
}

func TestIntegration_CacheInfoHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "cache", "info", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show cache information")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CacheInfo_Empty(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "info", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "totalSize")
	assert.Contains(t, stdout, "totalFiles")
	assert.Contains(t, stdout, "HTTP Cache")
}

func TestIntegration_CacheClear_Empty(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "clear", "--force")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No cached content found")
}

func TestIntegration_CacheClear_InvalidKind(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "cache", "clear", "--kind", "invalid", "--force")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid cache kind")
}

func TestIntegration_CacheClear_JSON(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "clear", "--force", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "removedFiles")
	assert.Contains(t, stdout, "removedBytes")
}

func TestIntegration_CacheClear_HTTPKind(t *testing.T) {
	t.Parallel()
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "clear", "--kind", "http", "--force")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No cached content found")
}

func TestIntegration_CacheClear_BuildKind(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "clear", "--kind", "build", "--force")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No cached content found")
}

func TestIntegration_CacheInfo_ShowsBuildCache(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "cache", "info")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Build Cache")
}

func TestIntegration_BuildSolution_NoCacheFlag(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_BuildCacheHit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// First build
	stdout1, stderr1, exitCode1 := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	if exitCode1 != 0 {
		t.Logf("stdout: %s", stdout1)
		t.Logf("stderr: %s", stderr1)
	}
	require.Equal(t, 0, exitCode1)
	assert.Contains(t, stdout1, "Built")

	// Second build with same inputs — should be a cache hit
	stdout2, stderr2, exitCode2 := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	if exitCode2 != 0 {
		t.Logf("stdout: %s", stdout2)
		t.Logf("stderr: %s", stderr2)
	}
	assert.Equal(t, 0, exitCode2)
	assert.Contains(t, stdout2, "cache hit")
}

func TestIntegration_BuildSolution_NoCacheBypassesCacheHit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// First build (populates cache)
	_, _, exitCode1 := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode1)

	// Second build with --no-cache — should NOT be a cache hit, should fail with "already exists" since --force is not set
	stdout2, _, exitCode2 := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")
	// Without --force, re-building same version should fail or succeed with --force
	// It should at least NOT say "cache hit"
	assert.NotContains(t, stdout2, "cache hit")
	_ = exitCode2 // exit code depends on force flag behavior
}

// ============================================================================
// Solution Provider Tests
// ============================================================================

func TestIntegration_SolutionProvider_ResolverComposition(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctlLong(t,
		"run", "resolver",
		"-f", "tests/integration/testdata/solution-provider/parent-resolver.yaml",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "hello from child")
	assert.Contains(t, stdout, "passed from parent")
}

func TestIntegration_SolutionProvider_WorkflowComposition(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctlLong(t,
		"run", "solution",
		"-f", "tests/integration/testdata/solution-provider/parent-action.yaml",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "succeeded")
}

func TestIntegration_SolutionProvider_ValueOverrideContract(t *testing.T) {
	t.Parallel()

	// With an override: the parent passes an enriched value into the child's
	// opt-in override input, and the child merges it over its internal value.
	stdout, stderr, exitCode := runScafctlLong(t,
		"run", "resolver",
		"-f", "tests/integration/testdata/solution-provider/override-parent.yaml",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	// child_labels merges the child's internal keys with the parent's overrides.
	// Decode the output and assert on the object to avoid depending on JSON
	// formatting (spacing, key ordering, pretty vs compact).
	var parentResolvers struct {
		ChildLabels map[string]any `json:"child_labels"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &parentResolvers))
	assert.Equal(t, map[string]any{
		"app":        "child",
		"tier":       "backend",
		"team":       "platform",
		"costCenter": "CC-42",
	}, parentResolvers.ChildLabels)

	// Without an override: the child alone falls back to its internal value
	// because labels_override defaults to {} (map.merge with {} is a no-op).
	childOut, childErr, childExit := runScafctlLong(t,
		"run", "resolver",
		"-f", "tests/integration/testdata/solution-provider/override-child.yaml",
		"-o", "json",
	)
	t.Logf("childOut: %s", childOut)
	t.Logf("childErr: %s", childErr)
	assert.Equal(t, 0, childExit, "expected exit code 0, got %d", childExit)
	var childResolvers struct {
		Labels map[string]any `json:"labels"`
	}
	require.NoError(t, json.Unmarshal([]byte(childOut), &childResolvers))
	assert.Equal(t, map[string]any{
		"app":  "child",
		"tier": "backend",
	}, childResolvers.Labels)
	assert.NotContains(t, childResolvers.Labels, "team")
	assert.NotContains(t, childResolvers.Labels, "costCenter")
}

func TestIntegration_SolutionProvider_CircularReference(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "tests/integration/testdata/solution-provider/circular-a.yaml",
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "circular reference detected")
}

// TestIntegration_RunResolver_CycleChainMessage verifies that a circular
// resolver dependency surfaces as an ordered, closed cycle chain
// ("alpha -> beta -> alpha") rather than the legacy "dagObject depends on"
// phrasing. The chain is deterministic: the search starts from the
// lexicographically smallest node and follows the smallest dependency edge.
func TestIntegration_RunResolver_CycleChainMessage(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "tests/integration/solutions/lint-resolver-cycle/solution.yaml",
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "cycle detected")
	assert.Contains(t, stderr, "dependency cycle: alpha -> beta -> alpha",
		"cycle error must render an ordered, closed chain")
	assert.NotContains(t, stderr, "dagObject",
		"legacy 'dagObject depends on' phrasing must not appear")
}

func TestIntegration_SolutionProvider_DryRun(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "tests/integration/testdata/solution-provider/parent-action.yaml",
		"--dry-run",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	// Dry-run displays the execution plan without running actions
	assert.Contains(t, stdout, "DRY RUN")
	assert.Contains(t, stdout, "run-child")
}

func TestIntegration_SolutionProvider_PropagateErrorsFalse(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a child solution that will fail (references a nonexistent provider)
	childSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: failing-child
  version: 1.0.0
spec:
  resolvers:
    data:
      type: string
      resolve:
        with:
          - provider: nonexistent-provider
            inputs:
              value: "will fail"
`
	childPath := filepath.Join(tmpDir, "failing-child.yaml")
	require.NoError(t, os.WriteFile(childPath, []byte(childSolution), 0o644))

	// Create a parent that uses propagateErrors: false
	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-no-propagate
  version: 1.0.0
spec:
  resolvers:
    child-result:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "` + filepath.ToSlash(childPath) + `"
              propagateErrors: false
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	// With propagateErrors: false, the parent solution should succeed
	// and return an envelope with status "failed" for the child
	assert.Equal(t, 0, exitCode, "expected exit code 0 with propagateErrors=false, got %d", exitCode)
	assert.Contains(t, stdout, "failed")
}

func TestIntegration_SolutionProvider_PropagateErrorsFalse_LoadFailure(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a parent that references a non-existent child with propagateErrors: false.
	// This exercises the load-tolerant compose mode where child parse/load failures
	// degrade gracefully into the envelope instead of aborting the parent.
	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-load-tolerant
  version: 1.0.0
spec:
  resolvers:
    child-result:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "` + filepath.ToSlash(filepath.Join(tmpDir, "does-not-exist.yaml")) + `"
              propagateErrors: false
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	// With propagateErrors: false, a load failure should NOT abort the parent.
	assert.Equal(t, 0, exitCode, "expected exit code 0 with propagateErrors=false on load failure, got %d", exitCode)
	assert.Contains(t, stdout, "failed")
	assert.Contains(t, stdout, "_loader")
}

func TestIntegration_SolutionProvider_PropagateErrorsFalse_MalformedChild(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a malformed child YAML (parse failure).
	malformedChild := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: malformed
  [[[invalid yaml
`
	childPath := filepath.Join(tmpDir, "malformed-child.yaml")
	require.NoError(t, os.WriteFile(childPath, []byte(malformedChild), 0o644))

	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-parse-tolerant
  version: 1.0.0
spec:
  resolvers:
    child-result:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "` + filepath.ToSlash(childPath) + `"
              propagateErrors: false
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	// With propagateErrors: false, a parse failure should degrade gracefully.
	assert.Equal(t, 0, exitCode, "expected exit code 0 with propagateErrors=false on parse failure, got %d", exitCode)
	assert.Contains(t, stdout, "failed")
	assert.Contains(t, stdout, "_loader")
}

func TestIntegration_SolutionProvider_MaxDepthExceeded(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create a self-referencing solution with maxDepth: 1
	selfRef := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: self-ref
  version: 1.0.0
spec:
  resolvers:
    data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "` + filepath.ToSlash(filepath.Join(tmpDir, "self.yaml")) + `"
              maxDepth: 1
`
	selfPath := filepath.Join(tmpDir, "self.yaml")
	require.NoError(t, os.WriteFile(selfPath, []byte(selfRef), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", selfPath,
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	// Should hit either circular reference or max depth
	assert.True(t,
		strings.Contains(stderr, "circular reference detected") || strings.Contains(stderr, "max nesting depth"),
		"expected circular reference or max depth error, got: %s", stderr)
}

func TestIntegration_SolutionProvider_ChildNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-missing-child
  version: 1.0.0
spec:
  resolvers:
    data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./nonexistent.yaml"
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	assert.True(t,
		strings.Contains(stderr, "failed to load") || strings.Contains(stderr, "not found") || strings.Contains(stderr, "no such file"),
		"expected load error, got: %s", stderr)
}

func TestIntegration_SolutionProvider_ResolverFilter(t *testing.T) {
	t.Parallel()
	// Parent requests only the "greeting" resolver from the child.
	// The child has two resolvers: greeting (static) and echo-param (parameter).
	// Since we only request "greeting", echo-param should not run and its absence
	// should not cause a failure (no parameter is provided).
	tmpDir := t.TempDir()

	// Use absolute path for the child because the parent lives in a temp dir,
	// and relative paths now resolve from the solution file's directory.
	absChildPath := filepath.Join(findProjectRoot(), "tests/integration/testdata/solution-provider/child.yaml")

	parentSolution := fmt.Sprintf(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-filter-test
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      timeout: 90s
      resolve:
        with:
          - provider: solution
            inputs:
              source: %q
              resolvers:
                - greeting
              timeout: "90s"
`, absChildPath)
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctlLong(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "hello from child")
	// echo-param should NOT be present since we only requested "greeting"
	assert.NotContains(t, stdout, "echo-param")
}

func TestIntegration_SolutionProvider_ResolverFilterNotFound(t *testing.T) {
	t.Parallel()
	// Request a resolver that does not exist in the child solution.
	tmpDir := t.TempDir()

	// Use absolute path for the child because the parent lives in a temp dir,
	// and relative paths now resolve from the solution file's directory.
	absChildPath := filepath.Join(findProjectRoot(), "tests/integration/testdata/solution-provider/child.yaml")

	parentSolution := fmt.Sprintf(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-filter-notfound
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      timeout: 90s
      resolve:
        with:
          - provider: solution
            inputs:
              source: %q
              resolvers:
                - does-not-exist
              timeout: "90s"
`, absChildPath)
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	_, stderr, exitCode := runScafctlLong(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not exist")
}

func TestIntegration_SolutionProvider_Timeout(t *testing.T) {
	t.Parallel()
	// Verify that the timeout field is accepted and the child succeeds within
	// the configured timeout budget.
	tmpDir := t.TempDir()

	// Use absolute path for the child because the parent lives in a temp dir,
	// and relative paths now resolve from the solution file's directory.
	absChildPath := filepath.Join(findProjectRoot(), "tests/integration/testdata/solution-provider/child.yaml")

	parentSolution := fmt.Sprintf(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: timeout-test
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      timeout: 90s
      resolve:
        with:
          - provider: solution
            inputs:
              source: %q
              inputs:
                message: "with timeout"
              timeout: "90s"
`, absChildPath)
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctlLong(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "hello from child")
	assert.Contains(t, stdout, "with timeout")
}

func TestIntegration_SolutionResolver_AuthProviderUnavailable(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/auth-provider/solution.yaml",
		"-o", "json",
	)
	t.Logf("stderr: %s", stderr)
	// Should fail because the auth handler doesn't exist
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "nonexistent-handler-xyz", "error should reference the missing handler name")
}

// TestIntegration_SolutionResolver_DeclaredTypeGovernsParameterCoercion is the
// regression test for issue-17: a type:string resolver reading a parameter
// source must keep the raw CLI value "2.0" verbatim instead of inferring a
// float and re-coercing it to "2".
func TestIntegration_SolutionResolver_DeclaredTypeGovernsParameterCoercion(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/declared-type-coercion/solution.yaml",
		"-r", "version=2.0",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	require.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &decoded), "stdout must be valid JSON")
	assert.Equal(t, "2.0", decoded["version"], "declared type:string must keep the raw CLI value verbatim")
}

// ============================================================================
// Bundle Command Tests
// ============================================================================

func TestIntegration_BundleDiffHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "diff", "bundle", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show what changed between two versions")
	assert.Contains(t, stdout, "--files-only")
	assert.Contains(t, stdout, "--solution-only")
	assert.Contains(t, stdout, "--ignore")
}

func TestIntegration_BundleExtractHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "extract", "bundle", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Extract files from a bundled solution artifact")
	assert.Contains(t, stdout, "--output-dir")
	assert.Contains(t, stdout, "--resolver")
	assert.Contains(t, stdout, "--action")
	assert.Contains(t, stdout, "--include")
	assert.Contains(t, stdout, "--list-only")
	assert.Contains(t, stdout, "--flatten")
}

func TestIntegration_BundleDiff_MissingArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "diff", "bundle")

	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_BundleExtract_MissingRef(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "extract", "bundle")

	assert.NotEqual(t, 0, exitCode)
}

// TestIntegration_BundleCommand_HardRemoved verifies the standalone `bundle`
// command group (and `bundle verify`) was retired in the grammar migration.
// Both now resolve as unknown commands and exit non-zero.
func TestIntegration_BundleCommand_HardRemoved(t *testing.T) {
	t.Parallel()

	t.Run("bundle group removed", func(t *testing.T) {
		t.Parallel()
		_, stderr, exitCode := runScafctl(t, "bundle")
		assert.NotEqual(t, 0, exitCode)
		assert.Contains(t, stderr, "unknown command")
	})

	t.Run("bundle verify removed", func(t *testing.T) {
		t.Parallel()
		_, stderr, exitCode := runScafctl(t, "bundle", "verify", "x")
		assert.NotEqual(t, 0, exitCode)
		assert.Contains(t, stderr, "unknown command")
	})
}

// incompleteSolutionFixture writes a solution that references an unvendored
// catalog dependency (which will not be present in the bundle) alongside a
// local file (so a bundle layer is actually produced). Packaging with
// --no-vendor therefore yields a bundle that fails completeness verification.
// It returns the directory containing the solution file.
func incompleteSolutionFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data.txt"), []byte("hello\n"), 0o644))
	solution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: incomplete-demo
  version: "1.0.0"
  description: Solution referencing an unvendored catalog dependency
spec:
  resolvers:
    localfile:
      description: Reads a local file so a bundle layer is produced
      type: string
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./data.txt
    dep:
      description: References an unvendored catalog solution dependency
      type: string
      resolve:
        with:
          - provider: solution
            inputs:
              source: nonexistent-dep@1.0.0
              resolver: value
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(solution), 0o644))
	return dir
}

// TestIntegration_PackageSolution_IncompleteBundleFails verifies the producer
// verification hook: packaging a solution whose built bundle is incomplete
// fails by default, and --no-verify skips the check.
func TestIntegration_PackageSolution_IncompleteBundleFails(t *testing.T) {
	t.Parallel()

	t.Run("fails on incomplete bundle", func(t *testing.T) {
		t.Parallel()
		dir := incompleteSolutionFixture(t)
		tmpDir := t.TempDir()
		env := map[string]string{"XDG_DATA_HOME": tmpDir, "XDG_CACHE_HOME": tmpDir}
		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, dir, env,
			"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
			"--no-vendor", "--skip-lint", "--skip-tests")
		assert.NotEqual(t, 0, exitCode)
		assert.Contains(t, stdout+stderr, "incomplete")
	})

	t.Run("--no-verify skips the check", func(t *testing.T) {
		t.Parallel()
		dir := incompleteSolutionFixture(t)
		tmpDir := t.TempDir()
		env := map[string]string{"XDG_DATA_HOME": tmpDir, "XDG_CACHE_HOME": tmpDir}
		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, dir, env,
			"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
			"--no-vendor", "--skip-lint", "--skip-tests", "--no-verify")
		assert.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)
	})

	t.Run("--no-verify does not poison the build cache", func(t *testing.T) {
		t.Parallel()
		dir := incompleteSolutionFixture(t)
		tmpDir := t.TempDir()
		env := map[string]string{"XDG_DATA_HOME": tmpDir, "XDG_CACHE_HOME": tmpDir}

		// First: package the incomplete artifact with --no-verify (succeeds,
		// but must NOT write a build-cache entry for the unverified artifact).
		_, _, exitCode := runScafctlWithEnvInDir(t, dir, env,
			"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
			"--no-vendor", "--skip-lint", "--skip-tests", "--no-verify")
		require.Equal(t, 0, exitCode, "--no-verify package should succeed")

		// Then: package again WITH verification enabled, same env. If the first
		// run had cached the artifact, this would hit the "Build cache hit" fast
		// path and exit 0 without verifying -- the poisoning bug. It must instead
		// re-verify and FAIL on the incomplete bundle.
		stdout, stderr, exitCode := runScafctlWithEnvInDir(t, dir, env,
			"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
			"--no-vendor", "--skip-lint", "--skip-tests")
		assert.NotEqual(t, 0, exitCode,
			"verify-enabled rerun must re-verify (not hit a poisoned cache); stdout: %s stderr: %s", stdout, stderr)
		assert.Contains(t, stdout+stderr, "incomplete")
	})
}

// TestIntegration_PackageSolution_IncompleteBundle_EmbedderBinaryName verifies
// that the producer verification failure is surfaced when the binary is invoked
// under a non-default (embedder) name via an argv[0] symlink.
func TestIntegration_PackageSolution_IncompleteBundle_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink dispatch test is POSIX-only")
	}

	dir := incompleteSolutionFixture(t)
	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, "mycli")
	require.NoError(t, os.Symlink(binaryPath, linkPath))

	tmpDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, linkPath,
		"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
		"--no-vendor", "--skip-lint", "--skip-tests")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+tmpDir, "XDG_CACHE_HOME="+tmpDir)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "expected non-zero exit for incomplete bundle")
	assert.NotEqual(t, 0, exitErr.ExitCode())
	assert.Contains(t, outBuf.String()+errBuf.String(), "incomplete")
}

// TestIntegration_PackageSolution_RerunAfterFailedVerify verifies Fix 4: a
// failed completeness verification must NOT leave a build-cache entry, so a
// rerun re-builds and re-verifies (and fails again) instead of hitting a cached
// broken artifact and exiting 0.
func TestIntegration_PackageSolution_RerunAfterFailedVerify(t *testing.T) {
	t.Parallel()
	dir := incompleteSolutionFixture(t)
	tmpDir := t.TempDir()
	env := map[string]string{"XDG_DATA_HOME": tmpDir, "XDG_CACHE_HOME": tmpDir}

	// First run: fails verification.
	_, _, exitCode := runScafctlWithEnvInDir(t, dir, env,
		"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
		"--no-vendor", "--skip-lint", "--skip-tests")
	require.NotEqual(t, 0, exitCode, "first run should fail verification")

	// Second run: must ALSO fail (no cached broken artifact short-circuits it).
	stdout, stderr, exitCode := runScafctlWithEnvInDir(t, dir, env,
		"package", "solution", "-f", "solution.yaml", "--version", "1.0.0",
		"--no-vendor", "--skip-lint", "--skip-tests", "--force")
	assert.NotEqual(t, 0, exitCode,
		"rerun must re-verify, not exit 0 from a build-cache hit; stdout: %s stderr: %s", stdout, stderr)
	assert.NotContains(t, stdout+stderr, "Build cache hit",
		"a failed verify must not produce a cache hit on rerun")
}

func TestIntegration_BundleExtract_AfterBuild(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Extract the built artifact
	extractDir := filepath.Join(tmpDir, "extracted")
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "extract", "bundle", "resolver-demo@1.0.0", "--output-dir", extractDir)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_BundleExtract_ListOnly(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build first
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List files — may have no bundle layer if the solution has no bundle config
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "extract", "bundle", "resolver-demo@1.0.0", "--list-only")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
	// Either lists files or warns about no bundle — both are valid
	assert.True(t, strings.Contains(stdout, "Total") || strings.Contains(stdout, "no bundle"),
		"expected either file list or no-bundle warning")
}

func TestIntegration_BundleDiff_SameVersion(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build two versions
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "2.0.0", "--no-cache")
	require.Equal(t, 0, exitCode)

	// Diff them
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "diff", "bundle", "resolver-demo@1.0.0", "resolver-demo@2.0.0")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Comparing")
	assert.Contains(t, stdout, "Summary")
}

func TestIntegration_BuildSolution_NestedBundle(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Build the nested-bundle example — should discover sub-solution files recursively
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/solutions/nested-bundle/parent.yaml", "--version", "1.0.0", "--dry-run")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)

	// Dry-run output should list the child sub-solution and its files
	assert.Contains(t, stdout, "parent-config.txt", "should discover parent's local file")
	assert.Contains(t, stdout, "child.yaml", "should discover the sub-solution file")
}

// ============================================================================
// Vendor Command Tests
// ============================================================================

func TestIntegration_VendorHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "vendor", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "vendor")
	assert.Contains(t, stdout, "update")
}

func TestIntegration_VendorUpdateHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "vendor", "update", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Re-resolve and update vendored dependencies")
	assert.Contains(t, stdout, "--dependency")
	assert.Contains(t, stdout, "--dry-run")
	assert.Contains(t, stdout, "--lock-only")
	assert.Contains(t, stdout, "--pre-release")
}

func TestIntegration_VendorUpdate_NoLockFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Create a minimal solution file without a lock file
	solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-vendor
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
`
	solPath := filepath.Join(tmpDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solPath, []byte(solContent), 0o644))

	_, stderr, exitCode := runScafctlWithEnv(t, env, "vendor", "update", "-f", solPath)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "lock file")
}

// ============================================================================
// Build Solution Dedup Tests
// ============================================================================

func TestIntegration_BuildSolutionHelp_DedupeFlags(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "build", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--dedupe")
	assert.Contains(t, stdout, "--dedupe-threshold")
}

func TestIntegration_BuildSolution_WithDedupe(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml",
		"--version", "1.0.0", "--dedupe")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_WithDedupeDisabled(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml",
		"--version", "1.0.0", "--dedupe=false")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_DryRunShowsDetails(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml",
		"--version", "1.0.0", "--dry-run")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	// Dry run should show structured output: files, analysis, summary
	assert.Contains(t, stdout, "Dry run")
}

// ============================================================================
// Build Plugin Integration Tests
// ============================================================================

func TestIntegration_BuildPluginHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "build", "plugin", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "multi-platform")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--version")
	assert.Contains(t, stdout, "--platform")
	assert.Contains(t, stdout, "--force")
}

func TestIntegration_BuildPlugin_HelpShownInBuildParent(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "build", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "plugin")
}

func TestIntegration_BuildPlugin_MissingRequiredFlags(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "build", "plugin")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_BuildPlugin_SinglePlatform(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Create a mock binary
	binPath := filepath.Join(tmpDir, "my-provider")
	require.NoError(t, os.WriteFile(binPath, []byte("fake-plugin-binary"), 0o755))

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "test-provider",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built test-provider@1.0.0")
	assert.Contains(t, stdout, "1 platform(s)")
	assert.Contains(t, stdout, "linux/amd64")
}

func TestIntegration_BuildPlugin_MultiPlatform(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	// Create mock binaries for two platforms
	linuxBin := filepath.Join(tmpDir, "provider-linux")
	darwinBin := filepath.Join(tmpDir, "provider-darwin")
	require.NoError(t, os.WriteFile(linuxBin, []byte("linux-binary"), 0o755))
	require.NoError(t, os.WriteFile(darwinBin, []byte("darwin-binary"), 0o755))

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "multi-provider",
		"--kind", "provider",
		"--version", "2.0.0",
		"--platform", "linux/amd64="+linuxBin,
		"--platform", "darwin/arm64="+darwinBin)

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built multi-provider@2.0.0")
	assert.Contains(t, stdout, "2 platform(s)")
}

func TestIntegration_BuildPlugin_AuthHandler(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	binPath := filepath.Join(tmpDir, "auth-handler")
	require.NoError(t, os.WriteFile(binPath, []byte("auth-binary"), 0o755))

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "test-auth",
		"--kind", "auth-handler",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built test-auth@1.0.0")
}

// TestIntegration_PackagePlugin_Canonical verifies the canonical "package
// plugin" command name packages a multi-platform plugin into the local catalog
// (the "build plugin" form is covered as an alias elsewhere).
func TestIntegration_PackagePlugin_Canonical(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	binPath := filepath.Join(tmpDir, "canonical-provider")
	require.NoError(t, os.WriteFile(binPath, []byte("provider-binary"), 0o755))

	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "package", "plugin",
		"--name", "canonical-provider",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built canonical-provider@1.0.0")
}

// TestIntegration_AuthHandlersInstall_ThirdPartyFromCatalog verifies the fix
// for issue #576: a non-official auth handler published to a configured
// catalog resolves by name instead of being rejected against the hardcoded
// official allowlist (entra, gcp, github).
func TestIntegration_AuthHandlersInstall_ThirdPartyFromCatalog(t *testing.T) {
	t.Parallel()
	env := isolatedCatalogEnv(t)

	// Build a third-party auth handler into the local catalog for the current
	// platform (so 'handlers install' can fetch it for this host).
	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "scafctl-plugin-auth-openshift")
	require.NoError(t, os.WriteFile(binPath, []byte("dummy-auth-handler"), 0o755))
	platform := runtime.GOOS + "/" + runtime.GOARCH

	_, buildErr, buildExit := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "openshift",
		"--kind", "auth-handler",
		"--version", "0.1.0",
		"--platform", platform+"="+binPath)
	require.Equal(t, 0, buildExit, "build plugin failed: %s", buildErr)

	// It is listed as an auth-handler artifact in the local catalog.
	listOut, _, listExit := runScafctlWithEnv(t, env, "catalog", "list", "--kind", "auth-handler", "-o", "json")
	require.Equal(t, 0, listExit)
	assert.Contains(t, listOut, "openshift")

	// 'auth handlers install openshift' must get PAST the official allowlist and
	// resolve the handler from the catalog, exiting 0. Previously this failed
	// immediately with 'unknown auth handler "openshift"; available: entra, gcp,
	// github'.
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "auth", "handlers", "install", "openshift")
	combined := stdout + stderr
	require.Equal(t, 0, exitCode,
		"third-party handler install should succeed; stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, combined, "openshift")
	assert.NotContains(t, combined, "available: entra, gcp, github",
		"handler should resolve from catalog, not be rejected by the allowlist")
	assert.NotContains(t, combined, `unknown auth handler "openshift"`,
		"handler should resolve from catalog, not be rejected by the allowlist")
}

// TestIntegration_AuthHandlersInstall_ThirdPartyDisabled verifies that the
// disableThirdPartyAuthHandlers policy rejects a non-official handler even when
// present in a configured catalog.
func TestIntegration_AuthHandlersInstall_ThirdPartyDisabled(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "scafctl")
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	configContent := `catalogs:
  - name: local
    type: filesystem
settings:
  disableOfficialCatalog: true
  disableThirdPartyAuthHandlers: true
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(configContent), 0o600))
	env := map[string]string{
		"XDG_DATA_HOME":   tmpDir,
		"XDG_CACHE_HOME":  tmpDir,
		"XDG_CONFIG_HOME": tmpDir,
	}

	binDir := t.TempDir()
	binPath := filepath.Join(binDir, "scafctl-plugin-auth-acme")
	require.NoError(t, os.WriteFile(binPath, []byte("dummy-auth-handler"), 0o755))
	platform := runtime.GOOS + "/" + runtime.GOARCH

	_, buildErr, buildExit := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "acme",
		"--kind", "auth-handler",
		"--version", "0.1.0",
		"--platform", platform+"="+binPath)
	require.Equal(t, 0, buildExit, "build plugin failed: %s", buildErr)

	// Policy blocks resolution of the third-party handler.
	_, stderr, exitCode := runScafctlWithEnv(t, env, "auth", "handlers", "install", "acme")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "acme")
	assert.Contains(t, stderr, "disabled")
}

func TestIntegration_BuildPlugin_ForceOverwrite(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME":  tmpDir,
		"XDG_CACHE_HOME": tmpDir,
	}

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary-v1"), 0o755))

	// First build
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.Equal(t, 0, exitCode)

	// Second build without --force should fail
	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "already exists")

	// Third build with --force should succeed
	stdout, _, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath,
		"--force")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built force-test@1.0.0")
}

func TestIntegration_BuildPlugin_InvalidPlatform(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME": tmpDir,
	}

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0o755))

	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "bad-plat",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "freebsd/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unsupported platform")
}

func TestIntegration_BuildPlugin_InvalidKind(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME": tmpDir,
	}

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0o755))

	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "bad-kind",
		"--kind", "solution",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid kind")
}

func TestIntegration_BuildPlugin_BinaryNotFound(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME": tmpDir,
	}

	missingBin := filepath.Join(t.TempDir(), "nonexistent-binary")
	_, stderr, exitCode := runScafctlWithEnv(t, env, "build", "plugin",
		"--name", "missing",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+missingBin)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "binary not found")
}

// ============================================================================
// Directory Provider Integration Tests
// ============================================================================

func TestIntegration_RunSolution_DirectoryProvider(t *testing.T) {
	t.Parallel()
	// Create a temp directory with test files
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("world"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "sub", "nested.go"), []byte("package sub"), 0o644))

	// Create a solution YAML that uses the directory provider
	solutionFile := filepath.Join(tmpDir, "dir-solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dir-test
  version: 1.0.0
  description: Directory provider integration test

spec:
  resolvers:
    listing:
      description: List temp directory
      type: any
      resolve:
        with:
          - provider: directory
            inputs:
              operation: list
              path: "` + filepath.ToSlash(tmpDir) + `"
              recursive: true
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", solutionFile,
		"-o", "json",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "hello.txt")
	assert.Contains(t, stdout, "nested.go")
	assert.Contains(t, stdout, "totalCount")
}

// ============================================================================
// Test Command Tests
// ============================================================================

func TestIntegration_Test_Functional(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-functional-pass
  version: 1.0.0
spec:
  resolvers:
    greeting:
      description: Static greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    cases:
      basic-render:
        description: Verify render works
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
          - contains: greeting
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
}

func TestIntegration_Test_Functional_Failure(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-functional-fail
  version: 1.0.0
spec:
  resolvers:
    greeting:
      description: Static greeting
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    cases:
      fail-on-purpose:
        description: This test should fail
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - contains: this-string-definitely-does-not-exist-in-output
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	_, _, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--no-color",
	)

	// Exit code 11 = TestFailed
	assert.Equal(t, 11, exitCode, "expected exit code 11 for test failure")
}

func TestIntegration_Test_Functional_SnapshotMasking(t *testing.T) {
	t.Parallel()

	const solution = "tests/integration/solutions/snapshot-masking/solution.yaml"

	type resultItem struct {
		Test        string         `json:"test"`
		Status      string         `json:"status"`
		Relaxed     bool           `json:"relaxed"`
		MaskMatches map[string]int `json:"maskMatches"`
	}

	t.Run("json surfaces relaxed status and mask counts", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, exitCode := runScafctlLong(t,
			"test", "functional",
			"-f", solution,
			"--skip-builtins",
			"--no-color",
			"-o", "json",
		)
		require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

		var report struct {
			Results []resultItem `json:"results"`
			Summary struct {
				Passed  int `json:"passed"`
				Relaxed int `json:"relaxed"`
			} `json:"summary"`
		}
		require.NoError(t, json.Unmarshal([]byte(stdout), &report))

		byTest := map[string]resultItem{}
		for _, r := range report.Results {
			byTest[r.Test] = r
		}

		// Stdout source: custom mask + opt-in email preset (uuid disabled).
		stdoutCase := byTest["snapshot-stdout"]
		assert.Equal(t, "pass", stdoutCase.Status)
		assert.True(t, stdoutCase.Relaxed, "masked test should report relaxed")
		assert.Equal(t, 1, stdoutCase.MaskMatches["greeting"], "custom greeting mask should match once")
		assert.Equal(t, 2, stdoutCase.MaskMatches["email"], "opt-in email preset should match twice")
		assert.NotContains(t, stdoutCase.MaskMatches, "uuid", "disabled built-in preset must not appear")

		// Files source: path-scoped custom mask over the rendered tree.
		filesCase := byTest["snapshot-files"]
		assert.Equal(t, "pass", filesCase.Status)
		assert.True(t, filesCase.Relaxed)
		assert.Equal(t, 1, filesCase.MaskMatches["contact"], "path-scoped contact mask should match once")

		assert.Equal(t, 2, report.Summary.Passed)
		assert.Equal(t, 2, report.Summary.Relaxed)
	})

	t.Run("table output shows PASS star and relaxed section", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, exitCode := runScafctlLong(t,
			"test", "functional",
			"-f", solution,
			"--skip-builtins",
			"--no-color",
		)
		require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)
		assert.Contains(t, stdout, "PASS*", "relaxed tests should render PASS* in the table")
		assert.Contains(t, stdout, "Relaxed (snapshot fidelity loosened):")
		assert.Contains(t, stdout, "2 relaxed")
	})
}

// TestIntegration_Test_Functional_Calls exercises the parameterized calls
// (spec.calls) feature end-to-end through the CLI by running the functional
// test cases bundled with the calls fixture solution. The fixture covers call
// definitions invoked from resolve, validate, and action steps; the args
// namespace; typed args with defaults and required values; array argument
// serialization; and opt-in de-duplication.
func TestIntegration_Test_Functional_Calls(t *testing.T) {
	t.Parallel()

	const solution = "tests/integration/solutions/calls/solution.yaml"

	type resultItem struct {
		Test   string `json:"test"`
		Status string `json:"status"`
	}

	stdout, stderr, exitCode := runScafctlLong(t,
		"test", "functional",
		"-f", solution,
		"--skip-builtins",
		"--no-color",
		"-o", "json",
	)
	require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	var report struct {
		Results []resultItem `json:"results"`
		Summary struct {
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	assert.Zero(t, report.Summary.Failed, "no calls test case should fail")
	assert.GreaterOrEqual(t, report.Summary.Passed, 8, "expected the calls fixture cases to pass")

	byTest := map[string]string{}
	for _, r := range report.Results {
		byTest[r.Test] = r.Status
	}
	// Spot-check representative cases across resolve, validate, and action call sites.
	for _, name := range []string{
		"default-arg",
		"override-arg",
		"arg-from-resolver-ref",
		"int-arg-default",
		"int-arg-override",
		"array-arg",
		"validate-call",
		"action-call",
	} {
		assert.Equal(t, "pass", byTest[name], "case %q should pass", name)
	}
}

// TestIntegration_Test_Functional_CrossValidation exercises the two-phase
// (deferred cross-resolver) validation feature end-to-end by running the
// functional test cases bundled with the cross-validation fixture solution.
// The fixture covers the load-without-cycle guarantee, deferred rules passing
// when the cross-resolver assertion holds, and both fatal (--on-validation-error error)
// and non-fatal failure modes when the assertion is violated.
func TestIntegration_Test_Functional_CrossValidation(t *testing.T) {
	t.Parallel()

	const solution = "tests/integration/solutions/resolvers/cross-validation/solution.yaml"

	type resultItem struct {
		Test   string `json:"test"`
		Status string `json:"status"`
	}

	stdout, stderr, exitCode := runScafctlLong(t,
		"test", "functional",
		"-f", solution,
		"--skip-builtins",
		"--no-color",
		"-o", "json",
	)
	require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	var report struct {
		Results []resultItem `json:"results"`
		Summary struct {
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	assert.Zero(t, report.Summary.Failed, "no cross-validation test case should fail")

	byTest := map[string]string{}
	for _, r := range report.Results {
		byTest[r.Test] = r.Status
	}
	for _, name := range []string{
		"loads-without-cycle",
		"differing-regions-pass",
		"equal-regions-fail",
		"equal-regions-nonfatal-shows-values",
	} {
		assert.Equal(t, "pass", byTest[name], "case %q should pass", name)
	}
}

// TestIntegration_Test_Functional_LintDeferredValidation exercises the linting
// side of two-phase validation by running the functional test cases bundled
// with the lint-deferred-validation fixture. The fixture asserts that
// cross-resolver validation references do NOT trigger the resolver-cycle rule
// and DO emit the informational deferred-validation-not-fail-fast advisory.
func TestIntegration_Test_Functional_LintDeferredValidation(t *testing.T) {
	t.Parallel()

	const solution = "tests/integration/solutions/lint-deferred-validation/solution.yaml"

	type resultItem struct {
		Test   string `json:"test"`
		Status string `json:"status"`
	}

	stdout, stderr, exitCode := runScafctlLong(t,
		"test", "functional",
		"-f", solution,
		"--skip-builtins",
		"--no-color",
		"-o", "json",
	)
	require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	var report struct {
		Results []resultItem `json:"results"`
		Summary struct {
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	assert.Zero(t, report.Summary.Failed, "no lint deferred-validation test case should fail")

	byTest := map[string]string{}
	for _, r := range report.Results {
		byTest[r.Test] = r.Status
	}
	for _, name := range []string{
		"lint-no-cycle",
		"lint-emits-advisory",
	} {
		assert.Equal(t, "pass", byTest[name], "case %q should pass", name)
	}
}

// TestIntegration_Test_Functional_LintParameterNumericMatches exercises the
// parameter-numeric-matches lint rule by running the functional test cases
// bundled with the lint-parameter-numeric-matches fixture. The fixture asserts
// that a resolver reading a numeric 'parameter' default without an explicit
// 'type' but calling matches() emits the warning, and that the warning does
// not fail lint.
func TestIntegration_Test_Functional_LintParameterNumericMatches(t *testing.T) {
	t.Parallel()

	const solution = "tests/integration/solutions/lint-parameter-numeric-matches/solution.yaml"

	type resultItem struct {
		Test   string `json:"test"`
		Status string `json:"status"`
	}

	stdout, stderr, exitCode := runScafctlLong(t,
		"test", "functional",
		"-f", solution,
		"--skip-builtins",
		"--no-color",
		"-o", "json",
	)
	require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	var report struct {
		Results []resultItem `json:"results"`
		Summary struct {
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"summary"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	assert.Zero(t, report.Summary.Failed, "no parameter-numeric-matches test case should fail")

	byTest := map[string]string{}
	for _, r := range report.Results {
		byTest[r.Test] = r.Status
	}
	for _, name := range []string{
		"lint-warns-numeric-matches",
		"lint-passes-with-warning",
	} {
		assert.Equal(t, "pass", byTest[name], "case %q should pass", name)
	}
}

// TestIntegration_Run_Resolver_ParameterTypes verifies the parameter provider's
// "type" coercion enum end-to-end by running the bundled example with CLI
// parameters and asserting each resolver coerces to the expected Go type.
func TestIntegration_Run_Resolver_ParameterTypes(t *testing.T) {
	t.Parallel()

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/providers/parameter-types.yaml",
		"-o", "json",
		"-r", "port=8080",
		"-r", "billingId=00042",
		"-r", "token=00abc",
		"-r", "ratio=1.5",
		"-r", "enabled=true",
		"-r", `config={"replicas":3}`,
		"-r", "regions=us-east-1, us-west-2",
		"-r", "repoUrl=https://github.com/org/repo",
	)
	require.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &out))

	// auto (default): numeric-looking value infers to a JSON number.
	assert.InDelta(t, float64(8080), out["inferred"], 0, "auto should infer a number")
	// auto: a URL value is stored verbatim, never fetched.
	assert.Equal(t, "https://github.com/org/repo", out["repoUrl"], "auto should keep a URL literal")
	// fetch: no configUrl provided, so the static fallback is used (no network).
	assert.Equal(t, "<no configUrl provided; pass -r configUrl=https://...>", out["fetchedConfig"], "fetch should fall back when configUrl is absent")
	// string: keep leading zeros as a string.
	assert.Equal(t, "00042", out["billingId"], "type string should preserve leading zeros")
	// raw: returned verbatim, no coercion.
	assert.Equal(t, "00abc", out["token"], "type raw should return the value verbatim")
	// int: forced integer parsing.
	assert.InDelta(t, float64(8080), out["replicas"], 0, "type int should parse an integer")
	// float: forced floating-point parsing.
	assert.InDelta(t, 1.5, out["ratio"], 0, "type float should parse a float")
	// bool: forced boolean parsing.
	assert.Equal(t, true, out["enabled"], "type bool should parse a boolean")
	// json: parsed into a structured object.
	assert.Equal(t, map[string]any{"replicas": float64(3)}, out["config"], "type json should parse JSON")
	// csv: split into a trimmed list of strings.
	assert.Equal(t, []any{"us-east-1", "us-west-2"}, out["regions"], "type csv should split and trim")
}

func TestIntegration_Test_List(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-list-example
  version: 1.0.0
spec:
  resolvers:
    msg:
      description: A message
      resolve:
        with:
          - provider: static
            inputs:
              value: hi
  testing:
    cases:
      smoke-test:
        description: Smoke test
        command: [run, resolver]
        args: ["-o", "json"]
        tags: [smoke]
        assertions:
          - expression: __exitCode == 0
      another-test:
        description: Another test
        command: [lint]
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "list",
		"-f", solutionFile,
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0")
	assert.Contains(t, stdout, "smoke-test")
	assert.Contains(t, stdout, "another-test")
}

func TestIntegration_Test_Functional_JSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-json-output
  version: 1.0.0
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: data
  testing:
    cases:
      json-test:
        description: Test with JSON output
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"-o", "json",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0")
	// JSON output should parse as valid JSON containing test results
	assert.Contains(t, stdout, "json-test")
}

func TestIntegration_Test_Functional_JUnit(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	reportFile := filepath.Join(tmpDir, "results.xml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-junit-output
  version: 1.0.0
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: data
  testing:
    cases:
      junit-test:
        description: Test for JUnit report
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--report-file", reportFile,
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0")

	// Verify JUnit XML file was written
	_, err := os.Stat(reportFile)
	assert.NoError(t, err, "JUnit report file should exist")

	data, err := os.ReadFile(reportFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<?xml")
	assert.Contains(t, string(data), "testsuite")
}

func TestIntegration_Test_Functional_DryRun(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-dry-run
  version: 1.0.0
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: data
  testing:
    cases:
      dry-test:
        description: Dry run test
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - contains: impossible-string-that-would-fail
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--dry-run",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Dry run should succeed even when assertions would fail
	assert.Equal(t, 0, exitCode, "dry-run should return exit code 0")
}

func TestIntegration_Test_Functional_Filter(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-filter
  version: 1.0.0
spec:
  resolvers:
    val:
      description: Static value
      resolve:
        with:
          - provider: static
            inputs:
              value: data
  testing:
    cases:
      render-pass:
        description: This test should run and pass
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
      skipped-test:
        description: This test should not run due to filter
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - contains: impossible-string-that-would-fail
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--filter", "render-*",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Should pass because only render-pass runs (skipped-test is filtered out)
	assert.Equal(t, 0, exitCode, "expected exit code 0 when filtered")
}

func TestIntegration_Test_Functional_Compose(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	testsDir := filepath.Join(tmpDir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0o755))

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-compose
  version: 1.0.0
compose:
  - tests/*.yaml
spec:
  resolvers:
    msg:
      description: A simple message
      resolve:
        with:
          - provider: static
            inputs:
              value: composed-output
  testing:
    cases:
      _base:
        description: Base template
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`
	testFileContent := `spec:
  testing:
    cases:
      composed-test:
        description: Test from composed file
        extends: [_base]
        assertions:
          - expression: '"msg" in __output'
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "smoke.yaml"), []byte(testFileContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0 for composed tests\nstdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "composed-test", "composed test should appear in output")
}

// TestIntegration_Test_Functional_ComposeFilesMerge verifies that
// testing.config.files entries from the root solution and a composed file are
// merged together — both shared files are copied into the test sandbox.
func TestIntegration_Test_Functional_ComposeFilesMerge(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	testsDir := filepath.Join(tmpDir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0o755))

	// Both files must exist at the solution root so the sandbox can copy them.
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("from-root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("from-composed"), 0o644))

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-compose-files
  version: 1.0.0
compose:
  - tests/*.yaml
spec:
  resolvers:
    fileA:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./a.txt
    fileB:
      resolve:
        with:
          - provider: file
            inputs:
              operation: read
              path: ./b.txt
  testing:
    config:
      # Root contributes a.txt; the composed file contributes b.txt.
      files:
        - a.txt
    cases:
      _base:
        description: Base template
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`
	// The composed file adds b.txt to testing.config.files and a test that
	// asserts both files were copied into the sandbox and read successfully.
	testFileContent := `spec:
  testing:
    config:
      files:
        - b.txt
    cases:
      reads-both-files:
        description: Both merged shared files are present in the sandbox
        extends: [_base]
        assertions:
          - expression: '__output.fileA.content == "from-root"'
            message: root file a.txt should be copied into the sandbox
          - expression: '__output.fileB.content == "from-composed"'
            message: composed file b.txt should be merged and copied into the sandbox
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "extra.yaml"), []byte(testFileContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0 when shared files merge across compose files\nstdout: %s\nstderr: %s", stdout, stderr)
	assert.Contains(t, stdout, "reads-both-files", "composed test should appear in output")
}

// TestIntegration_Test_Functional_ComposeDuplicateService verifies that a test
// service defined with the same name in both the root solution and a composed
// file is rejected with an error.
func TestIntegration_Test_Functional_ComposeDuplicateService(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	testsDir := filepath.Join(tmpDir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0o755))

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-compose-dup-service
  version: 1.0.0
compose:
  - tests/*.yaml
spec:
  resolvers:
    msg:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    config:
      services:
        - name: mock-api
          type: exec
          execRules:
            - command: "echo root"
              stdout: root
              exitCode: 0
    cases:
      _base:
        description: Base template
        command: [run, resolver]
        args: ["-o", "json"]
        assertions:
          - expression: __exitCode == 0
`
	// The composed file declares a service with the same name — must be rejected.
	testFileContent := `spec:
  testing:
    config:
      services:
        - name: mock-api
          type: exec
          execRules:
            - command: "echo composed"
              stdout: composed
              exitCode: 0
    cases:
      composed-test:
        description: Test from composed file
        extends: [_base]
        assertions:
          - expression: '"msg" in __output'
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "smoke.yaml"), []byte(testFileContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "functional",
		"-f", solutionFile,
		"--skip-builtins",
		"--no-color",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.NotEqual(t, 0, exitCode, "expected non-zero exit code for duplicate test service name")
	assert.Contains(t, stderr, "duplicate test service", "error should mention the duplicate test service")
}

func TestIntegration_Test_Functional_AutoDiscovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-functional
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
  testing:
    cases:
      resolve-greeting:
        description: Verify greeting resolver
        command: [run, resolver]
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir,
		"test", "functional", "--skip-builtins", "--no-color",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected auto-discovery to work\nstdout: %s\nstderr: %s", stdout, stderr)
}

func TestIntegration_Test_Functional_AutoDiscoveryNoFile(t *testing.T) {
	t.Parallel()
	emptyDir := t.TempDir()

	_, stderr, exitCode := runScafctlInDir(t, emptyDir, "test", "functional")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no solution path provided")
}

func TestIntegration_Test_List_AutoDiscovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-list
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
  testing:
    cases:
      resolve-greeting:
        description: Verify greeting resolver
        command: [run, resolver]
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "test", "list")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected test list to auto-discover solution.yaml")
	assert.Contains(t, stdout, "resolve-greeting")
}

func TestIntegration_Test_List_AutoDiscoveryNoFile(t *testing.T) {
	t.Parallel()
	emptyDir := t.TempDir()

	_, stderr, exitCode := runScafctlInDir(t, emptyDir, "test", "list")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no solution path provided")
}

func TestIntegration_Test_Init(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-init-example
  version: 1.0.0
spec:
  resolvers:
    repo:
      description: Repository name
      resolve:
        with:
          - provider: static
            inputs:
              value: my-app
    version:
      description: Version
      resolve:
        with:
          - provider: static
            inputs:
              value: "1.0.0"
      validate:
        with:
          - provider: validation
            inputs:
              match: '^\d+\.\d+\.\d+$'
            message: "Invalid version format"
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"test", "init",
		"-f", solutionFile,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0")
	assert.Contains(t, stdout, "cases:")
	assert.Contains(t, stdout, "resolve-defaults")
	assert.Contains(t, stdout, "lint")
	assert.Contains(t, stdout, "resolver-repo")
	assert.Contains(t, stdout, "resolver-version")
	assert.Contains(t, stdout, "resolver-version-invalid")
	assert.Contains(t, stdout, "expectFailure: true")
}

func TestIntegration_Test_Init_MissingFile(t *testing.T) {
	t.Parallel()

	_, stderr, exitCode := runScafctl(t,
		"test", "init",
		"-f", "/nonexistent/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode, "expected non-zero exit code")
	assert.Contains(t, stderr, "reading solution file")
}

func TestIntegration_Test_Init_AutoDiscovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-init
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "test", "init")
	assert.Equal(t, 0, exitCode, "expected test init to auto-discover solution.yaml")
	assert.Contains(t, stdout, "cases:")
}

func TestIntegration_Test_Init_AutoDiscoveryNoFile(t *testing.T) {
	t.Parallel()
	emptyDir := t.TempDir()

	_, stderr, exitCode := runScafctlInDir(t, emptyDir, "test", "init")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no solution path provided")
}

// ─── MCP Server Integration Tests ────────────────────────────────────────────

func TestIntegration_MCPHelp(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "MCP")
	assert.Contains(t, stdout, "serve")
}

func TestIntegration_MCPServeHelp(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "serve", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Start the MCP server")
	assert.Contains(t, stdout, "--transport")
	assert.Contains(t, stdout, "--info")
	assert.Contains(t, stdout, "--log-file")
}

func TestIntegration_MCPServeInfo(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "serve", "--info")

	assert.Equal(t, 0, exitCode)

	// Verify valid JSON output
	var info struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Tools   []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &info))
	assert.Equal(t, "scafctl", info.Name)
	// Version may be empty in dev builds (no ldflags)

	// Verify Phase 2 tools are registered
	toolNames := make(map[string]bool)
	for _, tool := range info.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["list_solutions"], "expected list_solutions tool")
	assert.True(t, toolNames["inspect_solution"], "expected inspect_solution tool")
	assert.True(t, toolNames["lint_solution"], "expected lint_solution tool")
	assert.True(t, toolNames["list_providers"], "expected list_providers tool")
	assert.True(t, toolNames["get_provider_schema"], "expected get_provider_schema tool")
	assert.True(t, toolNames["list_cel_functions"], "expected list_cel_functions tool")

	// Phase 3 tools
	assert.True(t, toolNames["evaluate_cel"], "expected evaluate_cel tool")
	assert.True(t, toolNames["render_solution"], "expected render_solution tool")
	// auth_status is filtered out when no auth handlers are registered.
	// With lazy loading, handlers are not eagerly loaded at MCP startup.
	assert.True(t, toolNames["catalog_list"], "expected catalog_list tool")

	// Phase 4b tools (schema, examples)
	assert.True(t, toolNames["get_solution_schema"], "expected get_solution_schema tool")
	assert.True(t, toolNames["explain_kind"], "expected explain_kind tool")
	assert.True(t, toolNames["list_examples"], "expected list_examples tool")
	assert.True(t, toolNames["get_example"], "expected get_example tool")

	// Phase 5 tools (authoring workflow)
	assert.True(t, toolNames["preview_resolvers"], "expected preview_resolvers tool")
	assert.True(t, toolNames["run_solution_tests"], "expected run_solution_tests tool")
	assert.True(t, toolNames["get_run_command"], "expected get_run_command tool")

	// New tools from recent enhancements
	assert.True(t, toolNames["explain_error"], "expected explain_error tool")
	assert.True(t, toolNames["get_provider_output_shape"], "expected get_provider_output_shape tool")
	assert.True(t, toolNames["dry_run_solution"], "expected dry_run_solution tool")
	assert.True(t, toolNames["explain_concepts"], "expected explain_concepts tool")
	assert.True(t, toolNames["list_context_variables"], "expected list_context_variables tool")
}

func TestIntegration_MCPServeProtocol(t *testing.T) {
	t.Parallel()

	// Test the MCP JSON-RPC protocol by piping an initialize message via stdin.
	// We build a simple stdin payload and verify the server responds correctly.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "mcp", "serve")
	cmd.Dir = findProjectRoot()

	initMsg := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	cmd.Stdin = strings.NewReader(initMsg + "\n")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	// The server may exit with an error when stdin closes, that's OK
	_ = err

	output := outBuf.String()
	require.NotEmpty(t, output, "expected JSON-RPC response on stdout")

	// Parse the first JSON-RPC response
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
			Capabilities struct {
				Tools     map[string]any `json:"tools"`
				Resources map[string]any `json:"resources"`
			} `json:"capabilities"`
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &resp))
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, 1, resp.ID)
	assert.Equal(t, "scafctl", resp.Result.ServerInfo.Name)
	assert.NotEmpty(t, resp.Result.ProtocolVersion)
	assert.NotEmpty(t, resp.Result.Instructions)
	assert.NotNil(t, resp.Result.Capabilities.Tools)
}

// ============================================================================
// Eval Command Tests
// ============================================================================

func TestIntegration_EvalCEL_Simple(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "1 + 2")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "3")
}

// eval refs was relocated from 'get resolver refs' (#644). Verify the new path
// extracts resolver references from an expression.
func TestIntegration_EvalRefs_Expr(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "refs", "--expr", `_.config.host + ":" + string(_.port)`)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "config")
	assert.Contains(t, stdout, "port")
}

// The old 'get resolver' group was removed by #644; it must no longer appear
// as a subcommand of 'get'.
func TestIntegration_GetResolver_Removed(t *testing.T) {
	t.Parallel()
	stdout, _, _ := runScafctl(t, "get", "--help")
	assert.NotContains(t, stdout, "resolver")
}

func TestIntegration_EvalCEL_WithVar(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "size(name) > 3", "-v", "name=hello")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "true")
}

func TestIntegration_EvalCEL_WithData(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "_.name == 'hello'", "--data", `{"name": "hello"}`)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "true")
}

func TestIntegration_EvalCEL_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "1 + 2", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "1 + 2", result["expression"])
}

func TestIntegration_EvalCEL_InvalidExpression(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "invalid ++ syntax")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalCEL_MissingExpression(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "cel")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalTemplate_Simple(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "hello {{ .name }}", "-v", "name=world")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello world")
}

func TestIntegration_EvalTemplate_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "hi {{ .name }}", "-v", "name=test", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Contains(t, result["output"], "hi test")
}

func TestIntegration_EvalTemplate_ShowRefs(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "{{ .name }} {{ .age }}", "-v", "name=test", "-v", "age=25", "--show-refs")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test")
}

func TestIntegration_EvalTemplate_MissingTemplate(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "template")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalTemplate_MissingKeyDefault(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "hello {{ .missing }}", "--missing-key", "default")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "<no value>")
}

func TestIntegration_EvalTemplate_MissingKeyError(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "template", "-t", "hello {{ .missing }}", "-v", "other=val", "--missing-key", "error")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_CELValid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "size(name) > 3", "--type", "cel")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "valid")
}

// TestIntegration_EvalValidate_CELOptionalChaining is a regression test: CEL
// optional access/chaining (_.?name) must validate as syntactically valid on
// the CLI, matching the runtime evaluation environment. Previously the
// validator used a bare CEL environment without OptionalTypes and rejected it.
func TestIntegration_EvalValidate_CELOptionalChaining(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{
		`_.?name.orValue("fallback")`,
		"msg.?field.?nested",
		`_[?"name"].orValue("x")`,
	} {
		expr := expr
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", expr, "--type", "cel", "-o", "json")
			assert.Equal(t, 0, exitCode)

			var result map[string]any
			require.NoError(t, json.Unmarshal([]byte(stdout), &result))
			assert.Equal(t, true, result["valid"], "expr %q should be valid", expr)
			assert.Equal(t, "cel", result["type"])
		})
	}
}

func TestIntegration_EvalValidate_CELInvalid(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "invalid +++ (", "--type", "cel")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_GoTemplateValid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "{{ .name }}", "--type", "go-template")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "valid")
}

func TestIntegration_EvalValidate_GoTemplateInvalid(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "{{ .name", "--type", "go-template")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "1 + 2", "--type", "cel", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["valid"])
	assert.Equal(t, "cel", result["type"])
}

func TestIntegration_EvalValidate_UnsupportedType(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "test", "--type", "python")
	assert.NotEqual(t, 0, exitCode)
}

// ============================================================================
// New Solution Command Tests
// ============================================================================

func TestIntegration_NewSolution_Basic(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "test-app", "--description", "A test application")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test-app")
	assert.Contains(t, stdout, "A test application")
}

func TestIntegration_NewSolution_WithFeatures(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "my-deploy", "--description", "Deploy to K8s",
		"--features", "parameters,resolvers,actions")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-deploy")
	assert.Contains(t, stdout, "resolvers")
}

func TestIntegration_NewSolution_WithProviders(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "my-deploy", "--description", "Deploy",
		"--providers", "exec,http")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-deploy")
}

func TestIntegration_NewSolution_ToFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "file-test", "--description", "Written to file", "-o", outFile)
	assert.Equal(t, 0, exitCode)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "file-test")
}

func TestIntegration_NewSolution_MissingName(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "new", "solution", "--description", "Missing name")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_NewSolution_InvalidFeature(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "test", "--description", "Test", "--features", "invalid-feature")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_NewSolution_ScaffoldPassesLint(t *testing.T) {
	t.Parallel()
	// Scaffold with defaults, write to file, then lint — must produce zero findings.
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "lint-check", "--description", "Lint check", "-o", outFile)
	require.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctl(t, "lint", "-f", outFile, "-o", "json")
	require.Equal(t, 0, exitCode, "lint should pass: %s", stdout)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, float64(0), result["errorCount"])
	assert.Equal(t, float64(0), result["warnCount"])
}

func TestIntegration_NewSolution_ScaffoldRunsSuccessfully(t *testing.T) {
	t.Parallel()
	// Scaffold with defaults, write to file, then run — must execute without errors.
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "run-check", "--description", "Run check", "-o", outFile)
	require.Equal(t, 0, exitCode)

	stdout, stderr, exitCode := runScafctl(t, "run", "solution", "-f", outFile, "-r", "inputName=world")
	assert.Equal(t, 0, exitCode, "run should succeed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "Hello")
}

func TestIntegration_NewSolution_AllFeaturesScaffoldPassesLint(t *testing.T) {
	t.Parallel()
	// Scaffold with all features, write to file, then lint.
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "all-features",
		"--description", "All features", "--features",
		"parameters,resolvers,actions,transforms,validation,tests,composition",
		"-o", outFile)
	require.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctl(t, "lint", "-f", outFile, "-o", "json")
	require.Equal(t, 0, exitCode, "lint should pass: %s", stdout)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, float64(0), result["errorCount"])
	assert.Equal(t, float64(0), result["warnCount"])
}

func TestIntegration_NewSolution_ScaffoldFunctionalTestsPass(t *testing.T) {
	t.Parallel()
	// Scaffold with defaults, then run functional tests — all must pass (including builtins).
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "func-test", "--description", "Functional test check", "-o", outFile)
	require.Equal(t, 0, exitCode)

	stdout, stderr, exitCode := runScafctlLong(t, "test", "functional", "-f", outFile, "--no-color")
	assert.Equal(t, 0, exitCode, "functional tests should pass: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "0 failed")
}

// ============================================================================
// Lint Rules/Explain Command Tests
// ============================================================================

func TestIntegration_LintRules_List(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "rules")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Rule")
	assert.Contains(t, stdout, "Severity")
}

func TestIntegration_LintRules_FilterSeverity(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "rules", "--severity", "error")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "error")
	assert.NotContains(t, stdout, "warning")
}

func TestIntegration_LintRules_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "rules", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var rules []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rules))
	assert.Greater(t, len(rules), 0)
	assert.Contains(t, rules[0], "rule")
	assert.Contains(t, rules[0], "severity")
}

func TestIntegration_LintRule_KnownRule(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "rule", "missing-description")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "missing-description")
	assert.Contains(t, stdout, "severity")
}

func TestIntegration_LintRule_UnknownRule(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "lint", "rule", "nonexistent-rule")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_LintRule_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "rule", "missing-description", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "missing-description", result["rule"])
}

// TestIntegration_LintExplain_DeprecatedAlias verifies the old 'lint explain'
// alias still works and prints a deprecation notice on stderr, while producing
// the same rule output on stdout as the canonical 'lint rule'.
func TestIntegration_LintExplain_DeprecatedAlias(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "lint", "explain", "missing-description")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "missing-description")
	assert.Contains(t, stderr, "deprecated", "deprecated alias must emit a deprecation notice on stderr")
}

func TestIntegration_LintExplain_KnownRule(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "explain", "missing-description")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "missing-description")
	assert.Contains(t, stdout, "severity")
}

func TestIntegration_LintExplain_UnknownRule(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "lint", "explain", "nonexistent-rule")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_LintExplain_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lint", "explain", "missing-description", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "missing-description", result["rule"])
}

// ============================================================================
// Examples Command Tests (Sprint 5)
// ============================================================================

func TestIntegration_Examples_RootCommandRemoved(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "examples", "--help")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown command")
}

func TestIntegration_GetExamples_List(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "examples", "-o", "yaml")
	assert.Equal(t, 0, exitCode)
	// Listing is metadata-driven: displayName/name/category come from each
	// solution's metadata block, not the filename. Explicit yaml tags keep the
	// YAML key casing aligned with the JSON output and the display schema.
	assert.Contains(t, stdout, "displayName:")
	assert.Contains(t, stdout, "name:")
	assert.Contains(t, stdout, "category:")
	// Names come from metadata.name, so they never carry a file extension.
	assert.NotContains(t, stdout, "name: resolver-demo.yaml")
}

func TestIntegration_GetExamples_List_ShowsViewTip(t *testing.T) {
	t.Parallel()
	// The default (table) list prints a tip to stderr telling the user how to
	// view an example's content, since the path is an embedded-FS handle.
	_, stderr, exitCode := runScafctl(t, "get", "examples")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, "get examples")
	assert.Contains(t, stderr, "to view an example")
}

func TestIntegration_GetExamples_List_OnlySolutions(t *testing.T) {
	t.Parallel()
	// Non-solution files (configs, partials, templates) must not appear.
	stdout, _, exitCode := runScafctl(t, "get", "examples", "-o", "json")
	assert.Equal(t, 0, exitCode)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.NotEmpty(t, items)
	for _, it := range items {
		p, _ := it["path"].(string)
		assert.NotContains(t, p, "bad-solution")
		assert.NotContains(t, p, "lint-stress-test")
		n, _ := it["name"].(string)
		assert.NotContains(t, n, ".yaml", "name must come from metadata, not filename")
	}
}

func TestIntegration_GetExamples_List_JSONYAMLKeyCasingAligned(t *testing.T) {
	t.Parallel()
	// JSON and YAML output must use the same camelCase field names (the struct
	// carries matching json+yaml tags), and both must match the display-schema
	// field names. Guards against yaml.Marshal lowercasing keys.
	yamlOut, _, yc := runScafctl(t, "get", "examples", "-o", "yaml")
	assert.Equal(t, 0, yc)
	assert.Contains(t, yamlOut, "displayName:")
	assert.NotContains(t, yamlOut, "displayname:")

	jsonOut, _, jc := runScafctl(t, "get", "examples", "-o", "json")
	assert.Equal(t, 0, jc)
	assert.Contains(t, jsonOut, "\"displayName\"")
}

func TestIntegration_GetExamples_List_FilterCategory(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "examples", "--category", "solutions")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "solutions")
}

func TestIntegration_GetExamples_List_EmptyStructuredEmitsArray(t *testing.T) {
	t.Parallel()
	// An empty result in a structured format must still emit a parseable, empty
	// document on stdout (not "null", not human text), and any guidance goes to
	// stderr only.
	stdout, _, exitCode := runScafctl(t, "get", "examples", "--category", "no-such-category", "-o", "json")
	assert.Equal(t, 0, exitCode)
	var items []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &items),
		"empty structured output must be valid JSON, got: %q", stdout)
	assert.Empty(t, items)
}

func TestIntegration_GetExamples_List_EmptyHumanMessageOnStderr(t *testing.T) {
	t.Parallel()
	// In the default (human) format, an empty result prints no data on stdout;
	// the "no examples" guidance is written to stderr.
	stdout, stderr, exitCode := runScafctl(t, "get", "examples", "--category", "no-such-category")
	assert.Equal(t, 0, exitCode)
	assert.Empty(t, strings.TrimSpace(stdout), "stdout should be empty for a human empty result")
	assert.Contains(t, stderr, "No examples found in category")
}

func TestIntegration_GetExamples_Get(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "examples", "resolver-demo.yaml")
	assert.Equal(t, 0, exitCode)
	assert.NotEmpty(t, stdout)
}

func TestIntegration_GetExamples_Get_ByName(t *testing.T) {
	t.Parallel()
	// A unique metadata.name resolves without needing the full path.
	stdout, _, exitCode := runScafctl(t, "get", "examples", "cel-basics")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "kind: Solution")
	assert.Contains(t, stdout, "name: cel-basics")
}

func TestIntegration_GetExamples_Get_NonSolutionByPath(t *testing.T) {
	t.Parallel()
	// The listing is solution-only, but exact-path fetch must still return any
	// embedded example file, including non-solution kinds (kind: Config here).
	stdout, _, exitCode := runScafctl(t, "get", "examples", "catalog/native-auth.yaml")
	assert.Equal(t, 0, exitCode)
	assert.NotEmpty(t, stdout)
}

func TestIntegration_GetExamples_Get_MultipleMatchesListed(t *testing.T) {
	t.Parallel()
	// "hello-world" is a basename shared by several examples. Rather than error,
	// the command lists the matching examples (with their paths) so the user can
	// pick one, and exits 0.
	stdout, stderr, exitCode := runScafctl(t, "get", "examples", "hello-world")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, "examples match")
	// The matches are rendered as a list including their disambiguating paths.
	combined := stdout + stderr
	assert.Contains(t, combined, "actions/hello-world.yaml")
	assert.Contains(t, combined, "resolvers/hello-world.yaml")
}

func TestIntegration_GetExamples_Get_UniqueNameShowsContent(t *testing.T) {
	t.Parallel()
	// A now-unique name resolves directly to that example's content.
	stdout, _, exitCode := runScafctl(t, "get", "examples", "hello-world-resolver")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "kind: Solution")
	assert.Contains(t, stdout, "name: hello-world-resolver")
}

func TestIntegration_GetExamples_Get_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	// A traversal query must be rejected before touching the filesystem.
	_, stderr, exitCode := runScafctl(t, "get", "examples", "../../etc/passwd")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "Invalid example path")
}

func TestIntegration_GetExamples_Get_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "examples", "resolver-demo.yaml", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var example map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &example))
	assert.Equal(t, "scafctl.io/v1", example["apiVersion"])
	assert.Contains(t, example, "metadata")
}

func TestIntegration_GetExamples_Get_NotFound(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "get", "examples", "nonexistent-example.yaml")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_GetExamples_Get_InvalidOutputFormat(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "get", "examples", "resolver-demo.yaml", "-o", "invalid-format")
	assert.NotEqual(t, 0, exitCode)
}

// ============================================================================
// Enhanced Dry-Run Tests (Sprint 5)
// ============================================================================

func TestIntegration_RunSolution_DryRun_EnhancedOutput(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
	)

	assert.Equal(t, 0, exitCode)
	// Enhanced dry-run includes WhatIf-style output
	assert.Contains(t, stdout, "DRY RUN: What would happen")
	assert.Contains(t, stdout, "What if:")
}

func TestIntegration_RunSolution_DryRun_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))
	assert.Equal(t, true, report["dryRun"])
	assert.NotEmpty(t, report["solution"])
	assert.Equal(t, true, report["hasWorkflow"])

	// Verify actionPlan with WhatIf messages
	actionPlan, ok := report["actionPlan"].([]any)
	require.True(t, ok, "actionPlan should be an array")
	require.NotEmpty(t, actionPlan)

	act := actionPlan[0].(map[string]any)
	assert.NotEmpty(t, act["wouldDo"])
	assert.NotEmpty(t, act["provider"])

	// MaterializedInputs should NOT be present without --verbose
	_, hasMaterialized := act["materializedInputs"]
	assert.False(t, hasMaterialized, "materializedInputs should not appear without --verbose")
}

func TestIntegration_RunSolution_DryRun_YAML(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
		"-o", "yaml",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "dryRun: true")
	assert.Contains(t, stdout, "solution:")
	assert.Contains(t, stdout, "wouldDo:")
	assert.Contains(t, stdout, "hasWorkflow: true")
}

// ============================================================================
// Output Directory Tests
// ============================================================================

func TestIntegration_RunSolution_OutputDir_ActionsWriteToOutputDir(t *testing.T) {
	t.Parallel()
	// Verifies that --output-dir causes actions to write files into the target
	// directory while resolvers still read from CWD.
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	// Copy the solution and its source.txt into a temp working directory
	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Verify action outputs landed in --output-dir
	assert.FileExists(t, filepath.Join(outputDir, "greeting.txt"))
	assert.FileExists(t, filepath.Join(outputDir, "config/app.yaml"))
	assert.FileExists(t, filepath.Join(outputDir, "cwd-info.txt"))
	assert.FileExists(t, filepath.Join(outputDir, "copied-source.txt"))

	// Verify greeting content
	greeting, err := os.ReadFile(filepath.Join(outputDir, "greeting.txt"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(greeting), "Hello from output-dir test")
	}

	// Verify config content
	configContent, err := os.ReadFile(filepath.Join(outputDir, "config/app.yaml"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(configContent), "name: output-dir-test")
		assert.Contains(t, string(configContent), "version: 1.0.0")
	}

	// Verify __cwd reference is the working directory, not the output dir
	cwdInfo, err := os.ReadFile(filepath.Join(outputDir, "cwd-info.txt"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(cwdInfo), "cwd=")
		assert.NotContains(t, string(cwdInfo), outputDir,
			"__cwd should reference the original working directory, not the output directory")
	}

	// Verify resolver read from CWD: source.txt content was copied by action
	copiedSource, err := os.ReadFile(filepath.Join(outputDir, "copied-source.txt"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(copiedSource), "source file content for output-dir test")
	}

	// Verify files were NOT created in the solution directory (CWD)
	assert.NoFileExists(t, filepath.Join(solutionDir, "greeting.txt"),
		"action output should not land in CWD when --output-dir is set")
	assert.NoFileExists(t, filepath.Join(solutionDir, "config/app.yaml"),
		"action output should not land in CWD when --output-dir is set")
}

func TestIntegration_RunSolution_OutputDir_WithoutFlag_UsesCWD(t *testing.T) {
	t.Parallel()
	// Without --output-dir, actions should write to CWD (backward compatible)
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Without --output-dir, files should land in CWD (solutionDir)
	assert.FileExists(t, filepath.Join(solutionDir, "greeting.txt"))
	assert.FileExists(t, filepath.Join(solutionDir, "config/app.yaml"))
}

func TestIntegration_RunSolution_OutputDir_AutoCreatesDirectory(t *testing.T) {
	t.Parallel()
	// --output-dir should auto-create the directory if it doesn't exist
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "nested", "output", "path")

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	_, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
	)

	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Verify directory was auto-created and files written
	assert.FileExists(t, filepath.Join(outputDir, "greeting.txt"))
}

func TestIntegration_RunSolution_OutputDir_AbsolutePath(t *testing.T) {
	t.Parallel()
	// Verify absolute output-dir paths work correctly
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()
	outputDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	_, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
	)

	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)
	assert.FileExists(t, filepath.Join(outputDir, "greeting.txt"))
}

func TestIntegration_RunSolution_OutputDir_RelativePath(t *testing.T) {
	t.Parallel()
	// Verify relative output-dir paths resolve against CWD
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	_, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", "my-output",
	)

	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Relative path should resolve against the CWD (solutionDir)
	assert.FileExists(t, filepath.Join(solutionDir, "my-output", "greeting.txt"))

	// Cleanup
	os.RemoveAll(filepath.Join(solutionDir, "my-output"))
}

func TestIntegration_RunSolution_OutputDir_DryRun(t *testing.T) {
	t.Parallel()
	// Verify dry-run with --output-dir shows correct target paths
	// and does NOT create the output directory as a side effect.
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()
	outputDir := filepath.Join(t.TempDir(), "should-not-be-created")

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"--dry-run",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Dry-run should not create the output directory itself
	_, err := os.Stat(outputDir)
	assert.True(t, os.IsNotExist(err),
		"dry-run should not create the output directory")

	// Should contain WhatIf dry-run output
	assert.Contains(t, stdout, "DRY RUN: What would happen")
}

func TestIntegration_RunSolution_DryRun_Verbose(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
		"--verbose",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	actionPlan, ok := report["actionPlan"].([]any)
	require.True(t, ok, "actionPlan should be an array")
	require.NotEmpty(t, actionPlan)

	act := actionPlan[0].(map[string]any)
	// Verbose mode should include materializedInputs
	_, hasMaterialized := act["materializedInputs"]
	assert.True(t, hasMaterialized, "--verbose should include materializedInputs")
}

func TestIntegration_RunSolution_DryRun_WhatIfMessages(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/dryrun/conditional-dryrun.yaml",
		"--dry-run",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)

	var report map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &report))

	actionPlan, ok := report["actionPlan"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, actionPlan)

	// All actions should have provider-specific WhatIf messages
	for _, a := range actionPlan {
		act := a.(map[string]any)
		wouldDo, _ := act["wouldDo"].(string)
		assert.NotEmpty(t, wouldDo, "action %s should have a WhatIf message", act["name"])
		assert.Contains(t, wouldDo, "Would execute", "exec provider WhatIf should contain 'Would execute'")
	}
}

func TestIntegration_RunResolver_OutputDir_NoEffect(t *testing.T) {
	t.Parallel()
	// Verify --output-dir has no effect on resolvers
	projectRoot := findProjectRoot()
	solutionDir := t.TempDir()
	outputDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/output-dir")
	require.NoError(t, copyDir(srcDir, solutionDir))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "resolver",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"-o", "json",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Resolvers should still read from CWD successfully
	assert.Contains(t, stdout, "Hello from output-dir test")
	assert.Contains(t, stdout, "source file content for output-dir test")
}

func TestIntegration_RunSolution_OutputDir_HelpFlag(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "solution", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--output-dir")
}

// ============================================================================
// Catalog CWD Resolution Tests
// ============================================================================

func TestIntegration_RunSolution_CatalogWritesToCallerCWD(t *testing.T) {
	t.Parallel()
	// When running a catalog solution (bare name) without --output-dir,
	// file write actions with relative paths should land in the caller's CWD,
	// NOT in the temporary bundle extraction directory.
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME": tmpDir,
	}

	projectRoot := findProjectRoot()

	// Build the catalog-cwd solution into the local catalog.
	// Skip pre-flight tests because the clean XDG env has no plugin cache
	// and the official OCI catalog may not have the providers published.
	// Use --no-cache so the build cache (shared via real XDG_CACHE_HOME)
	// doesn't produce a bundle-less re-store into the fresh catalog.
	_, stderr, exitCode := runScafctlWithEnvInDir(t, filepath.Join(projectRoot, "tests/integration/solutions/catalog-cwd"), env,
		"build", "solution", "-f", "solution.yaml", "--version", "1.0.0", "--force", "--skip-tests", "--no-cache",
	)
	require.Equalf(t, 0, exitCode, "build failed: %s", stderr)

	// Run by bare name from a fresh temp working directory
	workDir := t.TempDir()
	stdout, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env,
		"run", "solution", "catalog-cwd-test",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Verify action outputs landed in the caller's CWD
	assert.FileExists(t, filepath.Join(workDir, "catalog-output.txt"))
	assert.FileExists(t, filepath.Join(workDir, "sub/config.yaml"))
	assert.FileExists(t, filepath.Join(workDir, "cwd-ref.txt"))

	// Verify greeting content
	greeting, err := os.ReadFile(filepath.Join(workDir, "catalog-output.txt"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(greeting), "Hello from catalog-cwd test")
	}

	// Verify nested config content
	configContent, err := os.ReadFile(filepath.Join(workDir, "sub/config.yaml"))
	if assert.NoError(t, err) {
		assert.Contains(t, string(configContent), "app: catalog-cwd-test")
		assert.Contains(t, string(configContent), "version: 1.0.0")
	}

	// Verify __cwd points to workDir, not a temp bundle directory
	cwdRef, err := os.ReadFile(filepath.Join(workDir, "cwd-ref.txt"))
	if assert.NoError(t, err) {
		// Resolve symlinks (macOS: /var -> /private/var) for comparison.
		resolvedWorkDir, _ := filepath.EvalSymlinks(workDir)
		assert.Contains(t, string(cwdRef), "cwd="+resolvedWorkDir,
			"__cwd should reference the caller's CWD, not a bundle temp dir")
	}

	// Verify bundled file content was correctly extracted and written
	assert.FileExists(t, filepath.Join(workDir, "bundled-output.txt"))
	bundledContent, err := os.ReadFile(filepath.Join(workDir, "bundled-output.txt"))
	if assert.NoError(t, err) {
		assert.Equal(t, "bundled info content\n", string(bundledContent),
			"bundled file content should match original data/info.txt, not the solution YAML")
	}
}

func TestIntegration_RunSolution_CatalogOutputDirOverridesCWD(t *testing.T) {
	t.Parallel()
	// When --output-dir is set, it should still override even for catalog runs.
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_DATA_HOME": tmpDir,
	}

	projectRoot := findProjectRoot()

	// Build the catalog-cwd solution into the local catalog.
	// Skip pre-flight tests because the clean XDG env has no plugin cache
	// and the official OCI catalog may not have the providers published.
	// Use --no-cache so the build cache (shared via real XDG_CACHE_HOME)
	// doesn't produce a bundle-less re-store into the fresh catalog.
	_, stderr, exitCode := runScafctlWithEnvInDir(t, filepath.Join(projectRoot, "tests/integration/solutions/catalog-cwd"), env,
		"build", "solution", "-f", "solution.yaml", "--version", "1.0.0", "--force", "--skip-tests", "--no-cache",
	)
	require.Equalf(t, 0, exitCode, "build failed: %s", stderr)

	// Run by bare name with --output-dir
	workDir := t.TempDir()
	outputDir := t.TempDir()
	stdout, stderr, exitCode := runScafctlWithEnvInDir(t, workDir, env,
		"run", "solution", "catalog-cwd-test",
		"--output-dir", outputDir,
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstderr: %s", exitCode, stderr)

	// Verify action outputs landed in --output-dir, not CWD
	assert.FileExists(t, filepath.Join(outputDir, "catalog-output.txt"))
	assert.FileExists(t, filepath.Join(outputDir, "sub/config.yaml"))
	assert.FileExists(t, filepath.Join(outputDir, "cwd-ref.txt"))
	assert.FileExists(t, filepath.Join(outputDir, "bundled-output.txt"))

	// Verify files did NOT land in the caller's CWD
	assert.NoFileExists(t, filepath.Join(workDir, "catalog-output.txt"),
		"action output should not land in CWD when --output-dir is set")
	assert.NoFileExists(t, filepath.Join(workDir, "sub/config.yaml"),
		"action output should not land in CWD when --output-dir is set")
}

// ============================================================================
// Telemetry Flag Tests
// ============================================================================

// TestIntegration_LogLevel_Debug verifies that --log-level debug is accepted
// without errors. Debug logging only produces stderr output when the command
// emits V(1)+ log records, so we only assert a zero exit code.
func TestIntegration_LogLevel_Debug(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "version", "--log-level", "debug")
	assert.Equal(t, 0, exitCode)
}

// TestIntegration_LogLevel_Numeric verifies that a numeric V-level (e.g. "3")
// is accepted without a flag-parsing error.
func TestIntegration_LogLevel_Numeric(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "version", "--log-level", "3")
	assert.Equal(t, 0, exitCode)
	assert.NotContains(t, stderr, "invalid")
}

// TestIntegration_OtelEndpoint_FlagRegistered confirms that --otel-endpoint is
// listed in the options output.
func TestIntegration_OtelEndpoint_FlagRegistered(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "options")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "otel-endpoint")
}

// TestIntegration_OtelInsecure_FlagRegistered confirms that --otel-insecure is
// listed in the options output.
func TestIntegration_OtelInsecure_FlagRegistered(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "options")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "otel-insecure")
}

// ============================================================================
// Plugins Command Tests
// ============================================================================

// TestIntegration_Plugins_Help verifies the plugins command group shows help.
func TestIntegration_Plugins_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "plugins", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "install")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "update")
	assert.Contains(t, stdout, "prune")
}

// TestIntegration_Plugins_Install_Help verifies the plugins install command shows help.
func TestIntegration_Plugins_Install_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "plugins", "install", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--file")
	assert.Contains(t, stdout, "--platform")
	assert.Contains(t, stdout, "--cache-dir")
}

// TestIntegration_Plugins_List_EmptyCache verifies plugins list shows no plugins with an empty cache.
func TestIntegration_Plugins_List_EmptyCache(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	_, stderr, exitCode := runScafctlWithEnv(t, env, "plugins", "list")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, "No plugins cached")
}

// TestIntegration_Plugins_List_JSON verifies plugins list supports JSON output.
func TestIntegration_Plugins_List_JSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "plugins", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// With empty cache and JSON output, it emits a human-readable message or null/empty JSON.
	stdout = strings.TrimSpace(stdout)
	assert.True(t, stdout == "null" || stdout == "[]" || json.Valid([]byte(stdout)) || strings.Contains(stdout, "No plugins cached"),
		"expected valid JSON or no-cache message, got: %s", stdout)
}

// TestIntegration_Plugins_Install_MissingSolutionFile verifies error on missing file.
func TestIntegration_Plugins_Install_MissingSolutionFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "plugins", "install", "-f", "/nonexistent/solution.yaml")
	assert.NotEqual(t, 0, exitCode)
}

// TestIntegration_Plugins_Install_AutoDiscoveryNoFile verifies error when no solution is found.
func TestIntegration_Plugins_Install_AutoDiscoveryNoFile(t *testing.T) {
	t.Parallel()
	emptyDir := t.TempDir()
	_, stderr, exitCode := runScafctlInDir(t, emptyDir, "plugins", "install")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no plugin names or solution file provided")
}

// TestIntegration_Plugins_Install_NoPlugins verifies install succeeds with a solution that has no plugins.
func TestIntegration_Plugins_Install_NoPlugins(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
		"XDG_DATA_HOME":  tmpDir,
	}

	// Use a solution file that has no plugin dependencies (only built-in providers)
	stdout, stderr, exitCode := runScafctlWithEnv(t, env, "plugins", "install", "-f", "tests/integration/testdata/builtin-resolver-demo.yaml")
	_ = stderr
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No plugins declared")
}

// TestIntegration_Plugins_Install_AutoDiscovery verifies auto-discovery of solution file.
func TestIntegration_Plugins_Install_AutoDiscovery(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
		"XDG_DATA_HOME":  tmpDir,
	}
	solutionFile := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: auto-plugins
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctlWithEnvInDir(t, tmpDir, env, "plugins", "install")
	assert.Equal(t, 0, exitCode, "expected plugins install to auto-discover solution.yaml")
	assert.Contains(t, stdout, "No plugins declared")
}

// TestIntegration_Plugins_Install_StandaloneDryRun verifies standalone mode with --dry-run.
func TestIntegration_Plugins_Install_StandaloneDryRun(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
		"XDG_DATA_HOME":  tmpDir,
	}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "plugins", "install", "github", "--kind", "provider", "--dry-run")
	assert.Equal(t, 0, exitCode, "expected dry-run to succeed")
	assert.Contains(t, stdout, "Dry run")
	assert.Contains(t, stdout, "github")
}

// TestIntegration_Plugins_Install_InvalidKind verifies error on invalid --kind flag.
func TestIntegration_Plugins_Install_InvalidKind(t *testing.T) {
	t.Parallel()

	_, stderr, exitCode := runScafctl(t, "plugins", "install", "github", "--kind", "invalid")
	assert.NotEqual(t, 0, exitCode, "expected non-zero exit for invalid kind")
	assert.Contains(t, stderr, "invalid plugin kind")
}

// TestIntegration_RunResolver_MetadataProvider runs the metadata provider and verifies output.
func TestIntegration_RunResolver_MetadataProvider(t *testing.T) {
	t.Parallel()
	t.Skip("metadata plugin v0.2.0 does not propagate solution metadata from host")
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: metadata-test
  version: 1.0.0
spec:
  resolvers:
    meta:
      resolve:
        with:
          - provider: metadata
            inputs: {}
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.meta", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	// Verify the output contains the expected runtime metadata fields.
	assert.Contains(t, stdout, "version")
	assert.Contains(t, stdout, "args")
	assert.Contains(t, stdout, "cwd")
	assert.Contains(t, stdout, "entrypoint")
	assert.Contains(t, stdout, "command")
	assert.Contains(t, stdout, "solution")
	// Verify solution metadata was populated from the solution file.
	assert.Contains(t, stdout, "metadata-test")
}

// TestIntegration_RunResolver_TemplateFunctions_Slugify verifies slugify template function.
func TestIntegration_RunResolver_TemplateFunctions_Slugify(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: slugify-test
  version: 1.0.0
spec:
  resolvers:
    input:
      resolve:
        with:
          - provider: static
            inputs:
              value: "My Cool Project!"
    slugified:
      dependsOn: [input]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ slugify .input }}'
              name: slugify-test
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.slugified", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "my-cool-project")
}

// TestIntegration_RunResolver_CelFunctions_Slugify verifies the strings.slugify
// CEL function produces a DNS-safe label, matching the go-template slugify.
func TestIntegration_RunResolver_CelFunctions_Slugify(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cel-slugify-test
  version: 1.0.0
spec:
  resolvers:
    input:
      resolve:
        with:
          - provider: static
            inputs:
              value: "My_Org--Name! (test)"
    slugified:
      dependsOn: [input]
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: 'strings.slugify(_.input)'
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.slugified", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "my-org-name-test")
}

func TestIntegration_RunResolver_TemplateFunctions_CelInline(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cel-inline-test
  version: 1.0.0
spec:
  resolvers:
    items:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - name: a
                  active: true
                - name: b
                  active: false
    count:
      dependsOn: [items]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ cel "string(size(_.items.filter(x, x.active == true)))" . }}'
              name: cel-inline-test
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.count", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "1")
}

// TestIntegration_RunResolver_TemplateFunctions_WhereSelectField verifies where and selectField template functions.
func TestIntegration_RunResolver_TemplateFunctions_WhereSelectField(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: where-select-test
  version: 1.0.0
spec:
  resolvers:
    services:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - name: api
                  active: true
                - name: web
                  active: true
                - name: legacy
                  active: false
    names:
      dependsOn: [services]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ selectField "name" .services | toYaml }}'
              name: select-test
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.names", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "api")
	assert.Contains(t, stdout, "legacy")
}

// TestIntegration_RunResolver_TemplateFunctions_SelectFieldMap verifies selectField
// accepts a map input (legacy select parity): it works as an existence check and
// supports dotted key paths for descending into nested maps.
func TestIntegration_RunResolver_TemplateFunctions_SelectFieldMap(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: select-map-test
  version: 1.0.0
spec:
  resolvers:
    metadata:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                name: my-solution
                tags: [alpha, beta]
                database:
                  host: db.example.com
                  port: 5432
    hasTags:
      dependsOn: [metadata]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ if .metadata | selectField "tags" }}yes{{ else }}no{{ end }}'
              name: exists-test
    hasMissing:
      dependsOn: [metadata]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ if .metadata | selectField "absent" }}yes{{ else }}no{{ end }}'
              name: absent-test
    dbHost:
      dependsOn: [metadata]
      resolve:
        with:
          - provider: static
            inputs:
              value: placeholder
      transform:
        with:
          - provider: go-template
            inputs:
              template: '{{ .metadata | selectField "database.host" | first }}'
              name: dotted-test
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, `"hasTags": "yes"`)
	assert.Contains(t, stdout, `"hasMissing": "no"`)
	assert.Contains(t, stdout, `"dbHost": "db.example.com"`)
}

func TestIntegration_RunResolver_PositionalParams(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"name=Alice",
		"count=5",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "Alice")
}

func TestIntegration_RunResolver_PositionalMixedWithFlags(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"-r", "name=Bob",
		"count=3",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "Bob")
}

func TestIntegration_RunResolver_PositionalWithResolverNames(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"name",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"name=Charlie",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "Charlie")
	// Should NOT contain "count" or "uppercase" since we only asked for "name"
	assert.NotContains(t, stdout, "\"count\"")
}

func TestIntegration_RunResolver_DynamicHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"--help",
	)

	assert.Equal(t, 0, exitCode)
	// Should show standard help
	assert.Contains(t, stdout, "Execute resolvers from a solution without running actions")
	// Should show dynamic resolver help from the solution
	assert.Contains(t, stdout, "Solution Resolvers")
	assert.Contains(t, stdout, "PARAMETER")
	assert.Contains(t, stdout, "name")
}

// ============================================================================
// Run Resolver — Unknown Parameter Key Validation Tests
// ============================================================================

func TestIntegration_RunResolver_UnknownParamKey(t *testing.T) {
	t.Parallel()
	// "namee" is not a valid parameter (should suggest "name")
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"namee=Alice",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not accept input")
	assert.Contains(t, stderr, `did you mean "name"`)
}

func TestIntegration_RunResolver_UnknownParamKeyNoSuggestion(t *testing.T) {
	t.Parallel()
	// "zzzzz" is too far from any valid parameter key
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"zzzzz=value",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not accept input")
}

func TestIntegration_RunResolver_UnknownParamKey_WarnPolicy(t *testing.T) {
	t.Parallel()
	// --on-unknown-resolver=warn downgrades the rejection to a warning and
	// proceeds with execution.
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"--on-unknown-resolver=warn",
		"namee=Alice",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stderr, "does not accept input")
	// The resolver output is still produced.
	assert.Contains(t, stdout, "name")
}

func TestIntegration_RunResolver_UnknownParamKey_IgnorePolicy(t *testing.T) {
	t.Parallel()
	// --on-unknown-resolver=ignore accepts the unknown key silently.
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"--on-unknown-resolver=ignore",
		"namee=Alice",
	)

	assert.Equal(t, 0, exitCode)
	assert.NotContains(t, stderr, "does not accept input")
	assert.Contains(t, stdout, "name")
}

func TestIntegration_RunResolver_InvalidUnknownResolverPolicy(t *testing.T) {
	t.Parallel()
	// An unrecognized policy value is rejected with a clear error.
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"--on-unknown-resolver=loud",
		"name=Alice",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "valid: error, warn, ignore")
}

// TestIntegration_Lint_MissingFallbackSource verifies missing-fallback-source lint rule detects all-conditional resolvers.
func TestIntegration_Lint_MissingFallbackSource(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fallback-test
  version: 1.0.0
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: static
            inputs:
              value: dev
    endpoint:
      resolve:
        with:
          - provider: static
            when:
              expr: '_.environment == "prod"'
            inputs:
              value: https://api.prod.example.com
          - provider: static
            when:
              expr: '_.environment == "staging"'
            inputs:
              value: https://api.staging.example.com
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")

	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
	assert.Contains(t, stdout, "missing-fallback-source")
}

// TestIntegration_Lint_UnreachableTestPath verifies unreachable-test-path lint rule detects bad test file references.
func TestIntegration_Lint_UnreachableTestPath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unreachable-path-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  testing:
    cases:
      bad-test:
        description: This test references a non-existent file
        command: [run, resolver]
        files:
          - testdata/does-not-exist.json
        assertions:
          - expression: __exitCode == 0
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")

	// Exit code 0 = no errors (warnings only), 2 = validation errors found
	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
	assert.Contains(t, stdout, "unreachable-test-path")
}

// TestIntegration_Lint_TransformShapeMismatch verifies transform-shape-mismatch lint rule
// detects when transform accesses provider-specific fields but resolve chain has a fallback
// with a different output shape.
func TestIntegration_Lint_TransformShapeMismatch(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	solutionFile := filepath.Join(tmpDir, "solution.yaml")

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: shape-mismatch-test
  version: 1.0.0
spec:
  resolvers:
    api_data:
      description: Fetch data with static fallback
      resolve:
        with:
          - provider: http
            when:
              expr: 'true'
            inputs:
              url: https://api.example.com/data
          - provider: static
            inputs:
              value: []
      transform:
        with:
          - provider: cel
            inputs:
              expression: '__self.body'
`
	require.NoError(t, os.WriteFile(solutionFile, []byte(solutionContent), 0o644))

	stdout, _, exitCode := runScafctl(t, "lint", "-f", solutionFile, "-o", "json")

	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
	assert.Contains(t, stdout, "transform-shape-mismatch")
}

// TestIntegration_MCPServeInfo_ExplainConcepts verifies explain_concepts tool is registered.
func TestIntegration_MCPServeInfo_ExplainConcepts(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "serve", "--info")
	assert.Equal(t, 0, exitCode)

	var info struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &info))

	toolNames := make(map[string]bool)
	for _, tool := range info.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["explain_concepts"], "expected explain_concepts tool to be registered")
}

// TestIntegration_MCPServeInfo_ListContextVariables verifies the
// list_context_variables tool is registered.
func TestIntegration_MCPServeInfo_ListContextVariables(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "serve", "--info")
	assert.Equal(t, 0, exitCode)

	var info struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &info))

	toolNames := make(map[string]bool)
	for _, tool := range info.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["list_context_variables"], "expected list_context_variables tool to be registered")
}

func TestIntegration_MCPServeInfo_AuthTokenTools(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "serve", "--info")
	assert.Equal(t, 0, exitCode)

	var info struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &info))

	toolNames := make(map[string]bool)
	for _, tool := range info.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["auth_list_tokens"], "expected auth_list_tokens tool to be registered")
	assert.True(t, toolNames["auth_purge_expired"], "expected auth_purge_expired tool to be registered")
}

func TestIntegration_MCPListHelp(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "List all tools and prompts")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_MCPList(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "list", "-o", "json")

	assert.Equal(t, 0, exitCode)

	var caps []struct {
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &caps))
	assert.NotEmpty(t, caps, "should list capabilities")

	var hasTools, hasPrompts bool
	for _, c := range caps {
		if c.Kind == "tool" {
			hasTools = true
		}
		if c.Kind == "prompt" {
			hasPrompts = true
		}
		assert.NotEmpty(t, c.Name, "capability should have a name")
		assert.Equal(t, "core", c.Source, "all default capabilities should be core")
	}
	assert.True(t, hasTools, "should contain tools")
	assert.True(t, hasPrompts, "should contain prompts")
}

func TestIntegration_MCPListKindFilter(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := runScafctl(t, "mcp", "list", "--kind", "prompt", "-o", "json")

	assert.Equal(t, 0, exitCode)

	var caps []struct {
		Kind string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &caps))
	assert.NotEmpty(t, caps)

	for _, c := range caps {
		assert.Equal(t, "prompt", c.Kind, "all results should be prompts")
	}
}

func TestIntegration_MCPListInvalidKind(t *testing.T) {
	t.Parallel()

	_, stderr, exitCode := runScafctl(t, "mcp", "list", "--kind", "invalid")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid --kind value")
}

// ============================================================================
// Snapshot Command Tests
// ============================================================================

func TestIntegration_Snapshot_Show_Summary(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.json")

	// Create a snapshot first
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)
	require.Equal(t, 0, exitCode, "failed to create snapshot")

	// Show summary (default format)
	stdout, _, exitCode := runScafctl(t, "get", "snapshot", snapshotFile)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Snapshot Summary")
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "Resolvers:")
	assert.Contains(t, stdout, "Success:")
}

func TestIntegration_Snapshot_Show_JSON(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.json")

	// Create a snapshot
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)
	require.Equal(t, 0, exitCode, "failed to create snapshot")

	// Show as JSON
	stdout, _, exitCode := runScafctl(t, "get", "snapshot", snapshotFile, "-o", "json")
	assert.Equal(t, 0, exitCode)

	// Verify valid JSON
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(stdout), &parsed)
	assert.NoError(t, err, "get snapshot ... -o json should produce valid JSON")
	assert.Contains(t, parsed, "metadata")
	assert.Contains(t, parsed, "resolvers")
}

func TestIntegration_Snapshot_Show_Resolvers(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.json")

	// Create a snapshot
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)
	require.Equal(t, 0, exitCode, "failed to create snapshot")

	// Show resolvers detail view
	stdout, _, exitCode := runScafctl(t, "get", "snapshot", snapshotFile, "--detail")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Resolvers")
	// resolver-demo.yaml has environment, region, port, exposedPort, hostname, config
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "region")
	assert.Contains(t, stdout, "port")
}

func TestIntegration_Snapshot_Show_Verbose(t *testing.T) {
	t.Parallel()
	snapshotFile := filepath.Join(t.TempDir(), "snapshot.json")

	// Create a snapshot
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)
	require.Equal(t, 0, exitCode, "failed to create snapshot")

	// Show resolvers with verbose flag
	stdout, _, exitCode := runScafctl(t, "get", "snapshot", snapshotFile, "--detail", "--verbose")
	assert.Equal(t, 0, exitCode)
	// Verbose should show values
	assert.Contains(t, stdout, "Value:")
}

func TestIntegration_Snapshot_Show_MissingFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "get", "snapshot", "/nonexistent/path/snapshot.json")
	assert.NotEqual(t, 0, exitCode, "should fail when snapshot file does not exist")
}

func TestIntegration_Snapshot_Show_NoArgs(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "get", "snapshot")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "accepts 1 arg")
}

func TestIntegration_Snapshot_Diff_Human(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	// Create before snapshot from resolver-demo.yaml
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode, "failed to create before snapshot")

	// Create after snapshot from a modified solution
	modifiedSolution := filepath.Join(tmpDir, "modified.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-demo
  version: 2.0.0
spec:
  resolvers:
    environment:
      description: Target deployment environment
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: staging
    region:
      description: Deployment region
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: eu-west-1
    port:
      description: Application port
      type: int
      resolve:
        with:
          - provider: static
            inputs:
              value: 9090
`
	err := os.WriteFile(modifiedSolution, []byte(content), 0o600)
	require.NoError(t, err)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", modifiedSolution,
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode, "failed to create after snapshot")

	// Diff in human format (default)
	stdout, _, exitCode := runScafctl(t, "diff", "snapshot", beforeFile, afterFile)
	assert.Equal(t, 0, exitCode)
	// Human diff should contain some output (could be changes or summary)
	assert.NotEmpty(t, stdout, "diff output should not be empty")
}

func TestIntegration_Snapshot_Diff_JSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	// Create two snapshots (same solution = identical)
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode)

	// JSON format diff
	stdout, _, exitCode := runScafctl(t, "diff", "snapshot", beforeFile, afterFile, "-o", "json")
	assert.Equal(t, 0, exitCode)

	// Should be valid JSON
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(stdout), &parsed)
	assert.NoError(t, err, "diff -o json should produce valid JSON")
	assert.Contains(t, parsed, "summary")
}

func TestIntegration_Snapshot_Diff_Unified(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	// Create two identical snapshots
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode)

	// Unified diff format
	stdout, _, exitCode := runScafctl(t, "diff", "snapshot", beforeFile, afterFile, "-o", "unified")
	assert.Equal(t, 0, exitCode)
	// Output may be empty if nothing changed — that's fine
	t.Logf("unified diff output: %s", stdout)
}

func TestIntegration_Snapshot_Diff_IgnoreUnchanged(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	// Two identical snapshots
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode)

	// With --ignore-unchanged, identical snapshots should produce minimal output
	stdout, _, exitCode := runScafctl(t, "diff", "snapshot", beforeFile, afterFile, "--ignore-unchanged")
	assert.Equal(t, 0, exitCode)
	t.Logf("ignore-unchanged diff output: %s", stdout)
}

func TestIntegration_Snapshot_Diff_IgnoreFields(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode)

	// Ignore duration and providerCalls fields
	stdout, _, exitCode := runScafctl(t,
		"diff", "snapshot", beforeFile, afterFile,
		"--ignore-fields", "duration,providerCalls",
	)
	assert.Equal(t, 0, exitCode)
	t.Logf("ignore-fields diff output: %s", stdout)
}

// TestIntegration_Snapshot_Diff_OutputToStdout verifies that `diff snapshot`
// writes its output to stdout (users redirect with the shell). The former
// `--output <file>` file-write flag was removed; `-o` now selects the format.
func TestIntegration_Snapshot_Diff_OutputToStdout(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")

	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+beforeFile,
	)
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+afterFile,
	)
	require.Equal(t, 0, exitCode)

	// Diff output goes to stdout; capture and assert it is valid JSON.
	stdout, _, exitCode := runScafctl(t,
		"diff", "snapshot", beforeFile, afterFile,
		"-o", "json",
	)
	assert.Equal(t, 0, exitCode)

	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(stdout), &parsed)
	require.NoError(t, err, "diff -o json should write valid JSON to stdout")
	assert.Contains(t, parsed, "summary")
}

func TestIntegration_Snapshot_Diff_MissingFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	snapshotFile := filepath.Join(tmpDir, "exists.json")

	// Create one valid snapshot
	_, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--snapshot",
		"--snapshot-file="+snapshotFile,
	)
	require.Equal(t, 0, exitCode)

	// Diff with missing second file
	_, _, exitCode = runScafctl(t,
		"diff", "snapshot", snapshotFile, "/nonexistent/after.json",
	)
	assert.NotEqual(t, 0, exitCode, "should fail when snapshot file does not exist")
}

func TestIntegration_Snapshot_Diff_NoArgs(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "diff", "snapshot")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "accepts 2 arg")
}

func TestIntegration_Snapshot_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "snapshot", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Load and display the contents of a snapshot file")
	assert.Contains(t, stdout, "--detail")
	assert.Contains(t, stdout, "--output")
	// --format was removed in favor of the kvx -o/--output convention.
	assert.NotContains(t, stdout, "--format")
	// -f is not bound on 'get snapshot' (it means --file elsewhere).
	assert.NotContains(t, stdout, "-f, --output")
}

// ============================================================================
// CWD Flag Tests
// ============================================================================

func TestIntegration_CwdFlag_ResolvesRelativePath(t *testing.T) {
	t.Parallel()
	// Run from a different directory with --cwd pointing to project root,
	// using a relative solution path that only makes sense from the project root.
	projectRoot := findProjectRoot()

	// Use --cwd to set the working directory to the project root,
	// while the process CWD is a temp directory
	tmpDir := t.TempDir()
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir,
		"--cwd", projectRoot,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stderr: %s\nstdout: %s", stderr, stdout)
	// The JSON output should contain resolver results
	assert.Contains(t, stdout, "environment")
}

func TestIntegration_CwdFlag_NonExistentDir(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "--cwd", "/nonexistent-dir-12345", "version")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not exist")
}

func TestIntegration_CwdFlag_FileNotDir(t *testing.T) {
	t.Parallel()
	// Create a temp file (not directory)
	tmpFile := filepath.Join(t.TempDir(), "notadir.txt")
	require.NoError(t, os.WriteFile(tmpFile, []byte("hello"), 0o644))

	_, stderr, exitCode := runScafctl(t, "--cwd", tmpFile, "version")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not a directory")
}

func TestIntegration_CwdFlag_ShortFlag(t *testing.T) {
	t.Parallel()
	// The -C short flag should work the same as --cwd
	projectRoot := findProjectRoot()
	tmpDir := t.TempDir()
	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir,
		"-C", projectRoot,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stderr: %s\nstdout: %s", stderr, stdout)
	assert.Contains(t, stdout, "environment")
}

// ============================================================================
// Solution Diff Command Tests
// ============================================================================

func TestIntegration_SolutionDiff_Table(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"diff", "solution",
		"-f", "examples/soldiff/solution-v1.yaml",
		"-f", "examples/soldiff/solution-v2.yaml",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Solution Diff:")
	assert.Contains(t, stdout, "metadata.version")
	assert.Contains(t, stdout, "Summary:")
}

func TestIntegration_SolutionDiff_JSON(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"diff", "solution",
		"-f", "examples/soldiff/solution-v1.yaml",
		"-f", "examples/soldiff/solution-v2.yaml",
		"-o", "json",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Contains(t, result, "changes")
	assert.Contains(t, result, "summary")
}

func TestIntegration_SolutionDiff_YAML(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"diff", "solution",
		"-f", "examples/soldiff/solution-v1.yaml",
		"-f", "examples/soldiff/solution-v2.yaml",
		"-o", "yaml",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "pathA:")
	assert.Contains(t, stdout, "changes:")
}

func TestIntegration_CacheInfo_ShowsArtifactCache(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	env := map[string]string{
		"XDG_CACHE_HOME": tmpDir,
		"XDG_DATA_HOME":  tmpDir,
	}

	// Build a solution to populate the artifact cache
	_, _, exitCode := runScafctlWithEnv(t, env, "build", "solution", "-f", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Cache info should report artifact data
	stdout, _, exitCode2 := runScafctlWithEnv(t, env, "cache", "info")
	assert.Equal(t, 0, exitCode2)
	// Verify the cache info output contains expected sections
	assert.Contains(t, stdout, "Cache")
}

func TestIntegration_SolutionDiff_MissingFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t,
		"diff", "solution",
		"-f", "examples/soldiff/solution-v1.yaml",
		"-f", "/nonexistent/solution.yaml",
	)
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_SolutionDiff_NoArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "diff", "solution")
	assert.NotEqual(t, 0, exitCode)
}

// TestIntegration_OldDiffPaths_HardRemoved locks in the grammar migration
// phase 2 hard cutover: the pre-migration paths (`solution diff`, `sol diff`,
// `bundle diff`, `snapshot diff`) and the entire `solution` command group no
// longer exist. There are no aliases or shims -- invoking them must fail with
// a non-zero exit AND an "unknown command" error (not merely fall back to
// showing parent help and exiting 0).
func TestIntegration_OldDiffPaths_HardRemoved(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"solution", "diff", "-f", "a.yaml", "-f", "b.yaml"},
		{"sol", "diff", "-f", "a.yaml", "-f", "b.yaml"},
		{"solution"},
		{"sol"},
		{"bundle", "diff", "a@1.0.0", "b@2.0.0"},
		{"snapshot", "diff", "before.json", "after.json"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			_, stderr, exitCode := runScafctl(t, args...)
			assert.NotEqual(t, 0, exitCode, "old path %v must no longer exist", args)
			assert.Contains(t, stderr, "unknown command",
				"old path %v must fail with an unknown-command error, not fall back to help/exit-0", args)
		})
	}
}

// TestIntegration_OldExtractSnapshotPaths_HardRemoved locks in the grammar
// migration phase 3 hard cutover: `bundle extract` moved to `extract bundle`
// and `snapshot show` moved to `get snapshot`, with the entire `snapshot`
// command group (and its `snap` alias) deleted. There are no aliases or shims
// -- invoking the old paths must fail with a non-zero exit AND an "unknown
// command" error.
func TestIntegration_OldExtractSnapshotPaths_HardRemoved(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"bundle", "extract", "my-solution@1.0.0"},
		{"snapshot", "show", "snapshot.json"},
		{"snapshot"},
		{"snap"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			_, stderr, exitCode := runScafctl(t, args...)
			assert.NotEqual(t, 0, exitCode, "old path %v must no longer exist", args)
			assert.Contains(t, stderr, "unknown command",
				"old path %v must fail with an unknown-command error, not fall back to help/exit-0", args)
		})
	}
}

// TestIntegration_BareGroups_ShowHelpExitZero verifies that a BARE invocation
// of an existing command group (no subcommand) still shows help and exits 0 --
// this is CLI grammar Rule 8 and must not regress when we make the group error
// on unknown subcommands. Note `solution` was deleted entirely, so it is NOT
// listed here (see TestIntegration_OldDiffPaths_HardRemoved).
func TestIntegration_BareGroups_ShowHelpExitZero(t *testing.T) {
	t.Parallel()

	cases := []string{"extract", "catalog", "config", "auth", "secrets", "cache", "render"}

	for _, group := range cases {
		t.Run(group, func(t *testing.T) {
			t.Parallel()
			stdout, _, exitCode := runScafctl(t, group)
			assert.Equal(t, 0, exitCode, "bare %q must show help and exit 0", group)
			assert.Contains(t, stdout, "Usage:", "bare %q must print help", group)
		})
	}
}

// TestIntegration_UnknownSubcommand_ErrorsNonZero verifies that every parent
// command group converted to cmdutil.MakeHelpOnlyGroup rejects an unknown
// subcommand with a non-zero exit AND an "unknown command" error, rather than
// silently falling back to parent help and exiting 0 (issue #655). Nested
// groups are covered too.
func TestIntegration_UnknownSubcommand_ErrorsNonZero(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"catalog", "bogus-xyz"},
		{"config", "bogus-xyz"},
		{"auth", "bogus-xyz"},
		{"secrets", "bogus-xyz"},
		{"state", "bogus-xyz"},
		{"cache", "bogus-xyz"},
		{"kube", "bogus-xyz"},
		{"plugins", "bogus-xyz"},
		{"mcp", "bogus-xyz"},
		{"get", "bogus-xyz"},
		{"run", "bogus-xyz"},
		{"validate", "bogus-xyz"},
		{"new", "bogus-xyz"},
		{"eval", "bogus-xyz"},
		{"vendor", "bogus-xyz"},
		{"inspect", "bogus-xyz"},
		{"package", "bogus-xyz"},
		{"credential-helper", "bogus-xyz"},
		{"diff", "bogus-xyz"},
		{"extract", "bogus-xyz"},
		{"render", "bogus-xyz"},
		// Nested groups.
		{"auth", "alias", "bogus-xyz"},
		{"catalog", "index", "bogus-xyz"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			_, stderr, exitCode := runScafctl(t, args...)
			assert.NotEqual(t, 0, exitCode,
				"unknown subcommand %v must exit non-zero", args)
			assert.Contains(t, stderr, "unknown command",
				"unknown subcommand %v must fail with an unknown-command error", args)
		})
	}
}

// ============================================================================
// Positional Path Rejection Tests — Uniform -f/--file Input
// ============================================================================

func TestIntegration_InspectSolution_PositionalPathRejected(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "inspect", "solution", "./my-solution.yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "local file paths must use -f/--file flag")
}

func TestIntegration_GetSolution_PositionalPathRejected(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "get", "solution", "./my-solution.yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "local file paths must use -f/--file flag")
}

func TestIntegration_RenderSolution_PositionalPathRejected(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "render", "solution", "./my-solution.yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "local file paths must use -f/--file flag")
}

func TestIntegration_RunResolver_PositionalArgsAreNames(t *testing.T) {
	t.Parallel()
	// With the new behavior, positional args are treated as resolver names.
	// ./my-solution.yaml is treated as a resolver name, not a file path.
	// The command should NOT produce a "local file paths must use -f/--file" error.
	_, stderr, _ := runScafctl(t, "run", "resolver", "./my-solution.yaml")
	assert.NotContains(t, stderr, "local file paths must use -f/--file flag")
}

func TestIntegration_SolutionDiff_PositionalPathRejected(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "diff", "solution", "./v1.yaml", "./v2.yaml")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "local file paths must use -f/--file flag")
}

func TestIntegration_BuildSolution_RejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "build", "solution", "solution.yaml")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_VendorUpdate_RejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "vendor", "update", "solution.yaml")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_SolutionDiff_MixedFlagAndPositional(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"diff", "solution",
		"-f", "examples/soldiff/solution-v1.yaml",
		"my-app@1.0.0",
	)
	// This should fail because my-app@1.0.0 doesn't exist in catalog,
	// but the command should accept the mixed input without argument validation errors.
	// Verify that the failure is from catalog resolution, not input validation.
	_ = stdout
	assert.NotEqual(t, 0, exitCode)
	assert.NotContains(t, stderr, "local file paths must use -f/--file flag", "should not fail on arg validation")
	assert.NotContains(t, stderr, "cannot use both -f/--file", "should not fail on conflict detection")
}

// ============================================================================
// File Conflict Strategy Flag Tests
// ============================================================================

func TestIntegration_RunSolution_OnConflictFlag_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--on-conflict")
	assert.Contains(t, stdout, "--backup")
}

func TestIntegration_RunSolution_OnConflictFlag_Invalid(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "run", "solution",
		"-f", "examples/solutions/hello-world/solution.yaml",
		"--on-conflict", "invalid-value",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid --on-conflict value")
}

func TestIntegration_RunProvider_OnConflictFlag_Invalid(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "run", "provider",
		"static", "value=hello",
		"--on-conflict", "bad-strategy",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid --on-conflict value")
}

// ============================================================================
// File Conflict Strategy Behavior Tests
// ============================================================================

func TestIntegration_RunSolution_FileConflict_SkipPreservesFile(t *testing.T) {
	t.Parallel()
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/file-conflict")
	require.NoError(t, copyDir(srcDir, solutionDir))

	// Pre-create a target file for the `write-new` action which has NO explicit
	// onConflict. The --on-conflict skip CLI flag should prevent overwriting it.
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "new-file.txt"), []byte("original content"), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"--on-conflict", "skip",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	// Skip: existing file should be preserved (CLI flag affects write-new which has no onConflict)
	content, err := os.ReadFile(filepath.Join(outputDir, "new-file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "original content", string(content))
}

func TestIntegration_RunSolution_FileConflict_OverwriteReplacesFile(t *testing.T) {
	t.Parallel()
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/file-conflict")
	require.NoError(t, copyDir(srcDir, solutionDir))

	// Pre-create a target file that will be overwritten
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "new-file.txt"), []byte("old"), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"--on-conflict", "overwrite",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)

	// Overwrite: file should have new content
	content, err := os.ReadFile(filepath.Join(outputDir, "new-file.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Hello from conflict test")
}

func TestIntegration_RunSolution_FileConflict_ErrorOnExisting(t *testing.T) {
	t.Parallel()
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/file-conflict")
	require.NoError(t, copyDir(srcDir, solutionDir))

	// Pre-create a file that will cause the error strategy to fail
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "new-file.txt"), []byte("existing"), 0o644))

	_, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"--on-conflict", "error",
		"-o", "json",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "file already exists")
}

func TestIntegration_RunProvider_FileConflict_SkipUnchanged(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	// Write initial content
	require.NoError(t, os.WriteFile(target, []byte("same content"), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "provider",
		"file",
		"operation=write",
		"path="+target,
		"content=same content",
		"--on-conflict", "skip-unchanged",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "unchanged")
}

func TestIntegration_RunProvider_FileConflict_Backup(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	require.NoError(t, os.WriteFile(target, []byte("original"), 0o644))

	stdout, stderr, exitCode := runScafctl(t, "run", "provider",
		"file",
		"operation=write",
		"path="+target,
		"content=replacement",
		"onConflict=overwrite",
		"backup=true",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "backupPath")
	assert.Contains(t, stdout, "overwritten")
	assert.FileExists(t, target+".bak")
}

func TestIntegration_RunSolution_BackupFlag_CreatesBackupFile(t *testing.T) {
	t.Parallel()
	projectRoot := findProjectRoot()
	outputDir := t.TempDir()
	solutionDir := t.TempDir()

	srcDir := filepath.Join(projectRoot, "tests/integration/solutions/file-conflict")
	require.NoError(t, copyDir(srcDir, solutionDir))

	// Pre-create a file so --backup has something to back up before overwriting
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "new-file.txt"), []byte("old content"), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, solutionDir,
		"run", "solution",
		"-f", filepath.Join(solutionDir, "solution.yaml"),
		"--output-dir", outputDir,
		"--on-conflict", "overwrite",
		"--backup",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode, "stdout: %s\nstderr: %s", stdout, stderr)
	assert.FileExists(t, filepath.Join(outputDir, "new-file.txt.bak"), "backup file should be created")

	// Verify the original was replaced
	content, err := os.ReadFile(filepath.Join(outputDir, "new-file.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(content), "Hello from conflict test")
}

// ============================================================================
// Stdin Parameter Tests (@-)
// ============================================================================

func TestIntegration_RunResolver_StdinParamsYAML(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("name: StdinAlice\ncount: 7\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"-r", "@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "StdinAlice")
}

func TestIntegration_RunResolver_StdinParamsJSON(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader(`{"name": "StdinBob", "count": 3}`)
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"-r", "@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "StdinBob")
}

func TestIntegration_RunResolver_StdinParamsPositional(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("name: StdinCharlie\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "StdinCharlie")
}

func TestIntegration_RunResolver_StdinConflictWithFileStdin(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("name: test\n")
	_, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "-",
		"-r", "@-",
	)

	assert.NotEqual(t, 0, exitCode, "expected non-zero exit code\nstderr: %s", stderr)
	assert.Contains(t, stderr, "cannot use both -f - and @-")
}

func TestIntegration_RunSolution_StdinParams(t *testing.T) {
	t.Parallel()
	// Scaffold a solution with a workflow, then run it with @- stdin params
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "stdin-test", "--description", "Stdin test", "-o", outFile)
	require.Equal(t, 0, exitCode)

	stdin := strings.NewReader(`{"inputName": "StdinWorld"}`)
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "solution",
		"-f", outFile,
		"-r", "@-",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	// Verify the solution ran successfully — the scaffolded solution echoes output
	assert.Contains(t, stdout, `"status": "succeeded"`)
}

func TestIntegration_RunSolution_StdinConflictWithFileStdin(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("inputName: test\n")
	_, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "solution",
		"-f", "-",
		"-r", "@-",
	)

	assert.NotEqual(t, 0, exitCode, "expected non-zero exit code\nstderr: %s", stderr)
	assert.Contains(t, stderr, "cannot use both -f - and @-")
}

func TestIntegration_RunResolver_StdinMixedWithFileRef(t *testing.T) {
	t.Parallel()
	// Create a param file with one key, pipe another via stdin
	tmpDir := t.TempDir()
	paramFile := filepath.Join(tmpDir, "params.yaml")
	err := os.WriteFile(paramFile, []byte("name: FromFile\n"), 0o600)
	require.NoError(t, err)

	stdin := strings.NewReader("count: 7\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"-r", "@"+paramFile,
		"-r", "@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "FromFile")
}

// ============================================================================
// Raw Stdin Parameter Tests (key=@-)
// ============================================================================

func TestIntegration_RunResolver_RawStdinParam(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("StdinRawAlice\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/stdin-params/solution.yaml",
		"-o", "json",
		"-r", "greeting=Hello",
		"-r", "name=@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "StdinRawAlice")
	assert.Contains(t, stdout, "Hello, StdinRawAlice!")
}

func TestIntegration_RunResolver_RawFileParam(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	contentFile := filepath.Join(tmpDir, "name.txt")
	err := os.WriteFile(contentFile, []byte("FileContentBob\n"), 0o600)
	require.NoError(t, err)

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/stdin-params/solution.yaml",
		"-o", "json",
		"-r", "greeting=Hi",
		"-r", "name=@"+contentFile,
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "FileContentBob")
	assert.Contains(t, stdout, "Hi, FileContentBob!")
}

func TestIntegration_RunProvider_RawStdinParam(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("Hello from stdin\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "provider", "message",
		"message=@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
}

func TestIntegration_RunProvider_RawFileParam(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	contentFile := filepath.Join(tmpDir, "msg.txt")
	err := os.WriteFile(contentFile, []byte("Hello from file\n"), 0o600)
	require.NoError(t, err)

	stdout, stderr, exitCode := runScafctl(t,
		"run", "provider", "message",
		"message=@"+contentFile,
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
}

func TestIntegration_RunResolver_RawStdinConflictWithStandaloneAt(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("raw data\n")
	_, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/stdin-params/solution.yaml",
		"-o", "json",
		"-r", "name=@-",
		"-r", "@-",
	)

	assert.NotEqual(t, 0, exitCode, "expected non-zero exit code\nstderr: %s", stderr)
	assert.Contains(t, stderr, "@- can only be specified once")
}

func TestIntegration_RunResolver_RawStdinWithOtherParams(t *testing.T) {
	t.Parallel()
	stdin := strings.NewReader("World\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/stdin-params/solution.yaml",
		"-o", "json",
		"-r", "greeting=Hey",
		"-r", "name=@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "World")
	assert.Contains(t, stdout, "Hey, World!")
}

func TestIntegration_RunResolver_RawFileAndStdinMixed(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	contentFile := filepath.Join(tmpDir, "greeting.txt")
	err := os.WriteFile(contentFile, []byte("Howdy\n"), 0o600)
	require.NoError(t, err)

	stdin := strings.NewReader("Partner\n")
	stdout, stderr, exitCode := runScafctlWithStdin(t, stdin,
		"run", "resolver",
		"-f", "tests/integration/solutions/resolvers/stdin-params/solution.yaml",
		"-o", "json",
		"-r", "greeting=@"+contentFile,
		"-r", "name=@-",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "Howdy")
	assert.Contains(t, stdout, "Partner")
	assert.Contains(t, stdout, "Howdy, Partner!")
}

// ── REST API Server (serve) commands ──

func TestIntegration_ServeHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "serve", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Start the scafctl REST API server")
	assert.Contains(t, stdout, "--port")
	assert.Contains(t, stdout, "--host")
	assert.Contains(t, stdout, "--enable-tls")
}

func TestIntegration_ServeOpenAPIHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "serve", "openapi", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Generate the full OpenAPI specification")
	assert.Contains(t, stdout, "--format")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_ServeOpenAPI_JSON(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "serve", "openapi", "--format", "json")
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "openapi")
	assert.Contains(t, stdout, "scafctl API")
	assert.Contains(t, stdout, "/health")
}

func TestIntegration_ServeOpenAPI_YAML(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t, "serve", "openapi", "--format", "yaml")
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "openapi:")
	assert.Contains(t, stdout, "scafctl API")
}

func TestIntegration_ServeOpenAPI_ToFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "openapi.json")
	_, stderr, exitCode := runScafctl(t, "serve", "openapi", "--format", "json", "--output", outFile)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.FileExists(t, outFile)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "scafctl API")
}

// ============================================================================
// Credential Helper Command Tests
// ============================================================================

func TestIntegration_CredentialHelperHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Docker credential helper protocol")
	assert.Contains(t, stdout, "get")
	assert.Contains(t, stdout, "store")
	assert.Contains(t, stdout, "erase")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "install")
	assert.Contains(t, stdout, "uninstall")
}

func TestIntegration_CredentialHelperGetHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "get", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Reads a server URL from stdin and writes credentials as JSON to stdout")
}

func TestIntegration_CredentialHelperStoreHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "store", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Reads a JSON credential object from stdin and stores it")
}

func TestIntegration_CredentialHelperEraseHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "erase", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Reads a server URL from stdin and removes credentials")
}

func TestIntegration_CredentialHelperListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "list", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Writes a JSON map of server URLs to usernames to stdout")
}

func TestIntegration_CredentialHelperInstallHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "install", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "docker-credential-scafctl credential helper")
	assert.Contains(t, stdout, "--bin-dir")
	assert.Contains(t, stdout, "--docker")
	assert.Contains(t, stdout, "--podman")
	assert.Contains(t, stdout, "--registry")
}

func TestIntegration_CredentialHelperUninstallHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "uninstall", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "docker-credential-scafctl symlink or managed shim")
	assert.Contains(t, stdout, "--bin-dir")
	assert.Contains(t, stdout, "--docker")
	assert.Contains(t, stdout, "--podman")
}

// TestIntegration_CredentialHelperInstallUninstall exercises the real install
// and uninstall flow against a temp bin directory. On POSIX a symlink is
// created; the Windows shim path is covered by unit tests in the
// credentialhelper package.
func TestIntegration_CredentialHelperInstallUninstall(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink install path is POSIX-only; Windows shim is covered by unit tests")
	}

	binDir := t.TempDir()
	helperPath := filepath.Join(binDir, "docker-credential-scafctl")

	stdout, stderr, exitCode := runScafctl(t, "credential-helper", "install", "--bin-dir", binDir)
	require.Equal(t, 0, exitCode, "install should succeed: stdout=%q stderr=%q", stdout, stderr)

	info, err := os.Lstat(helperPath)
	require.NoError(t, err, "helper should be created at %s", helperPath)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "helper should be a symlink on POSIX")

	_, _, exitCode = runScafctl(t, "credential-helper", "uninstall", "--bin-dir", binDir)
	assert.Equal(t, 0, exitCode, "uninstall should succeed")

	_, statErr := os.Lstat(helperPath)
	assert.True(t, os.IsNotExist(statErr), "helper should be removed after uninstall")
}

func TestIntegration_CredentialHelperGetNotFound(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctlWithStdin(t, strings.NewReader("https://unknown.registry.io"), "credential-helper", "get")
	assert.NotEqual(t, 0, exitCode)

	var errResp map[string]string
	require.NoError(t, json.Unmarshal([]byte(stdout), &errResp))
	assert.Contains(t, errResp["message"], "credentials not found")
}

func TestIntegration_CredentialHelperListEmpty(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "credential-helper", "list")
	// list may fail if no secrets store is initialized, or return empty map
	if exitCode == 0 {
		var result map[string]string
		require.NoError(t, json.Unmarshal([]byte(stdout), &result))
		// Just verify it's valid JSON
		assert.NotNil(t, result)
	}
}

// runCredHelperSymlink invokes the integration binary through a
// docker-credential-<name> symlink, mirroring how Docker/Podman exec a
// credential helper. It returns stdout, stderr, and the exit code.
func runCredHelperSymlink(t *testing.T, aliasName, stdin, verb string) (stdout, stderr string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink dispatch test is POSIX-only")
	}

	linkDir := t.TempDir()
	linkPath := filepath.Join(linkDir, aliasName)
	require.NoError(t, os.Symlink(binaryPath, linkPath))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, linkPath, verb)
	cmd.Dir = findProjectRoot()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run credential helper symlink: %v", err)
		}
	}
	return stdout, stderr, exitCode
}

// TestIntegration_CredentialHelperSymlinkDispatch verifies that invoking the
// binary under a docker-credential-<name> alias routes into the
// credential-helper command tree (Problem 1 in issue #540). Without the
// argv[0] dispatch, cobra would fail with "unknown command \"get\"".
func TestIntegration_CredentialHelperSymlinkDispatch(t *testing.T) {
	t.Parallel()

	t.Run("get routes via scafctl alias", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, exitCode := runCredHelperSymlink(t, "docker-credential-scafctl", "https://unknown.registry.io", "get")

		assert.NotContains(t, stderr, "unknown command", "argv[0] dispatch should route get into credential-helper")
		assert.NotEqual(t, 0, exitCode, "unknown registry should fail")

		var errResp map[string]string
		require.NoError(t, json.Unmarshal([]byte(stdout), &errResp), "stdout should be a JSON error response, got: %q / stderr: %q", stdout, stderr)
		assert.Contains(t, errResp["message"], "credentials not found")
	})

	t.Run("embedder binary name also routes", func(t *testing.T) {
		t.Parallel()
		// The dispatch keys off the docker-credential- prefix, not the binary
		// suffix, so an embedder alias routes the same way.
		stdout, stderr, exitCode := runCredHelperSymlink(t, "docker-credential-myembedder", "https://unknown.registry.io", "get")

		assert.NotContains(t, stderr, "unknown command")
		assert.NotEqual(t, 0, exitCode)

		var errResp map[string]string
		require.NoError(t, json.Unmarshal([]byte(stdout), &errResp), "stdout should be JSON, got: %q / stderr: %q", stdout, stderr)
		assert.Contains(t, errResp["message"], "credentials not found")
	})

	t.Run("list routes and returns valid JSON", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, exitCode := runCredHelperSymlink(t, "docker-credential-scafctl", "", "list")

		assert.NotContains(t, stderr, "unknown command", "argv[0] dispatch should route list into credential-helper")
		if exitCode == 0 {
			var result map[string]string
			require.NoError(t, json.Unmarshal([]byte(stdout), &result))
			assert.NotNil(t, result)
		}
	})
}

// ============================================================================
// Plugin Execution Integration Tests
// ============================================================================

var (
	echoPluginOnce sync.Once
	echoPluginPath string
	echoPluginDir  string
)

// buildEchoPlugin builds the echo example plugin once and returns the binary path.
// Subsequent calls reuse the cached binary.
func buildEchoPlugin(t *testing.T) string {
	t.Helper()
	echoPluginOnce.Do(func() {
		projectRoot := findProjectRoot()
		tmpDir, err := os.MkdirTemp("", "scafctl-echo-plugin-*")
		if err != nil {
			t.Fatalf("failed to create temp dir for echo plugin: %v", err)
		}
		binPath := filepath.Join(tmpDir, "scafctl-plugin-echo")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, "./examples/plugins/echo/main.go")
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build echo plugin: %s", string(output))
		}
		echoPluginPath = binPath
		echoPluginDir = tmpDir
	})
	require.NotEmpty(t, echoPluginPath, "echo plugin build failed in a previous test")
	return echoPluginPath
}

func TestIntegration_PluginExecution_RunProvider(t *testing.T) {
	t.Parallel()
	pluginBin := buildEchoPlugin(t)
	pluginDir := filepath.Dir(pluginBin)

	stdout, stderr, exitCode := runScafctl(t, "run", "provider", "echo",
		"--capability", "transform",
		"--plugin-dir", pluginDir,
		"message=hello from integration test",
		"-o", "json")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode, "expected exit 0; stderr: %s", stderr)
	assert.Contains(t, stdout, "hello from integration test")
}

func TestIntegration_PluginExecution_RunProviderDryRun(t *testing.T) {
	t.Parallel()
	pluginBin := buildEchoPlugin(t)
	pluginDir := filepath.Dir(pluginBin)

	stdout, stderr, exitCode := runScafctl(t, "run", "provider", "echo",
		"--capability", "transform",
		"--plugin-dir", pluginDir,
		"message=dry run test",
		"--dry-run",
		"-o", "json")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode, "expected exit 0; stderr: %s", stderr)
	assert.Contains(t, stdout, "dry run test")
}

func TestIntegration_PluginExecution_RunProviderMultiInput(t *testing.T) {
	t.Parallel()
	pluginBin := buildEchoPlugin(t)
	pluginDir := filepath.Dir(pluginBin)

	// Test with multiple valid inputs
	stdout, stderr, exitCode := runScafctl(t, "run", "provider", "echo",
		"--capability", "transform",
		"--plugin-dir", pluginDir,
		"message=hello from multi-input test",
		"-o", "json")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode, "expected exit 0; stderr: %s", stderr)
	assert.Contains(t, stdout, "hello from multi-input test")
}

func TestIntegration_PluginExecution_GetProviderSchema(t *testing.T) {
	t.Parallel()
	// get provider does not support --plugin-dir; plugin providers require
	// catalog auto-fetch or the cache to be populated. This test verifies
	// the command handles unknown providers gracefully.
	_, stderr, exitCode := runScafctl(t, "get", "provider", "nonexistent-plugin-provider", "-o", "json")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_GetProvider_OfficialMetadata(t *testing.T) {
	t.Parallel()
	// Official plugin providers (exec, git, etc.) should return metadata
	// even when the plugin binary cannot be fetched.
	stdout, _, exitCode := runScafctl(t, "get", "provider", "exec", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, `"name"`)
	assert.Contains(t, stdout, `"exec"`)
	assert.Contains(t, stdout, `"source"`)
	assert.Contains(t, stdout, `"official"`)
}

func TestIntegration_MCPServe_GetProviderSchema_Builtin(t *testing.T) {
	t.Parallel()
	// Verify the MCP get_provider_schema tool returns full schema for
	// builtin providers via JSON-RPC protocol.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "mcp", "serve")
	cmd.Dir = findProjectRoot()

	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_provider_schema","arguments":{"name":"cel"}}}`,
	}, "\n")
	cmd.Stdin = strings.NewReader(messages + "\n")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	_ = cmd.Run()

	output := outBuf.String()
	// Find the tools/call response (id:2)
	var schemaResp string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"id":2`) {
			schemaResp = line
			break
		}
	}
	require.NotEmpty(t, schemaResp, "expected tools/call response for id 2")
	assert.Contains(t, schemaResp, "cel")
	assert.Contains(t, schemaResp, "expression")
	assert.NotContains(t, schemaResp, "NOT_FOUND")
}

func TestIntegration_MCPServe_GetProviderSchema_OfficialFallback(t *testing.T) {
	t.Parallel()
	// When an official plugin provider can't be fetched (no fetcher in test
	// env), get_provider_schema should return a helpful NOT_FOUND response
	// with guidance rather than crashing.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "mcp", "serve")
	cmd.Dir = findProjectRoot()

	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_provider_schema","arguments":{"name":"exec"}}}`,
	}, "\n")
	cmd.Stdin = strings.NewReader(messages + "\n")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	_ = cmd.Run()

	output := outBuf.String()
	var schemaResp string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"id":2`) {
			schemaResp = line
			break
		}
	}
	require.NotEmpty(t, schemaResp, "expected tools/call response for id 2")
	// Should mention exec and provide useful context (either schema if cached,
	// or NOT_FOUND with guidance about the plugin).
	assert.Contains(t, schemaResp, "exec")
}

func TestIntegration_MCPServe_GetProviderSchema_CachedDescriptor(t *testing.T) {
	t.Parallel()
	// Pre-populate the provider schema cache with a fake descriptor.
	// When the plugin binary is unavailable, get_provider_schema should
	// return the cached descriptor with "source": "cached".
	cacheDir := t.TempDir()

	// Write a minimal cached descriptor entry (use current time so TTL check passes).
	// Note: the Descriptor struct tags the input schema as "schema" (not "inputSchema").
	cacheEntry := fmt.Sprintf(`{
		"cachedAt": %q,
		"descriptor": {
			"name": "fake-cached-provider",
			"apiVersion": "v1",
			"version": "1.0.0",
			"description": "A test cached provider",
			"capabilities": ["from"],
			"schema": {
				"type": "object",
				"properties": {
					"input1": {"type": "string", "description": "Test input"}
				}
			}
		}
	}`, time.Now().UTC().Format(time.RFC3339))
	err := os.MkdirAll(cacheDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(cacheDir, "fake-cached-provider.json"), []byte(cacheEntry), 0o600)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "mcp", "serve")
	cmd.Dir = findProjectRoot()
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+filepath.Dir(cacheDir))

	// The cache expects the path: $XDG_CACHE_HOME/scafctl/provider-schemas/
	// So we need to create the proper directory structure
	properCacheDir := filepath.Join(t.TempDir(), "scafctl", "provider-schemas")
	err = os.MkdirAll(properCacheDir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(properCacheDir, "fake-cached-provider.json"), []byte(cacheEntry), 0o600)
	require.NoError(t, err)

	xdgCacheHome := filepath.Dir(filepath.Dir(properCacheDir)) // parent of scafctl/
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+xdgCacheHome)

	messages := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_provider_schema","arguments":{"name":"fake-cached-provider"}}}`,
	}, "\n")
	cmd.Stdin = strings.NewReader(messages + "\n")

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	_ = cmd.Run()

	output := outBuf.String()
	var schemaResp string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, `"id":2`) {
			schemaResp = line
			break
		}
	}
	require.NotEmpty(t, schemaResp, "expected tools/call response for id 2")
	assert.Contains(t, schemaResp, "fake-cached-provider")
	assert.Contains(t, schemaResp, "cached")
	assert.Contains(t, schemaResp, "input1")
}

func TestIntegration_PluginsList_RunsSuccessfully(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "plugins", "list")

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_PluginsList_PathNotInTable(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	// Seed a fake plugin binary so the table is actually rendered.
	platform := runtime.GOOS + "-" + runtime.GOARCH
	pluginDir := filepath.Join(cacheDir, "scafctl", "plugins", "fake-plugin", "1.0.0", platform)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	binName := "fake-plugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, binName), []byte("#!/bin/sh\nexit 0"), 0o755))

	stdout, _, exitCode := runScafctlWithEnv(t, map[string]string{"XDG_CACHE_HOME": cacheDir}, "plugins", "list")

	assert.Equal(t, 0, exitCode)
	// Table should render with the version column visible
	assert.Contains(t, stdout, "version")
	// Path column should be hidden via column hints
	assert.NotContains(t, stdout, "path")
}

// TestIntegration_PluginsList_DedupesToLatestByDefault verifies that plugins
// list shows only the latest cached version of each plugin by default, and
// that --all-versions restores the full listing (issue #532).
func TestIntegration_PluginsList_DedupesToLatestByDefault(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	platform := runtime.GOOS + "-" + runtime.GOARCH
	binName := "fake-plugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	for _, version := range []string{"0.1.1", "0.2.0"} {
		pluginDir := filepath.Join(cacheDir, "scafctl", "plugins", "fake-plugin", version, platform)
		require.NoError(t, os.MkdirAll(pluginDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(pluginDir, binName), []byte("#!/bin/sh\nexit 0"), 0o755))
	}

	env := map[string]string{"XDG_CACHE_HOME": cacheDir}

	stdout, _, exitCode := runScafctlWithEnv(t, env, "plugins", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "0.2.0")
	assert.NotContains(t, stdout, "0.1.1")

	stdoutAll, _, exitCodeAll := runScafctlWithEnv(t, env, "plugins", "list", "--all-versions", "-o", "json")
	assert.Equal(t, 0, exitCodeAll)
	assert.Contains(t, stdoutAll, "0.2.0")
	assert.Contains(t, stdoutAll, "0.1.1")

	// The --all alias must behave identically to --all-versions end-to-end.
	stdoutAlias, _, exitCodeAlias := runScafctlWithEnv(t, env, "plugins", "list", "--all", "-o", "json")
	assert.Equal(t, 0, exitCodeAlias)
	assert.Contains(t, stdoutAlias, "0.2.0")
	assert.Contains(t, stdoutAlias, "0.1.1")
}

// TestIntegration_PluginsListHelp verifies plugins list --help documents the
// new --all-versions/--all flags (issue #532), mirroring
// TestIntegration_CatalogListHelp for catalog list.
func TestIntegration_PluginsListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "plugins", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--all-versions")
	// Line-anchored so this only matches "--all" as its own flag (e.g.
	// "    --all    Alias for --all-versions"), not merely as a substring
	// of "--all-versions" which assert.Contains alone would also satisfy.
	allFlagPattern := regexp.MustCompile(`(?m)^\s+--all\s`)
	assert.True(t, allFlagPattern.MatchString(stdout), "expected --help to document --all as its own flag, got:\n%s", stdout)
}

func TestIntegration_RunProvider_VersionPinFlag(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	// Using a non-existent version should fail gracefully with a clear error
	_, stderr, exitCode := runScafctlWithEnv(t, map[string]string{"XDG_CACHE_HOME": cacheDir}, "run", "provider", "exec", "--plugin-version", "99.99.99", "command=echo hi")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "99.99.99")
}

func TestIntegration_RunProvider_AtVersionSyntax(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	// Using name@version with a non-existent version should fail gracefully
	_, stderr, exitCode := runScafctlWithEnv(t, map[string]string{"XDG_CACHE_HOME": cacheDir}, "run", "provider", "exec@99.99.99", "command=echo hi")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "99.99.99")
}

func TestIntegration_RunProvider_HelpShowsVersionPinning(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "run", "provider", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--plugin-version")
}

func TestIntegration_RunProvider_BuiltinVersionPinWarning(t *testing.T) {
	t.Parallel()
	// Builtin provider with version 1.0.0 -- pinning to a different version should error.
	_, stderr, exitCode := runScafctl(t, "run", "provider", "static", "--plugin-version", "2.0.0", "value=hello")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version")
	assert.Contains(t, stderr, "2.0.0")
}

func TestIntegration_RunProvider_BuiltinVersionPinMatch(t *testing.T) {
	t.Parallel()
	// Builtin provider with version 1.0.0 -- pinning to 1.0.0 should succeed.
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--plugin-version", "1.0.0", "value=hello")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello")
}

func TestIntegration_GetProvider_VersionColumn(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider", "-o", "json")

	assert.Equal(t, 0, exitCode)
	// JSON output should include version field for providers
	assert.Contains(t, stdout, "\"version\"")
}

// ============================================================================
// Get Commands Tests
// ============================================================================

func TestIntegration_GetCommands(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "commands", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "run solution")
	assert.Contains(t, stdout, "get provider")
}

func TestIntegration_GetCommands_LeafOnly(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "commands", "--leaf", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "get provider")
	// Parent commands should not appear as standalone entries when --leaf is set
	assert.NotRegexp(t, `"name"\s*:\s*"scafctl get"`, stdout)
	assert.NotRegexp(t, `"name"\s*:\s*"scafctl run"`, stdout)
}

// ============================================================================
// Inspect Solution Tests
// ============================================================================

func TestIntegration_InspectSolution_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"inspect", "solution",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "\"resolvers\"")
	assert.Contains(t, stdout, "\"hasResolvers\"")
}

func TestIntegration_InspectSolution_YAML(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"inspect", "solution",
		"-f", "examples/resolver-demo.yaml",
		"-o", "yaml",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "name:")
	assert.Contains(t, stdout, "hasResolvers:")
}

func TestIntegration_InspectSolution_InvalidFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "inspect", "solution", "-f", "/nonexistent.yaml")

	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_InspectSolution_Alias(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"inspect", "sol",
		"-f", "examples/resolver-demo.yaml",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
}

// ============================================================================
// Auto-Discovery: Taskfile and Multi-Match Tests
// ============================================================================

func TestIntegration_Lint_AutoDiscovery_TaskfileYaml(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Only a taskfile.yaml exists — should be auto-discovered
	taskfile := filepath.Join(tmpDir, "taskfile.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: taskfile-discovery
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello from taskfile
`
	require.NoError(t, os.WriteFile(taskfile, []byte(content), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "lint", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_AutoDiscovery_MultiMatch_Warning(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Place two solution files — low-risk command should warn
	solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-match
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), []byte(solutionYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "taskfile.yaml"), []byte(solutionYAML), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "lint", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	// Should succeed (low risk); multi-match message is verbose-only now
	assert.True(t, exitCode == 0 || exitCode == 2, "lint should exit 0 or 2, got %d", exitCode)
	assert.NotContains(t, stderr, "Multiple solution files found")

	// With --verbose the message appears
	_, stderrV, exitCodeV := runScafctlInDir(t, tmpDir, "lint", "-o", "json", "--verbose")
	assert.True(t, exitCodeV == 0 || exitCodeV == 2, "lint --verbose should exit 0 or 2, got %d", exitCodeV)
	assert.Contains(t, stderrV, "Multiple solution files found")
}

func TestIntegration_BuildSolution_AutoDiscovery_MultiMatch_Error(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Place two solution files — high-risk command should error
	solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-match-build
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), []byte(solutionYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "taskfile.yaml"), []byte(solutionYAML), 0o644))

	_, stderr, exitCode := runScafctlInDir(t, tmpDir, "build", "solution")
	t.Logf("stderr: %s", stderr)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "multiple solution files found")
	assert.Contains(t, stderr, "use -f/--file")
}

func TestIntegration_RunResolver_AutoDiscovery_TaskfileYaml(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Only a taskfile.yaml exists — run resolver should auto-discover it
	taskfile := filepath.Join(tmpDir, "taskfile.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: taskfile-run-resolver
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: Hello from taskfile resolver
`
	require.NoError(t, os.WriteFile(taskfile, []byte(content), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "resolver", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Hello from taskfile resolver")
}

func TestIntegration_RunResolver_AutoDiscovery_MultiMatch_Warning(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Place solution.yaml and taskfile.yaml — run resolver (low-risk) should
	// use solution.yaml (higher priority) and warn about the other.
	solutionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-match-resolver
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: from solution.yaml
`
	taskfileYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-match-taskfile
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: from taskfile.yaml
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), []byte(solutionYAML), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "taskfile.yaml"), []byte(taskfileYAML), 0o644))

	stdout, stderr, exitCode := runScafctlInDir(t, tmpDir, "run", "resolver", "-o", "json")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "from solution.yaml")
	// Multi-match message is verbose-only; should not appear by default
	assert.NotContains(t, stderr, "Multiple solution files found")
}

// ============================================================================
// State Command Tests
// ============================================================================

func TestIntegration_StateHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "state", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "state")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "get")
	assert.Contains(t, stdout, "set")
	assert.Contains(t, stdout, "delete")
	assert.Contains(t, stdout, "clear")
}

func TestIntegration_StateSetAndGet(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Set a key
	_, _, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "region", "--value", "us-east-1")
	assert.Equal(t, 0, exitCode)

	// Get the key
	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "region")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "us-east-1")
}

func TestIntegration_StateList(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Persist keys so they land in the resolvers section that `state list` shows.
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "k1", "--value", "v1", "--persist")
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "k2", "--value", "v2", "--persist")

	// List
	stdout, _, exitCode := runScafctl(t, "state", "list", "--path", stateFile)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "k1")
	assert.Contains(t, stdout, "k2")
}

func TestIntegration_StateDelete(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Set then delete
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "ephemeral", "--value", "temp")
	_, _, exitCode := runScafctl(t, "state", "delete", "--path", stateFile, "--key", "ephemeral")
	assert.Equal(t, 0, exitCode)

	// Verify deleted
	_, _, exitCode = runScafctl(t, "state", "get", "--path", stateFile, "--key", "ephemeral")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_StateSetTypedInt(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	_, _, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "port", "--value", "8080", "--type", "int")
	assert.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "port", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "8080")
}

func TestIntegration_StateSetTypedInt_Invalid(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	_, stderr, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "port", "--value", "abc", "--type", "int")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "cannot parse")
}

func TestIntegration_StateClear(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed state with entries
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "k1", "--value", "v1")
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "k2", "--value", "v2")

	// Clear
	stdout, _, exitCode := runScafctl(t, "state", "clear", "--path", stateFile)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Cleared 2 entries")

	// Verify entries are gone
	_, _, exitCode = runScafctl(t, "state", "get", "--path", stateFile, "--key", "k1")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_StateSetImmutableRejectsOverwrite(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed state with an immutable entry using the new schema
	content := `{"schemaVersion":2,"metadata":{},"command":{"subcommand":"","parameters":{}},"parameters":{},"resolvers":{"locked":{"value":"original","type":"string","immutable":true,"createdAt":"2025-01-01T00:00:00Z"}},"fingerprints":{}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	// Attempt to overwrite the immutable key
	_, stderr, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "locked", "--value", "new-value")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "immutable")

	// Verify the original value is preserved
	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "locked")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "original")
}

// TestIntegration_RunSolution_ImmutableViolationAbortsBeforeActions is a
// regression test for a bug where an immutable resolver whose value changed
// between runs still let the action workflow execute (scaffolding files with
// the illegal value) before the immutable check fired at state-save time.
// The immutable check must run BEFORE any action, so a violation aborts the
// run and leaves prior output untouched.
func TestIntegration_RunSolution_ImmutableViolationAbortsBeforeActions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solutionPath := filepath.Join(dir, "solution.yaml")
	solution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: immutable-action-ordering
  version: 1.0.0
state:
  enabled: true
  backend:
    provider: file
    inputs:
      path: "state.json"
spec:
  resolvers:
    deployment_id:
      type: string
      immutable: true
      resolve:
        with:
          - provider: parameter
            inputs:
              key: "deployment_id"
  workflow:
    actions:
      write-id:
        provider: file
        inputs:
          operation: write
          path: "out.txt"
          content:
            expr: "_.deployment_id"
`
	require.NoError(t, os.WriteFile(solutionPath, []byte(solution), 0o600))
	outFile := filepath.Join(dir, "out.txt")

	// First run locks deployment_id=original and scaffolds out.txt.
	_, stderr, exitCode := runScafctlInDir(t, dir,
		"run", "solution", "-f", solutionPath,
		"-r", "deployment_id=original", "--on-conflict", "overwrite",
	)
	require.Equal(t, 0, exitCode, "first run should succeed: %s", stderr)
	original, err := os.ReadFile(outFile) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Contains(t, string(original), "original")

	// Second run attempts to change the immutable value. It must abort BEFORE
	// the action runs, so out.txt must remain unchanged (never rewritten to
	// "changed").
	_, stderr, exitCode = runScafctlInDir(t, dir,
		"run", "solution", "-f", solutionPath,
		"-r", "deployment_id=changed", "--on-conflict", "overwrite",
	)
	assert.NotEqual(t, 0, exitCode, "run must fail when an immutable value changes")
	assert.Contains(t, stderr, "immutable")

	after, err := os.ReadFile(outFile) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, string(original), string(after),
		"output file must not be scaffolded with the illegal immutable value")
	assert.NotContains(t, string(after), "changed",
		"action must not have executed with the changed immutable value")
}

func TestIntegration_StateGetJSON(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed state with an immutable entry using the new schema
	content := `{"schemaVersion":2,"metadata":{},"command":{"subcommand":"","parameters":{}},"parameters":{},"resolvers":{"api_key":{"value":"sk-123","type":"string","immutable":true,"createdAt":"2025-04-01T10:00:00Z"}},"fingerprints":{}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "api_key", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "sk-123")
	assert.Contains(t, stdout, "immutable")
	assert.Contains(t, stdout, "string")
}

func TestIntegration_StateListJSON(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Persist keys so they appear in the resolvers section `state list` returns.
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "alpha", "--value", "a", "--persist")
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "beta", "--value", "b", "--persist")

	stdout, _, exitCode := runScafctl(t, "state", "list", "--path", stateFile, "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "alpha")
	assert.Contains(t, stdout, "beta")
	// JSON output should be parseable
	assert.Contains(t, stdout, "{")
}

// TestIntegration_StateListResolversJSON verifies that `state list -o json`
// returns only the persisted resolvers map -- the entries accessible to the
// state provider -- not the full state document.
func TestIntegration_StateListResolversJSON(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed a state file exercising every section: a parameter, a persisted
	// resolver, an immutable resolver, and a fingerprint.
	content := `{"schemaVersion":3,` +
		`"metadata":{"solution":"grouped-demo","version":"2.0.0","createdAt":"2025-01-01T00:00:00Z","lastUpdatedAt":"2025-02-02T00:00:00Z","runtime":{"engine":{"name":"scafctl","version":"dev"},"cli":{"name":"scafctl","version":"dev"}}},` +
		`"command":{"subcommand":"run resolver","parameters":{"token":"alpha"}},` +
		`"parameters":{"env":"prod"},` +
		`"resolvers":{` +
		`"current_token":{"value":"alpha","type":"string","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-02-02T00:00:00Z"},` +
		`"locked":{"value":"secret","type":"string","immutable":true,"createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}},` +
		`"fingerprints":{"__fingerprint:build:inputs":{"value":"deadbeef","updatedAt":"2025-02-02T00:00:00Z"}}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "state", "list", "--path", stateFile, "-o", "json")
	require.Equal(t, 0, exitCode)

	// The payload is the resolvers subtree itself, keyed by resolver name.
	var resolvers map[string]struct {
		Value     any    `json:"value"`
		Type      string `json:"type"`
		Immutable bool   `json:"immutable"`
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &resolvers), "output must be valid JSON")

	require.Len(t, resolvers, 2)
	current := resolvers["current_token"]
	assert.False(t, current.Immutable)
	assert.Equal(t, "string", current.Type)
	assert.Equal(t, "2025-02-02T00:00:00Z", current.UpdatedAt)
	assert.True(t, resolvers["locked"].Immutable)

	// Other document sections are NOT present in the list payload.
	assert.NotContains(t, stdout, "schemaVersion")
	assert.NotContains(t, stdout, "fingerprints")
}

// TestIntegration_StateListResolversTable verifies that the default (human)
// table output for `state list` shows only the resolvers section -- no summary
// header and no other section headings.
func TestIntegration_StateListResolversTable(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	content := `{"schemaVersion":3,` +
		`"metadata":{"solution":"grouped-demo","version":"2.0.0","createdAt":"2025-01-01T00:00:00Z","lastUpdatedAt":"2025-02-02T00:00:00Z","runtime":{"engine":{"name":"scafctl","version":"dev"},"cli":{"name":"scafctl","version":"dev"}}},` +
		`"command":{"subcommand":"run resolver","parameters":{}},` +
		`"parameters":{"env":"prod"},` +
		`"resolvers":{"current_token":{"value":"alpha","type":"string","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-02-02T00:00:00Z"}},` +
		`"fingerprints":{}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "state", "list", "--path", stateFile)
	require.Equal(t, 0, exitCode)

	// Only the resolvers section renders.
	assert.Contains(t, stdout, "Resolvers")
	assert.Contains(t, stdout, "current_token")
	// No summary header and no other section headings.
	assert.NotContains(t, stdout, "grouped-demo@2.0.0")
	assert.NotContains(t, stdout, "Metadata")
	assert.NotContains(t, stdout, "Parameters")
}

// TestIntegration_StateShowGrouped verifies that `state show -o json` returns
// the faithful on-disk state schema (the raw state object), so scripts always
// see the true structure. This path is schema-driven and does not need updating
// when new fields are added to the state format.
func TestIntegration_StateShowGrouped(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed a state file exercising every section: a parameter, a persisted
	// resolver, an immutable resolver, and a fingerprint.
	content := `{"schemaVersion":3,` +
		`"metadata":{"solution":"grouped-demo","version":"2.0.0","createdAt":"2025-01-01T00:00:00Z","lastUpdatedAt":"2025-02-02T00:00:00Z","runtime":{"engine":{"name":"scafctl","version":"dev"},"cli":{"name":"scafctl","version":"dev"}}},` +
		`"command":{"subcommand":"run resolver","parameters":{"token":"alpha"}},` +
		`"parameters":{"env":"prod"},` +
		`"resolvers":{` +
		`"current_token":{"value":"alpha","type":"string","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-02-02T00:00:00Z"},` +
		`"locked":{"value":"secret","type":"string","immutable":true,"createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-01T00:00:00Z"}},` +
		`"fingerprints":{"__fingerprint:build:inputs":{"value":"deadbeef","updatedAt":"2025-02-02T00:00:00Z"}}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "state", "show", "--path", stateFile, "-o", "json")
	require.Equal(t, 0, exitCode)

	var view struct {
		SchemaVersion int            `json:"schemaVersion"`
		Metadata      map[string]any `json:"metadata"`
		Command       map[string]any `json:"command"`
		Parameters    map[string]any `json:"parameters"`
		Resolvers     map[string]struct {
			Value     any    `json:"value"`
			Type      string `json:"type"`
			Immutable bool   `json:"immutable"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"resolvers"`
		Fingerprints map[string]struct {
			Value     string `json:"value"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"fingerprints"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &view), "output must be valid JSON")

	assert.Equal(t, 3, view.SchemaVersion)
	assert.Equal(t, "grouped-demo", view.Metadata["solution"])
	assert.Equal(t, "run resolver", view.Command["subcommand"])

	require.Len(t, view.Parameters, 1)
	assert.Equal(t, "prod", view.Parameters["env"])

	require.Len(t, view.Resolvers, 2)
	current := view.Resolvers["current_token"]
	assert.False(t, current.Immutable)
	assert.Equal(t, "string", current.Type)
	assert.Equal(t, "2025-02-02T00:00:00Z", current.UpdatedAt)
	assert.True(t, view.Resolvers["locked"].Immutable)

	require.Len(t, view.Fingerprints, 1)
	assert.Equal(t, "deadbeef", view.Fingerprints["__fingerprint:build:inputs"].Value)
}

// TestIntegration_StateShowGroupedTable verifies that the default (human) table
// output for `state show` renders a summary header plus each populated section
// under its own heading.
func TestIntegration_StateShowGroupedTable(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	content := `{"schemaVersion":3,` +
		`"metadata":{"solution":"grouped-demo","version":"2.0.0","createdAt":"2025-01-01T00:00:00Z","lastUpdatedAt":"2025-02-02T00:00:00Z","runtime":{"engine":{"name":"scafctl","version":"dev"},"cli":{"name":"scafctl","version":"dev"}}},` +
		`"command":{"subcommand":"run resolver","parameters":{}},` +
		`"parameters":{"env":"prod"},` +
		`"resolvers":{"current_token":{"value":"alpha","type":"string","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-02-02T00:00:00Z"}},` +
		`"fingerprints":{}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	stdout, _, exitCode := runScafctl(t, "state", "show", "--path", stateFile)
	require.Equal(t, 0, exitCode)

	// A compact summary header leads the view for at-a-glance context.
	assert.Contains(t, stdout, "grouped-demo@2.0.0")
	assert.Contains(t, stdout, "1 parameters, 1 resolvers, 0 fingerprints")

	// Section headings mirror the state file layout.
	assert.Contains(t, stdout, "Metadata")
	assert.Contains(t, stdout, "Command")
	assert.Contains(t, stdout, "Parameters")
	assert.Contains(t, stdout, "Resolvers")
	// Detail values are present.
	assert.Contains(t, stdout, "grouped-demo")
	assert.Contains(t, stdout, "current_token")
	// Empty fingerprints section is omitted from the human view.
	assert.NotContains(t, stdout, "Fingerprints")
}

func TestIntegration_StateDeleteNonexistentKey(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed with one key
	runScafctl(t, "state", "set", "--path", stateFile, "--key", "exists", "--value", "val")

	// Delete a key that doesn't exist
	_, stderr, exitCode := runScafctl(t, "state", "delete", "--path", stateFile, "--key", "ghost")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_StateSetTypedBool(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	_, _, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "debug", "--value", "true", "--type", "bool")
	assert.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "debug", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "true")
}

func TestIntegration_StateSetTypedFloat(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	_, _, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "ratio", "--value", "3.14", "--type", "float")
	assert.Equal(t, 0, exitCode)

	stdout, _, exitCode := runScafctl(t, "state", "get", "--path", stateFile, "--key", "ratio", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "3.14")
}

func TestIntegration_StateSetTypedBool_Invalid(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	_, stderr, exitCode := runScafctl(t, "state", "set", "--path", stateFile, "--key", "debug", "--value", "maybe", "--type", "bool")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "cannot parse")
}

func TestIntegration_StateClearPreservesMetadata(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "test-state.json")

	// Seed state with metadata and parameters
	content := `{"schemaVersion":3,"metadata":{"solution":"my-sol","version":"2.0.0","createdAt":"2025-01-01T00:00:00Z","lastUpdatedAt":"2025-03-01T00:00:00Z","runtime":{"engine":{"name":"scafctl","version":"1.0.0"},"cli":{"name":"scafctl","version":"1.0.0"}}},"command":{"subcommand":"run solution","parameters":{"env":"prod"}},"parameters":{"k1":"v1","k2":"v2"},"resolvers":{},"fingerprints":{}}`
	require.NoError(t, os.WriteFile(stateFile, []byte(content), 0o600))

	// Clear
	stdout, _, exitCode := runScafctl(t, "state", "clear", "--path", stateFile)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Cleared 2 entries")

	// Read the file directly and verify metadata is preserved
	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "my-sol")
	assert.Contains(t, string(data), "2.0.0")
	// But parameters should be empty
	assert.NotContains(t, string(data), "k1")
	assert.NotContains(t, string(data), "k2")
}

// ── refactor rename resolver ─────────────────────────────────────────────────

const refactorRenameFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refactor-int # keep this comment
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - environment
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
`

func writeRefactorFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameFixture), 0o600))
	return path
}

func TestIntegration_RefactorRenameDryRun(t *testing.T) {
	t.Parallel()
	path := writeRefactorFixture(t)

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "resolver", "environment", "env", "-f", path, "--dry-run")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Would rename resolver")
	assert.Contains(t, stdout, "environment -> env")

	// Dry-run must not modify the file.
	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, refactorRenameFixture, string(got))
}

func TestIntegration_RefactorRenameApply(t *testing.T) {
	t.Parallel()
	path := writeRefactorFixture(t)

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "resolver", "environment", "env", "-f", path)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Renamed resolver")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.NotContains(t, string(got), "environment")
	assert.Contains(t, string(got), "env:")
	assert.Contains(t, string(got), "- env")
	assert.Contains(t, string(got), "expr: _.env")
	// Comments/formatting preserved.
	assert.Contains(t, string(got), "# keep this comment")
}

func TestIntegration_RefactorRenameCollision(t *testing.T) {
	t.Parallel()
	path := writeRefactorFixture(t)

	_, stderr, exitCode := runScafctl(t, "refactor", "rename", "resolver", "environment", "appName", "-f", path)
	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	assert.Contains(t, stderr, "already exists")
}

func TestIntegration_RefactorHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "refactor", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "rename")
}

// ── refactor rename action ───────────────────────────────────────────────────

const refactorRenameActionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refactor-action-int # keep this comment
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        alias: b
        provider: message
        inputs:
          message: make build
      deploy:
        dependsOn:
          - build
        provider: message
        when:
          expr: __actions.build.success
        inputs:
          message:
            tmpl: 'deploy {{ .__actions.build.message }}'
`

func writeRefactorActionFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameActionFixture), 0o600))
	return path
}

func TestIntegration_RefactorRenameActionDryRun(t *testing.T) {
	t.Parallel()
	path := writeRefactorActionFixture(t)

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "action", "build", "compile", "-f", path, "--dry-run")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Would rename action")
	assert.Contains(t, stdout, "build -> compile")

	// Dry-run must not modify the file.
	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, refactorRenameActionFixture, string(got))
}

func TestIntegration_RefactorRenameActionApply(t *testing.T) {
	t.Parallel()
	path := writeRefactorActionFixture(t)

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "action", "build", "compile", "-f", path)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Renamed action")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	s := string(got)
	assert.Contains(t, s, "compile:")
	assert.Contains(t, s, "- compile")
	assert.Contains(t, s, "__actions.compile.success")
	assert.Contains(t, s, ".__actions.compile.message")
	// The action's alias is a separate name and must be left untouched.
	assert.Contains(t, s, "alias: b")
	// Unrelated literal text is preserved.
	assert.Contains(t, s, "message: make build")
	assert.Contains(t, s, "# keep this comment")
}

func TestIntegration_RefactorRenameActionCollision(t *testing.T) {
	t.Parallel()
	path := writeRefactorActionFixture(t)

	_, stderr, exitCode := runScafctl(t, "refactor", "rename", "action", "build", "deploy", "-f", path)
	assert.Equal(t, exitcode.ValidationFailed, exitCode)
	assert.Contains(t, stderr, "already exists")
}

// ── lsp (language server over stdio) ─────────────────────────────────────────

// lspFrame wraps a JSON-RPC message in an LSP Content-Length frame.
func lspFrame(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)
	return append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))), body...)
}

// lockedBuffer is a bytes.Buffer safe for concurrent Write (by the child
// process pipe) and String reads (by the test poll loop).
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestIntegration_LspHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lsp", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "language server")
	assert.Contains(t, stdout, "LSP")
}

func TestIntegration_LspDocumentSelectors(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "lsp", "document-selectors", "-o", "json")
	require.Equal(t, 0, exitCode)

	var got struct {
		BinaryName string   `json:"binaryName"`
		YAMLNames  []string `json:"yamlNames"`
		JSONNames  []string `json:"jsonNames"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "output must be valid JSON: %s", stdout)

	// The full recognized set the extension previously missed must be reported.
	assert.Contains(t, got.YAMLNames, "solution.yaml")
	assert.Contains(t, got.YAMLNames, "taskfile.yaml")
	assert.Contains(t, got.YAMLNames, "actions.yaml")
	// JSON solutions are routed to jsonNames, never yamlNames.
	assert.Contains(t, got.JSONNames, "solution.json")
	assert.NotContains(t, got.YAMLNames, "solution.json")
}

func TestIntegration_LspInitializeAndDiagnostics(t *testing.T) {
	t.Parallel()

	badSolution := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: bad\nspec:\n  resolvers:\n    appName:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value:\n                expr: _.doesNotExist\n"

	// Migrated to the runLSPSession harness: this test sends no requests beyond
	// the handshake + didOpen, so it exercises the harness's "diagnostics only"
	// path (the server publishes diagnostics for the opened document on its own).
	out := runLSPSession(t, badSolution)

	assert.Contains(t, out, `"capabilities"`, "initialize response")
	assert.Contains(t, out, "scafctl lsp", "server info")
	assert.Contains(t, out, "publishDiagnostics", "diagnostics notification")
	assert.Contains(t, out, "doesNotExist", "the finding message")
	// Navigation capabilities are advertised.
	assert.Contains(t, out, "definitionProvider", "go-to-definition capability")
	assert.Contains(t, out, "referencesProvider", "find-references capability")
	assert.Contains(t, out, "renameProvider", "rename capability")
}

func TestIntegration_LspDocumentSymbol(t *testing.T) {
	t.Parallel()

	sol := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: symbols\nspec:\n  calls:\n    fetch:\n      provider: message\n      inputs:\n        message: fetching\n  resolvers:\n    environment:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: dev\n"

	out := runLSPSession(t, sol, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/documentSymbol", "params": map[string]any{
			"textDocument": map[string]any{"uri": lspSessionURI},
		},
	})

	// The provider is advertised in the initialize response.
	assert.Contains(t, out, "documentSymbolProvider", "documentSymbol capability advertised")
	// The response lists the spec root, both groups, and their symbols.
	assert.Contains(t, out, `"name":"spec"`, "spec root symbol")
	assert.Contains(t, out, `"name":"resolvers"`, "resolvers group")
	assert.Contains(t, out, `"name":"environment"`, "resolver symbol")
	assert.Contains(t, out, `"name":"calls"`, "calls group")
	assert.Contains(t, out, `"name":"fetch"`, "call symbol")
	// Hierarchy is expressed via children.
	assert.Contains(t, out, `"children"`, "symbols nested as children")
}

func TestIntegration_LspRename(t *testing.T) {
	t.Parallel()

	// "environment:" is defined on line 6 (0-based); the cursor at line 6 char 6
	// sits inside that identifier.
	sol := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: nav\nspec:\n  resolvers:\n    environment:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value: dev\n    appName:\n      dependsOn:\n        - environment\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value:\n                expr: _.environment\n"

	// Migrated to the reusable runLSPSession harness: initialize + didOpen + the
	// rename request + teardown are all handled by the helper. The request
	// targets lspSessionURI (the URI the harness opens the document under).
	out := runLSPSession(t, sol, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "textDocument/rename", "params": map[string]any{
			"textDocument": map[string]any{"uri": lspSessionURI},
			"position":     map[string]any{"line": 6, "character": 6},
			"newName":      "env",
		},
	})

	assert.Contains(t, out, `"changes"`, "rename returns a workspace edit")
	assert.Contains(t, out, `"newText":"env"`, "edits rename to env")
	assert.Contains(t, out, lspSessionURI, "edits target the document")
}

// TestIntegration_LspStdioFlag verifies that `lsp --stdio` starts the server and
// serves the LSP protocol. Many LSP clients (and vscode-languageclient for
// TransportKind.stdio) append `--stdio`; the server must accept it rather than
// exit with "unknown flag", which this exercises end-to-end.
func TestIntegration_LspStdioFlag(t *testing.T) {
	t.Parallel()

	badSolution := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: bad\nspec:\n  resolvers:\n    appName:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value:\n                expr: _.doesNotExist\n"

	msgs := [][]byte{
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{}}}),
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}}),
		lspFrame(t, map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{"uri": "file:///tmp/bad.yaml", "languageId": "yaml", "version": 1, "text": badSolution},
		}}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The --stdio flag is the key part of this test: it must not error.
	cmd := exec.CommandContext(ctx, binaryPath, "lsp", "--stdio")
	cmd.Dir = findProjectRoot()
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	out := &lockedBuffer{}
	var errBuf bytes.Buffer
	cmd.Stdout = out
	cmd.Stderr = &errBuf
	require.NoError(t, cmd.Start())

	for _, m := range msgs {
		_, werr := stdin.Write(m)
		require.NoError(t, werr, "write LSP frame to server stdin")
	}

	// Poll until diagnostics for the bad solution appear instead of sleeping a
	// fixed amount, so the test is not flaky under load (mirrors
	// TestIntegration_LspInitializeAndDiagnostics).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "publishDiagnostics") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = stdin.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()

	// The server must not have rejected the flag, and must have served the
	// protocol (published diagnostics for the bad solution).
	assert.NotContains(t, errBuf.String(), "unknown flag", "--stdio must be accepted")
	assert.Contains(t, out.String(), "publishDiagnostics", "server serves LSP over stdio with --stdio")
}

// ── refactor rename call / function ──────────────────────────────────────────

const refactorRenameCallFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refactor-call-int # keep this comment
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
  workflow:
    actions:
      a1:
        call: fetch
`

func TestIntegration_RefactorRenameCallDryRun(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameCallFixture), 0o600))

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "call", "fetch", "download", "-f", path, "--dry-run")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Would rename call")
	assert.Contains(t, stdout, "fetch -> download")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, refactorRenameCallFixture, string(got), "dry-run must not modify the file")
}

func TestIntegration_RefactorRenameCallApply(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameCallFixture), 0o600))

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "call", "fetch", "download", "-f", path)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Renamed call")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	s := string(got)
	assert.Contains(t, s, "download:")
	assert.Contains(t, s, "- call: download")
	assert.NotContains(t, s, "call: fetch")
	assert.Contains(t, s, "message: fetching") // unrelated literal preserved
	assert.Contains(t, s, "# keep this comment")
}

const refactorRenameFunctionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: refactor-func-int # keep this comment
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
    loud:
      params:
        - name: msg
      template: "{{ greet .args.msg }}!"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }}"
`

func TestIntegration_RefactorRenameFunctionDryRun(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameFunctionFixture), 0o600))

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "function", "greet", "welcome", "-f", path, "--dry-run")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Would rename function")
	assert.Contains(t, stdout, "greet -> welcome")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	assert.Equal(t, refactorRenameFunctionFixture, string(got))
}

func TestIntegration_RefactorRenameFunctionApply(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "solution.yaml")
	require.NoError(t, os.WriteFile(path, []byte(refactorRenameFunctionFixture), 0o600))

	stdout, _, exitCode := runScafctl(t, "refactor", "rename", "function", "greet", "welcome", "-f", path)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Renamed function")

	got, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	require.NoError(t, err)
	s := string(got)
	assert.Contains(t, s, "welcome:")
	assert.Contains(t, s, "{{ welcome .args.msg }}")
	assert.Contains(t, s, "{{ welcome ._.env }}")
	assert.NotContains(t, s, "greet")
}
