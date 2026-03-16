// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// Build the binary once for all tests
	tmpDir, err := os.MkdirTemp("", "scafctl-integration-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	binaryPath = filepath.Join(tmpDir, "scafctl")

	// Build from project root
	projectRoot := findProjectRoot()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, "./cmd/scafctl/scafctl.go")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		panic("failed to build scafctl: " + err.Error() + "\n" + string(output))
	}

	os.Exit(m.Run())
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir

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
	assert.Contains(t, stdout, "Scaffolding Commands:")
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
	assert.Contains(t, stdout, "static")
	assert.Contains(t, stdout, "env")
	assert.Contains(t, stdout, "http")
	assert.Contains(t, stdout, "exec")
	assert.Contains(t, stdout, "cel")
	assert.Contains(t, stdout, "directory")
}

func TestIntegration_GetProviderJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "get", "provider", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "static")
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
	assert.Contains(t, stdout, "map.merge")
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
	assert.Contains(t, stdout, "toHcl")
	assert.Contains(t, stdout, "toYaml")
	assert.Contains(t, stdout, "fromYaml")
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
	stdout, _, exitCode := runScafctl(t, "run", "provider", "static", "--input", "value=test", "--capability", "from")

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
	stdout, _, exitCode := runScafctl(t, "get", "provider", "static", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "cliUsage")
	assert.Contains(t, stdout, "scafctl run provider static")
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

func TestIntegration_RunSolution_FileNotFound(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "/nonexistent/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.True(t, strings.Contains(stderr, "not found") || strings.Contains(stderr, "no such file"))
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
	_, stderr, exitCode := runScafctl(t,
		"lint",
		"-f", "tests/integration/solutions/edge-cases/null-resolver/solution.yaml",
		"-o", "json",
	)

	// Lint returns exit code 1 because spec validation rejects the null resolver
	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stderr, "null value")
}

func TestIntegration_RunSolution_DryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
	)

	assert.Equal(t, 0, exitCode)
	// Dry run should show what would happen without executing
	t.Logf("dry-run output: %s", stdout)
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
	assert.Contains(t, stdout, "--dry-run")
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
	// __execution metadata is always included in run resolver output
	assert.Contains(t, stdout, "__execution")
	assert.Contains(t, stdout, "resolvers")
	assert.Contains(t, stdout, "summary")
	assert.Contains(t, stdout, "totalDuration")
	assert.Contains(t, stdout, "phaseCount")
}

func TestIntegration_RunResolver_HideExecution(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--hide-execution",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.NotContains(t, stdout, "__execution")
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
	assert.Contains(t, stdout, "__execution")
}

func TestIntegration_RunResolver_DryRun(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--dry-run",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "dryRun")
	assert.Contains(t, stdout, "executionPlan")
	assert.Contains(t, stdout, "resolvers")
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
		"--hide-execution",
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

func TestIntegration_RunResolver_MutualExclusive_DryRunGraph(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolver-demo.yaml",
		"--dry-run",
		"--graph",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "mutually exclusive")
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
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "__execution")
	assert.Contains(t, stdout, "resolvers")
	assert.Contains(t, stdout, "summary")
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

	// With the embedded shell, command-not-found returns exit code 127
	// (not a Go error), so the action "succeeds" with exitCode 127 in data.
	// The CLI exits 0 because the provider did not return a Go error.
	assert.Equal(t, 0, exitCode)
	t.Logf("stderr: %s", stderr)
}

