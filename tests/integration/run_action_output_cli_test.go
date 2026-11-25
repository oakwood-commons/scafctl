// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	mockExecPluginOnce sync.Once
	mockExecPluginPath string
	mockExecPluginErr  error
)

const mockExecPluginSource = `package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	sdkplugin "github.com/oakwood-commons/scafctl-plugin-sdk/plugin"
	sdkprovider "github.com/oakwood-commons/scafctl-plugin-sdk/provider"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"
)

type mockExecPlugin struct{}

func (p *mockExecPlugin) GetProviders(_ context.Context) ([]string, error) {
	return []string{"mockexec"}, nil
}

func (p *mockExecPlugin) GetProviderDescriptor(_ context.Context, providerName string) (*sdkprovider.Descriptor, error) {
	if providerName != "mockexec" {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
	return &sdkprovider.Descriptor{
		Name:        "mockexec",
		DisplayName: "Mock Exec Provider",
		Description: "Test provider simulating buffered and streaming command execution",
		APIVersion:  "v1",
		Version:     semver.MustParse("0.1.0"),
		Category:    "system",
		Capabilities: []sdkprovider.Capability{
			sdkprovider.CapabilityAction,
		},
		Schema: sdkhelper.ObjectSchema(
			nil,
			map[string]*jsonschema.Schema{
				"stdout":   sdkhelper.StringProp("Simulated stdout"),
				"stderr":   sdkhelper.StringProp("Simulated stderr"),
				"exitCode": sdkhelper.IntProp("Simulated exit code"),
				"error":    sdkhelper.StringProp("Simulated error message"),
				"stream":   sdkhelper.BoolProp("Enable streaming execution", sdkhelper.WithDefault(json.RawMessage("false"))),
				"mode":     sdkhelper.StringProp("Execution mode: buffered-stream, stream, or unary-only"),
			},
		),
		OutputSchemas: map[sdkprovider.Capability]*jsonschema.Schema{
			sdkprovider.CapabilityAction: sdkhelper.ObjectSchema(
				nil,
				map[string]*jsonschema.Schema{
					"stdout":   sdkhelper.StringProp("Captured stdout"),
					"stderr":   sdkhelper.StringProp("Captured stderr"),
					"exitCode": sdkhelper.IntProp("Process exit code"),
					"success":  sdkhelper.BoolProp("Whether command succeeded"),
				},
			),
		},
	}, nil
}

func parseExitCode(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func buildMockOutput(input map[string]any, streamed bool) (*sdkprovider.Output, error) {
	stdoutStr, _ := input["stdout"].(string)
	stderrStr, _ := input["stderr"].(string)
	code := parseExitCode(input["exitCode"])
	errMsg, _ := input["error"].(string)

	out := &sdkprovider.Output{
		Streamed: streamed,
		Data: map[string]any{
			"stdout":   stdoutStr,
			"stderr":   stderrStr,
			"exitCode": code,
			"success":  code == 0 && errMsg == "",
		},
	}
	if code != 0 || errMsg != "" {
		if errMsg == "" {
			errMsg = fmt.Sprintf("command exited with code %d", code)
		}
		return out, errors.New(errMsg)
	}
	return out, nil
}

func (p *mockExecPlugin) ExecuteProvider(_ context.Context, providerName string, input map[string]any) (*sdkprovider.Output, error) {
	if providerName != "mockexec" {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}
	return buildMockOutput(input, false)
}

func (p *mockExecPlugin) ExecuteProviderStream(_ context.Context, providerName string, input map[string]any, cb func(sdkplugin.StreamChunk)) error {
	if providerName != "mockexec" {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	mode, _ := input["mode"].(string)
	if mode == "unary-only" {
		return sdkplugin.ErrStreamingNotSupported
	}
	if mode == "stderr-progress" {
		stderrStr, _ := input["stderr"].(string)
		if stderrStr != "" {
			cb(sdkplugin.StreamChunk{Stderr: []byte(stderrStr)})
		}
		stdoutStr, _ := input["stdout"].(string)
		code := parseExitCode(input["exitCode"])
		out := &sdkprovider.Output{
			Streamed: false,
			Data: map[string]any{
				"stdout":   stdoutStr,
				"exitCode": code,
				"success":  code == 0,
			},
		}
		cb(sdkplugin.StreamChunk{Result: out})
		if code != 0 {
			cb(sdkplugin.StreamChunk{Error: fmt.Sprintf("command exited with code %d", code)})
		}
		return nil
	}
	if mode == "chunk-only" {
		stdoutStr, _ := input["stdout"].(string)
		stderrStr, _ := input["stderr"].(string)
		if stdoutStr != "" {
			cb(sdkplugin.StreamChunk{Stdout: []byte(stdoutStr)})
		}
		if stderrStr != "" {
			cb(sdkplugin.StreamChunk{Stderr: []byte(stderrStr)})
		}
		code := parseExitCode(input["exitCode"])
		cb(sdkplugin.StreamChunk{Result: &sdkprovider.Output{
			Streamed: true,
			Data:     map[string]any{"exitCode": code, "success": code == 0},
		}})
		if code != 0 {
			cb(sdkplugin.StreamChunk{Error: fmt.Sprintf("command exited with code %d", code)})
		}
		return nil
	}
	if mode == "strict-schema-progress" {
		stderrStr, _ := input["stderr"].(string)
		if stderrStr != "" {
			cb(sdkplugin.StreamChunk{Stderr: []byte(stderrStr)})
		}
		cb(sdkplugin.StreamChunk{Result: &sdkprovider.Output{
			Streamed: true,
			Data:     map[string]any{"id": 1},
		}})
		return nil
	}
	stream, _ := input["stream"].(bool)
	if stream {
		stdoutStr, _ := input["stdout"].(string)
		stderrStr, _ := input["stderr"].(string)
		if stdoutStr != "" {
			cb(sdkplugin.StreamChunk{Stdout: []byte(stdoutStr)})
		}
		if stderrStr != "" {
			cb(sdkplugin.StreamChunk{Stderr: []byte(stderrStr)})
		}
	}
	out, execErr := buildMockOutput(input, stream)
	cb(sdkplugin.StreamChunk{Result: out})
	if execErr != nil {
		cb(sdkplugin.StreamChunk{Error: execErr.Error()})
	}
	return nil
}

func (p *mockExecPlugin) DescribeWhatIf(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "Would run mock command", nil
}

func (p *mockExecPlugin) ConfigureProvider(_ context.Context, _ string, _ sdkplugin.ProviderConfig) error {
	return nil
}

func (p *mockExecPlugin) ExtractDependencies(_ context.Context, _ string, _ map[string]any) ([]string, error) {
	return nil, nil
}

func (p *mockExecPlugin) StopProvider(_ context.Context, _ string) error {
	return nil
}

func main() {
	sdkplugin.Serve(&mockExecPlugin{})
}
`