func TestIntegration_RunSolution_RetryIfWithRetryEnabled(t *testing.T) {
	t.Parallel()
	// Test that retryIf: "true" allows retries on actual errors
	// This creates a temp script that succeeds on second run
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "retry-if-enabled.yaml")
	scriptPath := filepath.Join(tmpDir, "retry-script.sh")
	counterFile := filepath.Join(tmpDir, "counter.txt")

	// Create a script that fails first time, succeeds second time
	script := `#!/bin/sh
if [ -f "` + counterFile + `" ]; then
  echo "Second attempt - success"
  exit 0
else
  echo "1" > "` + counterFile + `"
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
          command: "` + scriptPath + `"
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
// Resolver Graph Tests
// ============================================================================

func TestIntegration_ResolverGraph(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--graph",
	)

	assert.Equal(t, 0, exitCode)
	// Should show dependency relationships
	assert.Contains(t, stdout, "Resolver Dependency Graph")
	t.Logf("graph output: %s", stdout)
}

func TestIntegration_ResolverGraphMermaid(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--graph",
		"--graph-format", "mermaid",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "graph")
}

func TestIntegration_ResolverGraphJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t,
		"render", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--graph",
		"--graph-format", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "phases")
}

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
}

func TestIntegration_AuthList(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "list")

	assert.Equal(t, 0, exitCode)
	// With no active login sessions the command succeeds and reports no tokens.
	// If tokens are cached from a prior login the handler name would appear instead.
	assert.True(t,
		strings.Contains(stdout, "No cached tokens found") ||
			strings.Contains(stdout, "entra") ||
			strings.Contains(stdout, "github") ||
			strings.Contains(stdout, "gcp"),
		"expected no-token message or token rows, got: %q", stdout,
	)
}

func TestIntegration_AuthListJSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "list", "-o", "json")

	// Command should always exit 0 regardless of whether tokens are present.
	assert.Equal(t, 0, exitCode)
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

func TestIntegration_AuthTokenHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "token", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--scope")
	assert.Contains(t, stdout, "--min-valid-for")
	assert.Contains(t, stdout, "--force-refresh")
}

func TestIntegration_AuthLoginGCPHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "login", "gcp", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "gcp")
	assert.Contains(t, stdout, "--flow")
	assert.Contains(t, stdout, "--impersonate-service-account")
}

func TestIntegration_AuthLoginGCPInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "gcp", "--flow", "invalid-flow")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown flow")
	assert.Contains(t, stderr, "gcp")
}

func TestIntegration_AuthStatusGCP(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "auth", "status", "gcp")

	// Should succeed even if not authenticated (shows "not authenticated")
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_AuthLogoutGCP(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "logout", "gcp")

	// Should succeed regardless of authentication state
	assert.Equal(t, 0, exitCode)
	// May show "Not currently authenticated" or "Successfully logged out" depending on environment
	assert.True(t,
		strings.Contains(stdout, "Not currently authenticated") || strings.Contains(stdout, "Successfully logged out"),
		"expected logout message, got: %s", stdout,
	)
}

func TestIntegration_AuthLoginEntraHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "login", "entra", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "entra")
	assert.Contains(t, stdout, "--flow")
	assert.Contains(t, stdout, "--tenant")
	assert.Contains(t, stdout, "--callback-port")
}

func TestIntegration_AuthLoginEntraInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "entra", "--flow", "bogus-flow")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown flow")
	assert.Contains(t, stderr, "interactive")
}

func TestIntegration_AuthLoginCallbackPortSupported(t *testing.T) {
	t.Parallel()
	// GitHub now supports --callback-port for the interactive (PKCE) flow.
	// The login command should accept this flag without error (it will still
	// fail because we don't complete the browser auth, but it shouldn't
	// reject the flag itself).
	stdout, _, exitCode := runScafctl(t, "auth", "login", "github", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--callback-port")
}

func TestIntegration_AuthLoginGitHubInteractiveFlow(t *testing.T) {
	t.Parallel()
	// Verify that --flow interactive is accepted for GitHub
	stdout, _, exitCode := runScafctl(t, "auth", "login", "github", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "--flow")
}

func TestIntegration_AuthLoginGitHubInvalidFlow(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "auth", "login", "github", "--flow", "bogus-flow")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown flow")
	assert.Contains(t, stderr, "github")
}

func TestIntegration_AuthLoginGitHubAppFlow(t *testing.T) {
	t.Parallel()
	// github-app flow without required config should fail with a config error,
	// not a flow-parsing error.
	_, stderr, exitCode := runScafctl(t, "auth", "login", "github", "--flow", "github-app")

	assert.NotEqual(t, 0, exitCode)
	// Should fail with a config error about missing app ID or private key
	assert.Contains(t, stderr, "app ID is required")
}

func TestIntegration_AuthStatusEntra(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "auth", "status", "entra")

	// Should succeed even if not authenticated
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_AuthLogoutEntra(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "auth", "logout", "entra")

	assert.Equal(t, 0, exitCode)
	assert.True(t,
		strings.Contains(stdout, "Not currently authenticated") || strings.Contains(stdout, "Successfully logged out"),
		"expected logout message, got: %s", stdout,
	)
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
	assert.Contains(t, stdout, "LINT RULES:")
	assert.Contains(t, stdout, "--file")
	assert.Contains(t, stdout, "--severity")
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

// ============================================================================
// Build Command Tests
// ============================================================================

func TestIntegration_BuildHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "build", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "build")
	assert.Contains(t, stdout, "solution")
}

func TestIntegration_BuildSolutionHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "build", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Build a solution")
	assert.Contains(t, stdout, "--version")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--force")
	assert.Contains(t, stdout, "--no-bundle")
	assert.Contains(t, stdout, "--no-vendor")
	assert.Contains(t, stdout, "--bundle-max-size")
	assert.Contains(t, stdout, "--dry-run")
}

func TestIntegration_BuildSolution_UsesMetadataVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build without --version flag - should use metadata version (1.0.0)
	stdout, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml")

	assert.Equal(t, 0, exitCode)
	// Should report the version from metadata
	assert.Contains(t, stdout, "1.0.0")
}

func TestIntegration_BuildSolution_VersionOverrideWarning(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build with different version than metadata - should warn
	stdout, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "9.9.9")

	assert.Equal(t, 0, exitCode)
	// Should warn about overriding metadata version
	assert.Contains(t, stdout, "overrides metadata version")
	assert.Contains(t, stdout, "9.9.9")
}

func TestIntegration_BuildSolution_FileNotFound(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "build", "solution", "nonexistent.yaml", "--version", "1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to read")
}

func TestIntegration_BuildSolution_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_BuildSolution_WithName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--name", "my-custom-name")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-custom-name")
}

func TestIntegration_BuildSolution_ForceOverwrite(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// First build
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Second build without force should fail
	_, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "exists")

	// Third build with force should succeed
	stdout, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--force", "--no-cache")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--dry-run")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Dry run")
}

func TestIntegration_BuildSolution_NoBundle(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-bundle")

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
}

func TestIntegration_CatalogListHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "list", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "List all artifacts")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogList_Empty(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "catalog", "list", "-o", "json")

	assert.Equal(t, 0, exitCode)
	// Empty list should return empty JSON array or null
	assert.True(t, strings.Contains(stdout, "[]") || strings.Contains(stdout, "null"))
}

func TestIntegration_CatalogList_WithArtifacts(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List should show the artifact
	stdout, _, exitCode := runScafctl(t, "catalog", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")
}

func TestIntegration_CatalogList_FilterByKind(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List with filter should work
	stdout, _, exitCode := runScafctl(t, "catalog", "list", "--kind", "solution", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_CatalogInspectHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "catalog", "inspect", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show detailed information")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogInspect_NotFound(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "inspect", "nonexistent")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogInspect_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Inspect the artifact
	stdout, _, exitCode := runScafctl(t, "catalog", "inspect", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")
	assert.Contains(t, stdout, "digest")
}

func TestIntegration_CatalogInspect_SpecificVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build multiple versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Inspect specific version
	stdout, _, exitCode := runScafctl(t, "catalog", "inspect", "resolver-demo@1.0.0", "-o", "json")
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
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete without version should fail
	_, stderr, exitCode := runScafctl(t, "catalog", "delete", "resolver-demo")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version required")
}

func TestIntegration_CatalogDelete_NotFound(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "delete", "nonexistent@1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogDelete_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete the artifact
	stdout, _, exitCode := runScafctl(t, "catalog", "delete", "resolver-demo@1.0.0")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Deleted")

	// Verify it's gone
	_, stderr, exitCode := runScafctl(t, "catalog", "inspect", "resolver-demo@1.0.0")
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
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "catalog", "prune")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No orphaned content")
}

func TestIntegration_CatalogPrune_JSON(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "catalog", "prune", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "removedManifests")
	assert.Contains(t, stdout, "removedBlobs")
	assert.Contains(t, stdout, "reclaimedBytes")
}

func TestIntegration_CatalogPrune_AfterDelete(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Delete the artifact (leaves orphaned blobs)
	_, _, exitCode = runScafctl(t, "catalog", "delete", "resolver-demo@1.0.0")
	require.Equal(t, 0, exitCode)

	// Prune should clean up
	stdout, _, exitCode := runScafctl(t, "catalog", "prune", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have pruned something
	assert.Contains(t, stdout, "removedBlobs")
}

// =============================================================================
// Run Solution from Catalog Tests
// =============================================================================

func TestIntegration_RunSolution_FromCatalog_NotFound(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Try to run a solution that doesn't exist in catalog
	stdout, stderr, exitCode := runScafctl(t, "run", "solution", "nonexistent-solution")
	assert.NotEqual(t, 0, exitCode)
	// Reports artifact not found in catalog and file system
	combined := stdout + stderr
	assert.True(t, strings.Contains(combined, "not found") || strings.Contains(combined, "no such file or directory"),
		"expected error about missing solution, got stdout=%q stderr=%q", stdout, stderr)
}

func TestIntegration_RunSolution_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name (should pick latest version)
	stdout, _, exitCode := runScafctl(t, "run", "resolver", "-f", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_ByNameVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build two versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name@version
	stdout, _, exitCode := runScafctl(t, "run", "resolver", "-f", "resolver-demo@1.0.0", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_FallbackToFile(t *testing.T) {
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Run a solution by file path (not bare name) - should use file
	stdout, _, exitCode := runScafctl(t, "run", "resolver", "-f", "examples/resolver-demo.yaml", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output from file
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_PathNotBareName(t *testing.T) {
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// A path with a separator should not be treated as a bare name
	// This should try to open a file, not lookup in catalog
	_, stderr, exitCode := runScafctl(t, "run", "solution", "./nonexistent.yaml")
	assert.NotEqual(t, 0, exitCode)
	// Should report file not found, not catalog not found
	assert.Contains(t, stderr, "Failed reading file")
}

// Render Solution from Catalog Tests
// =============================================================================

func TestIntegration_RenderSolution_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Render the solution graph from catalog by name (using --graph flag since resolver-demo has no workflow)
	stdout, _, exitCode := runScafctl(t, "render", "solution", "-f", "resolver-demo", "--graph")
	assert.Equal(t, 0, exitCode)
	// Should have graph output with resolver info
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "Phase")
}

// Explain Solution from Catalog Tests
// =============================================================================

func TestIntegration_ExplainSolution_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Explain the solution from catalog by name
	stdout, _, exitCode := runScafctl(t, "explain", "solution", "resolver-demo")
	assert.Equal(t, 0, exitCode)
	// Should have solution metadata
	assert.Contains(t, stdout, "resolver-demo")
}

// Lint Solution from Catalog Tests
// =============================================================================

func TestIntegration_Lint_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Lint the solution from catalog by name
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have lint output
	assert.Contains(t, stdout, "findings")
}

// Get Solution from Catalog Tests
// =============================================================================

func TestIntegration_GetSolution_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Get the solution from catalog by name
	stdout, _, exitCode := runScafctl(t, "get", "solution", "-p", "resolver-demo", "-o", "yaml")
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Try to save without output flag
	_, stderr, exitCode := runScafctl(t, "catalog", "save", "resolver-demo")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "required")
}

func TestIntegration_CatalogSave_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	outputPath := tmpDir + "/nonexistent.tar"
	_, stderr, exitCode := runScafctl(t, "catalog", "save", "nonexistent", "-o", outputPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogSave_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save to tar
	outputPath := tmpDir + "/export.tar"
	stdout, _, exitCode := runScafctl(t, "catalog", "save", "resolver-demo", "-o", outputPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")

	// Verify file was created
	info, err := os.Stat(outputPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0))
}

func TestIntegration_CatalogSave_SpecificVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build multiple versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Save specific version
	outputPath := tmpDir + "/v1.tar"
	stdout, _, exitCode := runScafctl(t, "catalog", "save", "resolver-demo@1.0.0", "-o", outputPath)
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "load", "--input", "/nonexistent/path.tar")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no such file")
}

func TestIntegration_CatalogLoad_Success(t *testing.T) {
	// Create source catalog
	srcDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", srcDir)
	t.Setenv("XDG_CACHE_HOME", srcDir)

	// Build and save an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	tarPath := srcDir + "/export.tar"
	_, _, exitCode = runScafctl(t, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Switch to destination catalog
	dstDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dstDir)
	t.Setenv("XDG_CACHE_HOME", dstDir)

	// Load the artifact
	stdout, _, exitCode := runScafctl(t, "catalog", "load", "--input", tarPath)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
	assert.Contains(t, stdout, "1.0.0")

	// Verify artifact is in catalog
	stdout, _, exitCode = runScafctl(t, "catalog", "list")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_CatalogLoad_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save it
	tarPath := tmpDir + "/export.tar"
	_, _, exitCode = runScafctl(t, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Try to load into same catalog (should fail)
	_, stderr, exitCode := runScafctl(t, "catalog", "load", "--input", tarPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "already exists")
}

func TestIntegration_CatalogLoad_ForceOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save it
	tarPath := tmpDir + "/export.tar"
	_, _, exitCode = runScafctl(t, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Load with force (should succeed)
	stdout, _, exitCode := runScafctl(t, "catalog", "load", "--input", tarPath, "--force")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

// =============================================================================
// Catalog Save/Load Round Trip Tests
// =============================================================================

func TestIntegration_CatalogSaveLoad_RoundTrip(t *testing.T) {
	// Create source catalog
	srcDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", srcDir)
	t.Setenv("XDG_CACHE_HOME", srcDir)

	// Build an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Save to tar
	tarPath := srcDir + "/export.tar"
	_, _, exitCode = runScafctl(t, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Switch to destination catalog
	dstDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dstDir)
	t.Setenv("XDG_CACHE_HOME", dstDir)

	// Load from tar
	_, _, exitCode = runScafctl(t, "catalog", "load", "--input", tarPath)
	require.Equal(t, 0, exitCode)

	// Verify the solution can be run
	stdout, _, exitCode := runScafctl(t, "run", "resolver", "-f", "resolver-demo", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Push without --catalog and no default configured should error.
	// Since artifact also doesn't exist locally, kind inference fails first.
	_, stderr, exitCode := runScafctl(t, "catalog", "push", "my-solution@1.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogPush_ArtifactNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Push a nonexistent artifact
	_, stderr, exitCode := runScafctl(t, "catalog", "push", "nonexistent@1.0.0", "--catalog", "ghcr.io/test/scafctl")
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
}

func TestIntegration_CatalogPull_InvalidReference(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Pull with invalid reference (no registry)
	_, stderr, exitCode := runScafctl(t, "catalog", "pull", "just-a-name")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid")
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Try to delete from a fake remote - should detect it as remote
	// and fail with auth/network error (not "invalid reference")
	_, stderr, exitCode := runScafctl(t, "catalog", "delete", "fake.registry.io/myorg/solutions/test@1.0.0")
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "my-solution", "stable")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version required")
}

func TestIntegration_CatalogTag_RejectsSemverAlias(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "my-solution@1.0.0", "2.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "semver version")
}

func TestIntegration_CatalogTag_ArtifactNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "nonexistent@1.0.0", "stable", "--kind", "solution")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogTag_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Tag it
	stdout, _, exitCode := runScafctl(t, "catalog", "tag", "resolver-demo@1.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Tagged")
	assert.Contains(t, stdout, "stable")
}

func TestIntegration_CatalogTag_MoveAlias(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build two versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0", "--force", "--no-cache")
	require.Equal(t, 0, exitCode)

	// Tag v1 as stable
	stdout, _, exitCode := runScafctl(t, "catalog", "tag", "resolver-demo@1.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "1.0.0")

	// Move stable to v2
	stdout, _, exitCode = runScafctl(t, "catalog", "tag", "resolver-demo@2.0.0", "stable")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "2.0.0")
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
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "info", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "totalSize")
	assert.Contains(t, stdout, "totalFiles")
}

func TestIntegration_CacheClear_Empty(t *testing.T) {
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--force")

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
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--force", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "removedFiles")
	assert.Contains(t, stdout, "removedBytes")
}

func TestIntegration_CacheClear_HTTPKind(t *testing.T) {
	// Create a temp directory for the cache
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--kind", "http", "--force")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No cached content found")
}

func TestIntegration_CacheClear_BuildKind(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--kind", "build", "--force")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No cached content found")
}

func TestIntegration_CacheInfo_ShowsBuildCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "cache", "info")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Build Cache")
}

func TestIntegration_BuildSolution_NoCacheFlag(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_BuildCacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// First build
	stdout1, stderr1, exitCode1 := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	if exitCode1 != 0 {
		t.Logf("stdout: %s", stdout1)
		t.Logf("stderr: %s", stderr1)
	}
	require.Equal(t, 0, exitCode1)
	assert.Contains(t, stdout1, "Built")

	// Second build with same inputs — should be a cache hit
	stdout2, stderr2, exitCode2 := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	if exitCode2 != 0 {
		t.Logf("stdout: %s", stdout2)
		t.Logf("stderr: %s", stderr2)
	}
	assert.Equal(t, 0, exitCode2)
	assert.Contains(t, stdout2, "cache hit")
}

func TestIntegration_BuildSolution_NoCacheBypassesCacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// First build (populates cache)
	_, _, exitCode1 := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode1)

	// Second build with --no-cache — should NOT be a cache hit, should fail with "already exists" since --force is not set
	stdout2, _, exitCode2 := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--no-cache")
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
	stdout, stderr, exitCode := runScafctl(t,
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
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "tests/integration/testdata/solution-provider/parent-action.yaml",
		"-o", "json",
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "succeeded")
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
              source: "` + childPath + `"
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
              source: "` + filepath.Join(tmpDir, "self.yaml") + `"
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

	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-filter-test
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "tests/integration/testdata/solution-provider/child.yaml"
              resolvers:
                - greeting
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
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

	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-filter-notfound
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "tests/integration/testdata/solution-provider/child.yaml"
              resolvers:
                - does-not-exist
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stderr: %s", stderr)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "does not exist")
}

func TestIntegration_SolutionProvider_Timeout(t *testing.T) {
	t.Parallel()
	// Use a very short timeout with a normal child solution.
	// The child should still succeed because the timeout is generous enough.
	tmpDir := t.TempDir()

	parentSolution := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: timeout-test
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "tests/integration/testdata/solution-provider/child.yaml"
              inputs:
                message: "with timeout"
              timeout: "30s"
`
	parentPath := filepath.Join(tmpDir, "parent.yaml")
	require.NoError(t, os.WriteFile(parentPath, []byte(parentSolution), 0o644))

	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", parentPath,
	)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "hello from child")
	assert.Contains(t, stdout, "with timeout")
}

// ============================================================================
// Bundle Command Tests
// ============================================================================

func TestIntegration_BundleHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "bundle", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "bundle")
	assert.Contains(t, stdout, "verify")
	assert.Contains(t, stdout, "diff")
	assert.Contains(t, stdout, "extract")
}

func TestIntegration_BundleVerifyHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "bundle", "verify", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Validate that a built artifact")
	assert.Contains(t, stdout, "--strict")
}

func TestIntegration_BundleDiffHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "bundle", "diff", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show what changed between two versions")
	assert.Contains(t, stdout, "--files-only")
	assert.Contains(t, stdout, "--solution-only")
	assert.Contains(t, stdout, "--ignore")
}

func TestIntegration_BundleExtractHelp(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "bundle", "extract", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Extract files from a bundled solution artifact")
	assert.Contains(t, stdout, "--output-dir")
	assert.Contains(t, stdout, "--resolver")
	assert.Contains(t, stdout, "--action")
	assert.Contains(t, stdout, "--include")
	assert.Contains(t, stdout, "--list-only")
	assert.Contains(t, stdout, "--flatten")
}

func TestIntegration_BundleVerify_MissingRef(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "bundle", "verify")

	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_BundleDiff_MissingArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "bundle", "diff")

	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_BundleExtract_MissingRef(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "bundle", "extract")

	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_BundleVerify_AfterBuild(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Verify the built artifact
	stdout, stderr, exitCode := runScafctl(t, "bundle", "verify", "resolver-demo@1.0.0")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_BundleExtract_AfterBuild(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Extract the built artifact
	extractDir := filepath.Join(tmpDir, "extracted")
	stdout, stderr, exitCode := runScafctl(t, "bundle", "extract", "resolver-demo@1.0.0", "--output-dir", extractDir)
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
}

func TestIntegration_BundleExtract_ListOnly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List files — may have no bundle layer if the solution has no bundle config
	stdout, stderr, exitCode := runScafctl(t, "bundle", "extract", "resolver-demo@1.0.0", "--list-only")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
	// Either lists files or warns about no bundle — both are valid
	assert.True(t, strings.Contains(stdout, "Total") || strings.Contains(stdout, "no bundle"),
		"expected either file list or no-bundle warning")
}