func buildMockExecPlugin(t *testing.T) string {
	t.Helper()
	mockExecPluginOnce.Do(func() {
		projectRoot := findProjectRoot()
		tempRoot := filepath.Join(projectRoot, "temp")
		if err := os.MkdirAll(tempRoot, 0o700); err != nil {
			mockExecPluginErr = fmt.Errorf("failed to create temp dir: %w", err)
			return
		}
		srcDir, err := os.MkdirTemp(tempRoot, "mockexec-plugin-src-*")
		if err != nil {
			mockExecPluginErr = fmt.Errorf("failed to create plugin source dir: %w", err)
			return
		}
		defer func() { _ = os.RemoveAll(srcDir) }()

		srcFile := filepath.Join(srcDir, "main.go")
		if err := os.WriteFile(srcFile, []byte(mockExecPluginSource), 0o600); err != nil {
			mockExecPluginErr = fmt.Errorf("failed to write mockexec plugin source: %w", err)
			return
		}

		distDir := filepath.Join(projectRoot, "dist")
		if err := os.MkdirAll(distDir, 0o700); err != nil {
			mockExecPluginErr = fmt.Errorf("failed to create dist dir: %w", err)
			return
		}
		binPath := filepath.Join(distDir, "scafctl-plugin-mockexec")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, srcFile)
		cmd.Dir = projectRoot
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

		out, err := cmd.CombinedOutput()
		if err != nil {
			mockExecPluginErr = fmt.Errorf("failed to build mockexec plugin: %w\n%s", err, string(out))
			return
		}
		mockExecPluginPath = binPath
	})
	require.NoError(t, mockExecPluginErr)
	require.NotEmpty(t, mockExecPluginPath, "mockexec plugin build failed")
	return mockExecPluginPath
}