func TestIntegration_BundleDiff_SameVersion(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build two versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0", "--no-cache")
	require.Equal(t, 0, exitCode)

	// Diff them
	stdout, stderr, exitCode := runScafctl(t, "bundle", "diff", "resolver-demo@1.0.0", "resolver-demo@2.0.0")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Comparing")
	assert.Contains(t, stdout, "Summary")
}

func TestIntegration_BuildSolution_NestedBundle(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Build the nested-bundle example — should discover sub-solution files recursively
	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/solutions/nested-bundle/parent.yaml", "--version", "1.0.0", "--dry-run")
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

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

	_, stderr, exitCode := runScafctl(t, "vendor", "update", solPath)

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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml",
		"--version", "1.0.0", "--dedupe")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_WithDedupeDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml",
		"--version", "1.0.0", "--dedupe=false")

	if exitCode != 0 {
		t.Logf("stdout: %s", stdout)
		t.Logf("stderr: %s", stderr)
	}
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

func TestIntegration_BuildSolution_DryRunShowsDetails(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml",
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create a mock binary
	binPath := filepath.Join(tmpDir, "my-provider")
	require.NoError(t, os.WriteFile(binPath, []byte("fake-plugin-binary"), 0o755))

	stdout, stderr, exitCode := runScafctl(t, "build", "plugin",
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	// Create mock binaries for two platforms
	linuxBin := filepath.Join(tmpDir, "provider-linux")
	darwinBin := filepath.Join(tmpDir, "provider-darwin")
	require.NoError(t, os.WriteFile(linuxBin, []byte("linux-binary"), 0o755))
	require.NoError(t, os.WriteFile(darwinBin, []byte("darwin-binary"), 0o755))

	stdout, stderr, exitCode := runScafctl(t, "build", "plugin",
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	binPath := filepath.Join(tmpDir, "auth-handler")
	require.NoError(t, os.WriteFile(binPath, []byte("auth-binary"), 0o755))

	stdout, stderr, exitCode := runScafctl(t, "build", "plugin",
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

func TestIntegration_BuildPlugin_ForceOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary-v1"), 0o755))

	// First build
	_, _, exitCode := runScafctl(t, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.Equal(t, 0, exitCode)

	// Second build without --force should fail
	_, stderr, exitCode := runScafctl(t, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "already exists")

	// Third build with --force should succeed
	stdout, _, exitCode := runScafctl(t, "build", "plugin",
		"--name", "force-test",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath,
		"--force")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built force-test@1.0.0")
}

func TestIntegration_BuildPlugin_InvalidPlatform(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0o755))

	_, stderr, exitCode := runScafctl(t, "build", "plugin",
		"--name", "bad-plat",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "freebsd/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unsupported platform")
}

func TestIntegration_BuildPlugin_InvalidKind(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	binPath := filepath.Join(tmpDir, "provider")
	require.NoError(t, os.WriteFile(binPath, []byte("binary"), 0o755))

	_, stderr, exitCode := runScafctl(t, "build", "plugin",
		"--name", "bad-kind",
		"--kind", "solution",
		"--version", "1.0.0",
		"--platform", "linux/amd64="+binPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid kind")
}

func TestIntegration_BuildPlugin_BinaryNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "build", "plugin",
		"--name", "missing",
		"--kind", "provider",
		"--version", "1.0.0",
		"--platform", "linux/amd64=/nonexistent/path")
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
              path: "` + tmpDir + `"
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
	assert.Contains(t, stdout, "render-defaults")
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
	assert.True(t, toolNames["auth_status"], "expected auth_status tool")
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
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "1 + 2")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "3")
}

func TestIntegration_EvalCEL_WithVar(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "size(name) > 3", "-v", "name=hello")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "true")
}

func TestIntegration_EvalCEL_WithData(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "_.name == 'hello'", "--data", `{"name": "hello"}`)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "true")
}

func TestIntegration_EvalCEL_JSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "1 + 2", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "1 + 2", result["expression"])
}

func TestIntegration_EvalCEL_InvalidExpression(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "cel", "--expression", "invalid ++ syntax")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalCEL_MissingExpression(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "cel")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalTemplate_Simple(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "hello {{ .name }}", "-v", "name=world")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "hello world")
}

func TestIntegration_EvalTemplate_JSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "hi {{ .name }}", "-v", "name=test", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Contains(t, result["output"], "hi test")
}

func TestIntegration_EvalTemplate_ShowRefs(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "template", "-t", "{{ .name }} {{ .age }}", "-v", "name=test", "-v", "age=25", "--show-refs")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test")
}

func TestIntegration_EvalTemplate_MissingTemplate(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "template")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_CELValid(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "size(name) > 3", "--type", "cel")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "valid")
}

func TestIntegration_EvalValidate_CELInvalid(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "invalid +++ (", "--type", "cel")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_GoTemplateValid(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "{{ .name }}", "--type", "go-template")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "valid")
}