func runBinaryIsolatedEnv(
	t *testing.T,
	exe, dir string,
	unset []string,
	env map[string]string,
	args ...string,
) (stdout, stderr string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir

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

func runEmbedderMyCLI(
	t *testing.T,
	dir string,
	unset []string,
	env map[string]string,
	args ...string,
) (stdout, stderr string, exitCode int) {
	t.Helper()
	testExe, err := os.Executable()
	require.NoError(t, err)

	merged := make(map[string]string, len(env)+1)
	for k, v := range env {
		merged[k] = v
	}
	merged["SCAFCTL_TEST_EMBEDDER_HELPER"] = "1"

	fullArgs := append([]string{"--"}, args...)
	return runBinaryIsolatedEnv(t, testExe, dir, unset, merged, fullArgs...)
}

func setupIsolatedCatalogWithMockExec(t *testing.T, appName, extraConfigYAML string) map[string]string {
	t.Helper()
	pluginBin := buildMockExecPlugin(t)
	xdgRoot := t.TempDir()
	cfgHome := filepath.Join(xdgRoot, "config")
	dataHome := filepath.Join(xdgRoot, "data")
	cacheHome := filepath.Join(xdgRoot, "cache")

	appCfgDir := filepath.Join(cfgHome, appName)
	require.NoError(t, os.MkdirAll(appCfgDir, 0o700))

	cfgYAML := "catalog:\n  disableOfficialCatalog: true\n"
	if extraConfigYAML != "" {
		cfgYAML += extraConfigYAML
	}
	require.NoError(t, os.WriteFile(filepath.Join(appCfgDir, "config.yaml"), []byte(cfgYAML), 0o600))

	env := map[string]string{
		"XDG_CONFIG_HOME": cfgHome,
		"XDG_DATA_HOME":   dataHome,
		"XDG_CACHE_HOME":  cacheHome,
	}
	unset := []string{
		"GITHUB_ACTIONS",
		"NO_COLOR",
		"SCAFCTL_ACTION_CIOUTPUT",
		"MYCLI_ACTION_CIOUTPUT",
	}

	platform := runtime.GOOS + "/" + runtime.GOARCH
	buildArgs := []string{
		"build", "plugin", "--force",
		"--name", "mockexec",
		"--kind", "provider",
		"--version", "0.1.0",
		"--platform", platform + "=" + pluginBin,
	}

	var stdout, stderr string
	var code int
	if appName == "mycli" {
		stdout, stderr, code = runEmbedderMyCLI(t, findProjectRoot(), unset, env, buildArgs...)
	} else {
		stdout, stderr, code = runBinaryIsolatedEnv(t, binaryPath, findProjectRoot(), unset, env, buildArgs...)
	}
	require.Equal(t, 0, code, "build plugin failed: stdout=%s stderr=%s", stdout, stderr)
	return env
}

func assertNoANSIOrStatusEmoji(t *testing.T, text string) {
	t.Helper()
	assert.NotContains(t, text, "\x1b[", "output must not contain ANSI escape sequences")
	forbiddenGlyphs := []string{
		"\u274c",     // cross mark emoji
		"\u2705",     // check mark emoji
		"\u26a0",     // warning sign
		"\U0001f4a1", // light bulb
		"\U0001f41b", // bug
		"\u2713",     // check mark
		"\u2717",     // ballot x
		"\u25cb",     // white circle
	}
	for _, g := range forbiddenGlyphs {
		assert.NotContains(t, text, g, "output must not contain status glyph %q under NO_COLOR", g)
	}
}

func TestIntegration_RunAction_FailingBufferedOutputPreserved(t *testing.T) {
	t.Parallel()

	const bufferedSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: buffered-exec-failure
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      test:
        description: Run unit tests
        provider: mockexec
        inputs:
          stdout: "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n"
          stderr: "FAIL\texample/widget\t0.012s\n"
          exitCode: 1
`

	t.Run("default human output preserves buffered stdout before failure and deduplicates error", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(bufferedSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"NO_COLOR":        "1",
		}
		unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", stdout)
		assert.Contains(t, stderr, "FAIL\texample/widget\t0.012s\n")
		assert.Contains(t, stderr, "mockexec: command exited with code 1")
		assert.NotContains(t, stderr, "mockexec: mockexec:")
		assert.NotContains(t, stderr, "provider execution failed: provider execution failed:")
		assert.Equal(t, 1, strings.Count(stderr, "command exited with code 1"),
			"failure reason must appear once without duplicate CLI exit error; stderr:\n%s", stderr)
	})

	t.Run("streaming failure does not double print stdout", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		streamYAML := strings.Replace(bufferedSolutionYAML, "exitCode: 1", "exitCode: 1\n          stream: true", 1)
		require.NoError(t, os.WriteFile(solFile, []byte(streamYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"NO_COLOR":        "1",
		}
		unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Equal(t, 1, strings.Count(stdout, "--- FAIL: TestWidget"),
			"streamed stdout must appear once, got stdout:\n%s", stdout)
	})

	t.Run("buffered stream result failure flushes stdout before failure banner", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		bufStreamYAML := strings.Replace(bufferedSolutionYAML, "exitCode: 1", "exitCode: 1\n          mode: buffered-stream", 1)
		require.NoError(t, os.WriteFile(solFile, []byte(bufStreamYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"NO_COLOR":        "1",
		}
		unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", stdout)
		assert.Contains(t, stderr, "FAIL\texample/widget\t0.012s\n")
		assert.Equal(t, 1, strings.Count(stderr, "command exited with code 1"), "stderr:\n%s", stderr)
	})

	t.Run("json output preserves failed action stdout in structured envelope", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(bufferedSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
		}
		unset := []string{"GITHUB_ACTIONS", "NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile, "-o", "json")

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)

		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout must be valid JSON:\n%s", stdout)
		assert.Equal(t, "failed", envelope["status"])

		actionsMap, ok := envelope["actions"].(map[string]any)
		require.True(t, ok, "actions map missing in JSON output: %v", envelope)
		testEntry, ok := actionsMap["test"].(map[string]any)
		require.True(t, ok, "test action missing in JSON output: %v", actionsMap)
		results, ok := testEntry["results"].(map[string]any)
		require.True(t, ok, "failed action results must be preserved in JSON output: %v", testEntry)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", results["stdout"])
		assert.Equal(t, float64(1), results["exitCode"])
	})

	t.Run("json output captures chunk-only streamed logs when provider Result omits stdout", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		chunkOnlyYAML := strings.Replace(bufferedSolutionYAML, "exitCode: 1", "exitCode: 1\n          mode: chunk-only", 1)
		require.NoError(t, os.WriteFile(solFile, []byte(chunkOnlyYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
		}
		unset := []string{"GITHUB_ACTIONS", "NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile, "-o", "json")

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)

		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout must be valid JSON:\n%s", stdout)
		actionsMap := envelope["actions"].(map[string]any)
		testEntry := actionsMap["test"].(map[string]any)
		results := testEntry["results"].(map[string]any)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", results["stdout"])
		assert.Equal(t, "FAIL\texample/widget\t0.012s\n", results["stderr"])
		assert.Equal(t, float64(1), results["exitCode"])
	})

	t.Run("mixed streamed stderr progress and buffered stdout preserves stdout in human mode", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		mixedYAML := strings.Replace(bufferedSolutionYAML, "exitCode: 1", "exitCode: 1\n          mode: stderr-progress", 1)
		require.NoError(t, os.WriteFile(solFile, []byte(mixedYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"NO_COLOR":        "1",
		}
		unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", stdout)
		assert.Contains(t, stderr, "FAIL\texample/widget\t0.012s\n")
	})

	t.Run("run solution also preserves buffered stdout on failure", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(bufferedSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"NO_COLOR":        "1",
		}
		unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "solution", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Equal(t, "--- FAIL: TestWidget (0.01s)\n    widget_test.go:14: expected 200, got 500\n", stdout)
		assert.Equal(t, 1, strings.Count(stderr, "command exited with code 1"), "stderr:\n%s", stderr)
	})
}

func TestIntegration_RunAction_GitHubActionsCIOutput(t *testing.T) {
	t.Parallel()

	const ciWorkflowSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: ci-workflow-output
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      lint:
        description: Run linter
        provider: mockexec
        inputs:
          stdout: "lint clean\n"
          exitCode: 0
      test:
        description: Run unit tests
        dependsOn:
          - lint
        provider: mockexec
        inputs:
          stdout: "--- FAIL: TestCI (0.02s)\n"
          stderr: "FAIL\texample/ci\t0.020s\n"
          exitCode: 1
`

	t.Run("auto mode emits group endgroup and error annotation when GITHUB_ACTIONS=true", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(ciWorkflowSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":  "true",
		}
		unset := []string{"NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile)

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.Contains(t, stdout, "lint clean\n")
		assert.Contains(t, stdout, "--- FAIL: TestCI (0.02s)\n")
		assert.Contains(t, stderr, "::group::lint: Run linter\n")
		assert.Contains(t, stderr, "::group::test: Run unit tests\n")
		assert.Contains(t, stderr, "::error title=test::")
		assert.Equal(t, 2, strings.Count(stderr, "::endgroup::\n"), "stderr:\n%s", stderr)

		testGroupIdx := strings.Index(stderr, "::group::test: Run unit tests\n")
		lastEndgroupIdx := strings.LastIndex(stderr, "::endgroup::\n")
		errAnnotIdx := strings.Index(stderr, "::error title=test::")
		assert.Greater(t, lastEndgroupIdx, testGroupIdx)
		assert.Greater(t, errAnnotIdx, lastEndgroupIdx,
			"::error annotation must be emitted after ::endgroup:: so GitHub Actions does not fold it")
	})

	t.Run("json output with GITHUB_ACTIONS=true keeps stdout valid JSON and writes annotations to stderr", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(ciWorkflowSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":  "true",
		}
		unset := []string{"NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}

		stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
			"run", "action", "test", "-f", solFile, "-o", "json")

		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.NotContains(t, stdout, "::group::")

		var envelope map[string]any
		require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout must be valid JSON:\n%s", stdout)
		assert.Contains(t, stderr, "::group::test: Run unit tests\n")
		assert.Contains(t, stderr, "::error title=test::")
	})

	t.Run("ci-output none flag env and config suppress workflow commands", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(ciWorkflowSolutionYAML), 0o600))

		// 1. Flag --ci-output=none suppresses workflow commands under GITHUB_ACTIONS=true.
		flagEnv := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":  "true",
		}
		_, stderrFlag, codeFlag := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}, flagEnv,
			"run", "action", "test", "-f", solFile, "--ci-output=none")
		assert.Equal(t, exitcode.ActionFailed, codeFlag)
		assert.NotContains(t, stderrFlag, "::group::")
		assert.NotContains(t, stderrFlag, "::endgroup::")
		assert.NotContains(t, stderrFlag, "::error")

		// 2. Env SCAFCTL_ACTION_CIOUTPUT=none suppresses workflow commands.
		envOverride := map[string]string{
			"XDG_CONFIG_HOME":         baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":           baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":          baseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":          "true",
			"SCAFCTL_ACTION_CIOUTPUT": "none",
		}
		_, stderrEnv, codeEnv := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"NO_COLOR"}, envOverride,
			"run", "action", "test", "-f", solFile)
		assert.Equal(t, exitcode.ActionFailed, codeEnv)
		assert.NotContains(t, stderrEnv, "::group::")
		assert.NotContains(t, stderrEnv, "::error")

		// 3. Config action.ciOutput: none suppresses by default, but --ci-output=github-actions overrides it.
		cfgBaseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "action:\n  ciOutput: none\n")
		cfgEnv := map[string]string{
			"XDG_CONFIG_HOME": cfgBaseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   cfgBaseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  cfgBaseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":  "true",
		}
		_, stderrCfg, codeCfg := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}, cfgEnv,
			"run", "action", "test", "-f", solFile)
		assert.Equal(t, exitcode.ActionFailed, codeCfg)
		assert.NotContains(t, stderrCfg, "::group::")

		_, stderrCfgOverride, codeCfgOverride := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}, cfgEnv,
			"run", "action", "test", "-f", solFile, "--ci-output=github-actions")
		assert.Equal(t, exitcode.ActionFailed, codeCfgOverride)
		assert.Contains(t, stderrCfgOverride, "::group::test: Run unit tests\n")
		assert.Contains(t, stderrCfgOverride, "::error title=test::")
	})

	t.Run("invalid --ci-output returns InvalidInput exit code", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solFile, []byte(ciWorkflowSolutionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
		}
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"GITHUB_ACTIONS", "NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}, env,
			"run", "action", "test", "-f", solFile, "--ci-output=bogus")
		assert.Equal(t, exitcode.InvalidInput, code, "stderr: %s", stderr)
		assert.Contains(t, stderr, "invalid CI output mode")
	})

	t.Run("neutralizes workflow command injection in descriptions and provider errors under GITHUB_ACTIONS=true", func(t *testing.T) {
		t.Parallel()
		baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
		solDir := t.TempDir()
		solFile := filepath.Join(solDir, "solution.yaml")
		injectionYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: ci-injection-guard
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      test:
        description: "Run tests\n::stop-commands::pwn ##[error]banner-spoof"
        provider: mockexec
        inputs:
          error: "boom\r::add-matcher::evil.json\n\u00a0::stop-commands::nbsp\n  ::error file=a.go::spoof\nmidline ##[error]spoof"
          exitCode: 1
`
		require.NoError(t, os.WriteFile(solFile, []byte(injectionYAML), 0o600))

		env := map[string]string{
			"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
			"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
			"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
			"GITHUB_ACTIONS":  "true",
			"NO_COLOR":        "1",
		}
		_, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, []string{"SCAFCTL_ACTION_CIOUTPUT"}, env,
			"run", "action", "test", "-f", solFile)
		assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
		assert.NotContains(t, stderr, "##[", "legacy hash commands must be neutralized on stderr")
		for _, line := range strings.Split(stderr, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "::") {
				assert.True(t,
					strings.HasPrefix(trimmed, "::group::") ||
						strings.HasPrefix(trimmed, "::endgroup::") ||
						strings.HasPrefix(trimmed, "::error title=test::"),
					"forged workflow command leaked to stderr line: %q", line,
				)
			}
		}
	})
}

func TestIntegration_RunAction_NoColor(t *testing.T) {
	t.Parallel()

	const noColorSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-color-actions
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      lint:
        description: Check formatting
        provider: mockexec
        inputs:
          stdout: "format ok\n"
          exitCode: 0
      skipme:
        description: Conditional deploy
        dependsOn:
          - lint
        when:
          expr: "false"
        provider: mockexec
        inputs:
          stdout: "never runs\n"
          exitCode: 0
      test:
        description: Run suite
        dependsOn:
          - skipme
        provider: mockexec
        inputs:
          stdout: "--- FAIL: TestSuite\n"
          exitCode: 1
`

	baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
	solDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(noColorSolutionYAML), 0o600))

	env := map[string]string{
		"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
		"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
		"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
		"NO_COLOR":        "1",
	}
	unset := []string{"GITHUB_ACTIONS", "SCAFCTL_ACTION_CIOUTPUT"}

	stdout, stderr, code := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, env,
		"run", "action", "-f", solFile, "--verbose")

	assert.Equal(t, exitcode.ActionFailed, code, "stderr: %s", stderr)
	assertNoANSIOrStatusEmoji(t, stdout)
	assertNoANSIOrStatusEmoji(t, stderr)
	assert.Contains(t, stdout, "format ok\n")
	assert.Contains(t, stdout, "--- FAIL: TestSuite\n")
	assert.Contains(t, stderr, "[ok] lint")
	assert.Contains(t, stderr, "[skip] skipme")
	assert.Contains(t, stderr, "[fail] test")
}