func TestIntegration_EvalValidate_GoTemplateInvalid(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "{{ .name", "--type", "go-template")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_EvalValidate_JSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "1 + 2", "--type", "cel", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, true, result["valid"])
	assert.Equal(t, "cel", result["type"])
}

func TestIntegration_EvalValidate_UnsupportedType(t *testing.T) {
	_, _, exitCode := runScafctl(t, "eval", "validate", "--expression", "test", "--type", "python")
	assert.NotEqual(t, 0, exitCode)
}

// ============================================================================
// New Solution Command Tests
// ============================================================================

func TestIntegration_NewSolution_Basic(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "test-app", "--description", "A test application")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "test-app")
	assert.Contains(t, stdout, "A test application")
}

func TestIntegration_NewSolution_WithFeatures(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "my-deploy", "--description", "Deploy to K8s",
		"--features", "parameters,resolvers,actions")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-deploy")
	assert.Contains(t, stdout, "resolvers")
}

func TestIntegration_NewSolution_WithProviders(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "new", "solution", "-n", "my-deploy", "--description", "Deploy",
		"--providers", "exec,http")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "my-deploy")
}

func TestIntegration_NewSolution_ToFile(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "file-test", "--description", "Written to file", "-o", outFile)
	assert.Equal(t, 0, exitCode)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "file-test")
}

func TestIntegration_NewSolution_MissingName(t *testing.T) {
	_, _, exitCode := runScafctl(t, "new", "solution", "--description", "Missing name")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_NewSolution_InvalidFeature(t *testing.T) {
	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "test", "--description", "Test", "--features", "invalid-feature")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_NewSolution_ScaffoldPassesLint(t *testing.T) {
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
	// Scaffold with defaults, then run functional tests — all must pass (including builtins).
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "solution.yaml")

	_, _, exitCode := runScafctl(t, "new", "solution", "-n", "func-test", "--description", "Functional test check", "-o", outFile)
	require.Equal(t, 0, exitCode)

	stdout, stderr, exitCode := runScafctl(t, "test", "functional", "-f", outFile, "--no-color")
	assert.Equal(t, 0, exitCode, "functional tests should pass: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "0 failed")
}

// ============================================================================
// Lint Rules/Explain Command Tests
// ============================================================================

func TestIntegration_LintRules_List(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "lint", "rules")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "RULE")
	assert.Contains(t, stdout, "SEVERITY")
}

func TestIntegration_LintRules_FilterSeverity(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "lint", "rules", "--severity", "error")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "error")
	assert.NotContains(t, stdout, "warning")
}

func TestIntegration_LintRules_JSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "lint", "rules", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var rules []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &rules))
	assert.Greater(t, len(rules), 0)
	assert.Contains(t, rules[0], "rule")
	assert.Contains(t, rules[0], "severity")
}

func TestIntegration_LintExplain_KnownRule(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "lint", "explain", "missing-description")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "missing-description")
	assert.Contains(t, stdout, "Severity")
}

func TestIntegration_LintExplain_UnknownRule(t *testing.T) {
	_, _, exitCode := runScafctl(t, "lint", "explain", "nonexistent-rule")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_LintExplain_JSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "lint", "explain", "missing-description", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	assert.Equal(t, "missing-description", result["rule"])
}

// ============================================================================
// Examples Command Tests (Sprint 5)
// ============================================================================

func TestIntegration_Examples_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "examples", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "get")
}

func TestIntegration_Examples_List(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "examples", "list", "-o", "yaml")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "path:")
	assert.Contains(t, stdout, "category:")
	assert.Contains(t, stdout, "description:")
}

func TestIntegration_Examples_List_JSON(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "examples", "list", "-o", "json")
	assert.Equal(t, 0, exitCode)

	var examples []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &examples))
	assert.Greater(t, len(examples), 0)
	assert.Contains(t, examples[0], "path")
	assert.Contains(t, examples[0], "category")
}

func TestIntegration_Examples_List_FilterCategory(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "examples", "list", "--category", "solutions")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "solutions")
}

func TestIntegration_Examples_Get(t *testing.T) {
	t.Parallel()
	// Get a known example — resolver-demo.yaml is a top-level example
	stdout, _, exitCode := runScafctl(t, "examples", "get", "resolver-demo.yaml")
	assert.Equal(t, 0, exitCode)
	assert.NotEmpty(t, stdout)
}

func TestIntegration_Examples_Get_NotFound(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "examples", "get", "nonexistent-example.yaml")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_Examples_Get_OutputFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "output.yaml")

	_, _, exitCode := runScafctl(t, "examples", "get", "resolver-demo.yaml", "-o", outFile)
	assert.Equal(t, 0, exitCode)

	// Verify the file was written
	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
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
	// Enhanced dry-run includes solution info and action plan
	assert.Contains(t, stdout, "DRY RUN")
	assert.Contains(t, stdout, "ACTION PLAN")
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
}

// ============================================================================
// Output Directory Tests
// ============================================================================

func TestIntegration_RunSolution_OutputDir_ActionsWriteToOutputDir(t *testing.T) {
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

	// Should contain dry-run output
	assert.Contains(t, stdout, "DRY RUN")
}

func TestIntegration_RunResolver_OutputDir_NoEffect(t *testing.T) {
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
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "plugins", "list")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No plugins cached")
}

// TestIntegration_Plugins_List_JSON verifies plugins list supports JSON output.
func TestIntegration_Plugins_List_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "plugins", "list", "-o", "json")
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
	assert.Contains(t, stderr, "no solution path provided")
}

// TestIntegration_Plugins_Install_NoPlugins verifies install succeeds with a solution that has no plugins.
func TestIntegration_Plugins_Install_NoPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Use an existing simple solution file that has no plugin dependencies
	stdout, stderr, exitCode := runScafctl(t, "plugins", "install", "-f", "examples/resolver-demo.yaml")
	_ = stderr
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No plugins declared")
}

// TestIntegration_Plugins_Install_AutoDiscovery verifies auto-discovery of solution file.
func TestIntegration_Plugins_Install_AutoDiscovery(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
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

	stdout, _, exitCode := runScafctlInDir(t, tmpDir, "plugins", "install")
	assert.Equal(t, 0, exitCode, "expected plugins install to auto-discover solution.yaml")
	assert.Contains(t, stdout, "No plugins declared")
}

// TestIntegration_RunResolver_MetadataProvider runs the metadata provider and verifies output.
func TestIntegration_RunResolver_MetadataProvider(t *testing.T) {
	t.Parallel()
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

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.meta", "-o", "json", "--hide-execution")
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

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.slugified", "-o", "json", "--hide-execution")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "my-cool-project")
}