func TestIntegration_RunAction_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	const embedderSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: embedder-ci-output
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      verify:
        description: Verify embedder action
        provider: mockexec
        inputs:
          stdout: "--- FAIL: TestEmbedder\n"
          exitCode: 1
`

	baseEnv := setupIsolatedCatalogWithMockExec(t, "mycli", "")
	solDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(embedderSolutionYAML), 0o600))

	// 1. MYCLI_ACTION_CIOUTPUT=none suppresses GitHub Actions workflow commands for mycli.
	suppressEnv := map[string]string{
		"XDG_CONFIG_HOME":       baseEnv["XDG_CONFIG_HOME"],
		"XDG_DATA_HOME":         baseEnv["XDG_DATA_HOME"],
		"XDG_CACHE_HOME":        baseEnv["XDG_CACHE_HOME"],
		"GITHUB_ACTIONS":        "true",
		"MYCLI_ACTION_CIOUTPUT": "none",
		"NO_COLOR":              "1",
	}
	stdoutSuppress, stderrSuppress, codeSuppress := runEmbedderMyCLI(t, solDir, []string{"SCAFCTL_ACTION_CIOUTPUT"}, suppressEnv,
		"run", "action", "verify", "-f", solFile, "--verbose")
	assert.Equal(t, exitcode.ActionFailed, codeSuppress, "stderr: %s", stderrSuppress)
	assert.Equal(t, "--- FAIL: TestEmbedder\n", stdoutSuppress)
	assert.NotContains(t, stderrSuppress, "::group::")
	assert.NotContains(t, stderrSuppress, "::error")
	assertNoANSIOrStatusEmoji(t, stdoutSuppress)
	assertNoANSIOrStatusEmoji(t, stderrSuppress)
	assert.Contains(t, stderrSuppress, "[fail] verify")

	// 2. --ci-output=github-actions emits workflow commands even when GITHUB_ACTIONS is unset.
	forceGHAEnv := map[string]string{
		"XDG_CONFIG_HOME": baseEnv["XDG_CONFIG_HOME"],
		"XDG_DATA_HOME":   baseEnv["XDG_DATA_HOME"],
		"XDG_CACHE_HOME":  baseEnv["XDG_CACHE_HOME"],
	}
	stdoutGHA, stderrGHA, codeGHA := runEmbedderMyCLI(t, solDir,
		[]string{"GITHUB_ACTIONS", "NO_COLOR", "MYCLI_ACTION_CIOUTPUT", "SCAFCTL_ACTION_CIOUTPUT"},
		forceGHAEnv,
		"run", "action", "verify", "-f", solFile, "--ci-output=github-actions")
	assert.Equal(t, exitcode.ActionFailed, codeGHA, "stderr: %s", stderrGHA)
	assert.Equal(t, "--- FAIL: TestEmbedder\n", stdoutGHA)
	assert.Contains(t, stderrGHA, "::group::verify: Verify embedder action\n")
	assert.Contains(t, stderrGHA, "::endgroup::\n")
	assert.Contains(t, stderrGHA, "::error title=verify::")
}

func TestIntegration_RunAction_StrictResultSchemaConsistentAcrossModes(t *testing.T) {
	t.Parallel()

	const strictSchemaSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: strict-result-schema-modes
  version: 0.1.0
bundle:
  plugins:
    - name: mockexec
      kind: provider
      version: ^0.1.0
spec:
  workflow:
    actions:
      provision:
        description: Provision resource with strict result schema
        provider: mockexec
        inputs:
          mode: strict-schema-progress
          stderr: "creating resource...\n"
        resultSchemaMode: error
        resultSchema:
          type: object
          required:
            - id
          properties:
            id:
              type: integer
          additionalProperties: false
`

	baseEnv := setupIsolatedCatalogWithMockExec(t, "scafctl", "")
	solDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(strictSchemaSolutionYAML), 0o600))

	unset := []string{"GITHUB_ACTIONS", "NO_COLOR", "SCAFCTL_ACTION_CIOUTPUT"}

	// 1. Default terminal mode succeeds and streams stderr progress.
	stdoutTerm, stderrTerm, codeTerm := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, baseEnv,
		"run", "action", "provision", "-f", solFile)
	require.Equal(t, 0, codeTerm, "terminal mode failed: stdout=%s stderr=%s", stdoutTerm, stderrTerm)
	assert.Contains(t, stderrTerm, "creating resource...\n")

	// 2. Structured -o json mode ALSO succeeds against additionalProperties: false
	// and merges captured stderr into the output envelope after validation.
	stdoutJSON, stderrJSON, codeJSON := runBinaryIsolatedEnv(t, binaryPath, solDir, unset, baseEnv,
		"run", "action", "provision", "-f", solFile, "-o", "json")
	require.Equal(t, 0, codeJSON, "-o json mode failed: stdout=%s stderr=%s", stdoutJSON, stderrJSON)

	var parsed struct {
		Actions map[string]struct {
			Status  string         `json:"status"`
			Results map[string]any `json:"results"`
		} `json:"actions"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdoutJSON), &parsed), "stdout JSON: %s", stdoutJSON)
	assert.Equal(t, "succeeded", parsed.Actions["provision"].Status)
	assert.Equal(t, float64(1), parsed.Actions["provision"].Results["id"])
	assert.Equal(t, "creating resource...\n", parsed.Actions["provision"].Results["stderr"])
}