// TestIntegration_RunResolver_TemplateFunctions_CelInline verifies inline cel template function.
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

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.count", "-o", "json", "--hide-execution")
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

	stdout, stderr, exitCode := runScafctl(t, "run", "resolver", "-f", solutionFile, "-e", "_.names", "-o", "json", "--hide-execution")
	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)
	assert.Contains(t, stdout, "api")
	assert.Contains(t, stdout, "legacy")
}

func TestIntegration_RunResolver_PositionalParams(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"run", "resolver",
		"-f", "examples/resolvers/parameters.yaml",
		"-o", "json",
		"--hide-execution",
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
		"--hide-execution",
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
		"--hide-execution",
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "show", snapshotFile)
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "show", snapshotFile, "--format", "json")
	assert.Equal(t, 0, exitCode)

	// Verify valid JSON
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(stdout), &parsed)
	assert.NoError(t, err, "snapshot show --format json should produce valid JSON")
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

	// Show resolvers format
	stdout, _, exitCode := runScafctl(t, "snapshot", "show", snapshotFile, "--format", "resolvers")
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "show", snapshotFile, "--format", "resolvers", "--verbose")
	assert.Equal(t, 0, exitCode)
	// Verbose should show values
	assert.Contains(t, stdout, "Value:")
}

func TestIntegration_Snapshot_Show_MissingFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "snapshot", "show", "/nonexistent/path/snapshot.json")
	assert.NotEqual(t, 0, exitCode, "should fail when snapshot file does not exist")
}

func TestIntegration_Snapshot_Show_NoArgs(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "snapshot", "show")
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "diff", beforeFile, afterFile)
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "diff", beforeFile, afterFile, "--format", "json")
	assert.Equal(t, 0, exitCode)

	// Should be valid JSON
	var parsed map[string]interface{}
	err := json.Unmarshal([]byte(stdout), &parsed)
	assert.NoError(t, err, "diff --format json should produce valid JSON")
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "diff", beforeFile, afterFile, "--format", "unified")
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
	stdout, _, exitCode := runScafctl(t, "snapshot", "diff", beforeFile, afterFile, "--ignore-unchanged")
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
		"snapshot", "diff", beforeFile, afterFile,
		"--ignore-fields", "duration,providerCalls",
	)
	assert.Equal(t, 0, exitCode)
	t.Logf("ignore-fields diff output: %s", stdout)
}

func TestIntegration_Snapshot_Diff_OutputFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	beforeFile := filepath.Join(tmpDir, "before.json")
	afterFile := filepath.Join(tmpDir, "after.json")
	outputFile := filepath.Join(tmpDir, "diff-output.txt")

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

	// Write diff to file
	_, _, exitCode = runScafctl(t,
		"snapshot", "diff", beforeFile, afterFile,
		"--output", outputFile,
	)
	assert.Equal(t, 0, exitCode)

	// Verify output file was created
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	assert.NotEmpty(t, data, "diff output file should not be empty")
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
		"snapshot", "diff", snapshotFile, "/nonexistent/after.json",
	)
	assert.NotEqual(t, 0, exitCode, "should fail when snapshot file does not exist")
}

func TestIntegration_Snapshot_Diff_NoArgs(t *testing.T) {
	t.Parallel()
	_, stderr, exitCode := runScafctl(t, "snapshot", "diff")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "accepts 2 arg")
}

func TestIntegration_Snapshot_Help(t *testing.T) {
	t.Parallel()
	stdout, _, exitCode := runScafctl(t, "snapshot", "--help")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "show")
	assert.Contains(t, stdout, "diff")
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
		"solution", "diff",
		"examples/soldiff/solution-v1.yaml",
		"examples/soldiff/solution-v2.yaml",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Solution Diff:")
	assert.Contains(t, stdout, "metadata.version")
	assert.Contains(t, stdout, "Summary:")
}

func TestIntegration_SolutionDiff_JSON(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"solution", "diff",
		"examples/soldiff/solution-v1.yaml",
		"examples/soldiff/solution-v2.yaml",
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
		"solution", "diff",
		"examples/soldiff/solution-v1.yaml",
		"examples/soldiff/solution-v2.yaml",
		"-o", "yaml",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "pathA:")
	assert.Contains(t, stdout, "changes:")
}

func TestIntegration_CacheInfo_ShowsArtifactCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Build a solution to populate the artifact cache
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Cache info should report artifact data
	stdout, _, exitCode2 := runScafctl(t, "cache", "info")
	assert.Equal(t, 0, exitCode2)
	// Verify the cache info output contains expected sections
	assert.Contains(t, stdout, "Cache")
}

func TestIntegration_SolutionDiff_MissingFile(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t,
		"solution", "diff",
		"examples/soldiff/solution-v1.yaml",
		"/nonexistent/solution.yaml",
	)
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_SolutionDiff_NoArgs(t *testing.T) {
	t.Parallel()
	_, _, exitCode := runScafctl(t, "solution", "diff")
	assert.NotEqual(t, 0, exitCode)
}

func TestIntegration_SolutionDiff_Alias(t *testing.T) {
	t.Parallel()
	stdout, stderr, exitCode := runScafctl(t,
		"sol", "diff",
		"examples/soldiff/solution-v1.yaml",
		"examples/soldiff/solution-v2.yaml",
	)
	assert.Equal(t, 0, exitCode, "stderr: %s", stderr)
	assert.Contains(t, stdout, "Solution Diff:")
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
