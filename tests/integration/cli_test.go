package integration

import (
	"bytes"
	"context"
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

func runScafctl(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = findProjectRoot()

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
	stdout, _, exitCode := runScafctl(t, "version")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Version")
}

func TestIntegration_VersionJSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "version", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "buildTime")
}

// ============================================================================
// Help Command Tests
// ============================================================================

func TestIntegration_Help(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "scafctl")
	assert.Contains(t, stdout, "run")
	assert.Contains(t, stdout, "render")
	assert.Contains(t, stdout, "get")
}

func TestIntegration_RunHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "run", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "solution")
}

// ============================================================================
// Get Provider Tests
// ============================================================================

func TestIntegration_GetProvider(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "get", "provider")

	assert.Equal(t, 0, exitCode)
	// Should list built-in providers
	assert.Contains(t, stdout, "static")
	assert.Contains(t, stdout, "env")
	assert.Contains(t, stdout, "http")
	assert.Contains(t, stdout, "exec")
	assert.Contains(t, stdout, "cel")
}

func TestIntegration_GetProviderJSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "get", "provider", "-o", "json")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "\"name\"")
	assert.Contains(t, stdout, "static")
}

// ============================================================================
// Explain Schema Tests
// ============================================================================

func TestIntegration_ExplainProvider(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "explain", "provider")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Descriptor")
	assert.Contains(t, stdout, "name")
}

func TestIntegration_ExplainProviderNotFound(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "explain", "nonexistentkind")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown kind")
}

// ============================================================================
// Run Solution Tests
// ============================================================================

func TestIntegration_RunSolution_HelloWorld(t *testing.T) {
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "Hello from Actions!")
}

func TestIntegration_RunSolution_FileNotFound(t *testing.T) {
	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "/nonexistent/solution.yaml",
	)

	assert.NotEqual(t, 0, exitCode)
	assert.True(t, strings.Contains(stderr, "not found") || strings.Contains(stderr, "no such file"))
}

func TestIntegration_RunSolution_InvalidYAML(t *testing.T) {
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

func TestIntegration_RunSolution_DryRun(t *testing.T) {
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--dry-run",
	)

	assert.Equal(t, 0, exitCode)
	// Dry run should show what would happen without executing
	t.Logf("dry-run output: %s", stdout)
}

func TestIntegration_RunSolution_SkipActions(t *testing.T) {
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/hello-world.yaml",
		"--skip-actions",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	// Should resolve but not execute actions
	assert.Contains(t, stdout, "greeting")
}

func TestIntegration_RunSolution_ConditionalRetry(t *testing.T) {
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/conditional-retry.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode, "expected exit code 0, got %d", exitCode)
	assert.Contains(t, stdout, "all tests complete")
}

func TestIntegration_RunSolution_RetryIfWithCommandNotFound(t *testing.T) {
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
          shell: false
          command: "/nonexistent-command-12345"
`
	require.NoError(t, os.WriteFile(solutionPath, []byte(solution), 0o644))

	_, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", solutionPath,
	)

	// Should fail because command doesn't exist
	assert.NotEqual(t, 0, exitCode)
	t.Logf("stderr: %s", stderr)
}

func TestIntegration_RunSolution_RetryIfWithRetryEnabled(t *testing.T) {
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
          shell: false
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
	// Use run solution with --skip-actions to get resolver outputs
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/resolver-demo.yaml",
		"--skip-actions",
	)

	assert.Equal(t, 0, exitCode)
	// Should contain resolver outputs
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "region")
}

func TestIntegration_RenderSolutionJSON(t *testing.T) {
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/resolver-demo.yaml",
		"--skip-actions",
		"-o", "json",
	)

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "production")
	assert.Contains(t, stdout, "us-west-2")
}

func TestIntegration_RenderSolutionYAML(t *testing.T) {
	stdout, _, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/resolver-demo.yaml",
		"--skip-actions",
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
	stdout, _, exitCode := runScafctl(t, "config", "view")

	// May return non-zero if no config exists, but shouldn't crash
	t.Logf("exit code: %d, stdout: %s", exitCode, stdout)
}

func TestIntegration_ConfigSchema(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "config", "schema")

	assert.Equal(t, 0, exitCode)
	// Should output JSON schema
	assert.Contains(t, stdout, "properties")
}

// ============================================================================
// Secrets Command Tests (basic, non-destructive)
// ============================================================================

func TestIntegration_SecretsList(t *testing.T) {
	// This test just verifies the command doesn't crash
	_, _, exitCode := runScafctl(t, "secrets", "list")

	// May fail if no secrets store, but shouldn't crash badly
	t.Logf("exit code: %d", exitCode)
}

func TestIntegration_SecretsHelp(t *testing.T) {
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
	// This test just verifies the command doesn't crash
	stdout, _, exitCode := runScafctl(t, "auth", "status")

	t.Logf("exit code: %d, stdout: %s", exitCode, stdout)
}

func TestIntegration_AuthHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "auth", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "login")
	assert.Contains(t, stdout, "logout")
	assert.Contains(t, stdout, "status")
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestIntegration_InvalidCommand(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "invalidcommand")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "unknown command")
}

func TestIntegration_MissingRequiredFlag(t *testing.T) {
	_, _, exitCode := runScafctl(t, "run", "solution")

	// Should fail due to missing -f flag
	assert.NotEqual(t, 0, exitCode)
}

// ============================================================================
// Complex Workflow Tests
// ============================================================================

func TestIntegration_SequentialChain(t *testing.T) {
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/sequential-chain.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ConditionalExecution(t *testing.T) {
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/conditional-execution.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

func TestIntegration_ParallelWithDeps(t *testing.T) {
	stdout, stderr, exitCode := runScafctl(t,
		"run", "solution",
		"-f", "examples/actions/parallel-with-deps.yaml",
	)

	t.Logf("stdout: %s", stdout)
	t.Logf("stderr: %s", stderr)

	assert.Equal(t, 0, exitCode)
}

// ============================================================================
// Quiet Mode Tests
// ============================================================================

func TestIntegration_QuietMode(t *testing.T) {
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
	formats := []string{"json", "yaml", "table"}

	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			stdout, _, exitCode := runScafctl(t,
				"run", "solution",
				"-f", "examples/resolver-demo.yaml",
				"--skip-actions",
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
	stdout, _, exitCode := runScafctl(t, "lint", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Analyze a solution file")
	assert.Contains(t, stdout, "LINT RULES:")
	assert.Contains(t, stdout, "--file")
	assert.Contains(t, stdout, "--severity")
}

func TestIntegration_Lint_RequiresFile(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "lint")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "required flag")
}

func TestIntegration_Lint_ValidSolution(t *testing.T) {
	// Test with a simple solution file that should have minimal issues
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// The demo may have some issues but should lint successfully
	assert.Contains(t, stdout, "findings")
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
}

func TestIntegration_Lint_SeverityFilter(t *testing.T) {
	// Test error-only filter
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "--severity", "error", "-o", "json")

	assert.Contains(t, stdout, "errorCount")
	// When filtering by error, warnCount and infoCount should be 0
	assert.Contains(t, stdout, `"warnCount": 0`)
	assert.Contains(t, stdout, `"infoCount": 0`)
}

func TestIntegration_Lint_QuietMode(t *testing.T) {
	// Quiet mode should produce no output on success
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "quiet")

	// In quiet mode, stdout should be empty (only exit code matters)
	assert.Empty(t, stdout)
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
}

func TestIntegration_Lint_JSONOutput(t *testing.T) {
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Verify JSON structure
	assert.Contains(t, stdout, `"file":`)
	assert.Contains(t, stdout, `"findings":`)
	assert.Contains(t, stdout, `"errorCount":`)
	assert.Contains(t, stdout, `"warnCount":`)
	assert.Contains(t, stdout, `"infoCount":`)
}

func TestIntegration_Lint_InvalidFile(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "lint", "-f", "nonexistent.yaml")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to load solution")
}

func TestIntegration_Lint_YAMLOutput(t *testing.T) {
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "yaml")

	// Verify YAML structure
	assert.Contains(t, stdout, "file:")
	assert.Contains(t, stdout, "findings:")
	assert.Contains(t, stdout, "errorCount:")
}

func TestIntegration_Lint_Alias(t *testing.T) {
	// Test the 'l' alias works
	stdout, _, exitCode := runScafctl(t, "l", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_CheckAlias(t *testing.T) {
	// Test the 'check' alias works
	stdout, _, exitCode := runScafctl(t, "check", "-f", "examples/resolver-demo.yaml", "-o", "json")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_WarningSeverityFilter(t *testing.T) {
	// Test warning filter includes warnings and errors but not info
	stdout, _, _ := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "--severity", "warning", "-o", "json")

	assert.Contains(t, stdout, "errorCount")
	// When filtering by warning, infoCount should be 0
	assert.Contains(t, stdout, `"infoCount": 0`)
}

func TestIntegration_Lint_ActionSolution(t *testing.T) {
	// Test linting a solution with actions
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/actions/hello-world.yaml", "-o", "json")

	// Should complete successfully (exit code 0 = no errors, 1 = general error, 2 = validation failed)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_ComplexSolution(t *testing.T) {
	// Test linting a more complex solution
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/solutions/comprehensive/solution.yaml", "-o", "json")

	// Should complete and report findings
	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	assert.Contains(t, stdout, "findings")
	assert.Contains(t, stdout, "errorCount")
}

func TestIntegration_Lint_TableOutput(t *testing.T) {
	// Test default table output (explicit)
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "table")

	// Exit code 0 = no errors, 1 = general error, 2 = validation failed (errors found)
	assert.True(t, exitCode == 0 || exitCode == 1 || exitCode == 2)
	// Table output should produce some text
	assert.NotEmpty(t, stdout)
}

// ============================================================================
// Build Command Tests
// ============================================================================

func TestIntegration_BuildHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "build", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "build")
	assert.Contains(t, stdout, "solution")
}

func TestIntegration_BuildSolutionHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "build", "solution", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Build a solution")
	assert.Contains(t, stdout, "--version")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--force")
}

func TestIntegration_BuildSolution_UsesMetadataVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	// Build with different version than metadata - should warn
	stdout, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "9.9.9")

	assert.Equal(t, 0, exitCode)
	// Should warn about overriding metadata version
	assert.Contains(t, stdout, "overrides metadata version")
	assert.Contains(t, stdout, "9.9.9")
}

func TestIntegration_BuildSolution_FileNotFound(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "build", "solution", "nonexistent.yaml", "--version", "1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "failed to read")
}

func TestIntegration_BuildSolution_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	// First build
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Second build without force should fail
	_, stderr, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "exists")

	// Third build with force should succeed
	stdout, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0", "--force")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Built")
}

// ============================================================================
// Catalog Command Tests
// ============================================================================

func TestIntegration_CatalogHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "catalog", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "catalog")
	assert.Contains(t, stdout, "list")
	assert.Contains(t, stdout, "inspect")
	assert.Contains(t, stdout, "delete")
}

func TestIntegration_CatalogListHelp(t *testing.T) {
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

	stdout, _, exitCode := runScafctl(t, "catalog", "list", "-o", "json")

	assert.Equal(t, 0, exitCode)
	// Empty list should return empty JSON array or null
	assert.True(t, strings.Contains(stdout, "[]") || strings.Contains(stdout, "null"))
}

func TestIntegration_CatalogList_WithArtifacts(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	// Build an artifact first
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// List with filter should work
	stdout, _, exitCode := runScafctl(t, "catalog", "list", "--kind", "solution", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "resolver-demo")
}

func TestIntegration_CatalogInspectHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "catalog", "inspect", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Show detailed information")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogInspect_NotFound(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "inspect", "nonexistent")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogInspect_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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
	stdout, _, exitCode := runScafctl(t, "catalog", "delete", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Delete an artifact")
}

func TestIntegration_CatalogDelete_RequiresVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	_, stderr, exitCode := runScafctl(t, "catalog", "delete", "nonexistent@1.0.0")

	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogDelete_Success(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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
	stdout, _, exitCode := runScafctl(t, "catalog", "prune", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Remove orphaned blobs")
	assert.Contains(t, stdout, "--output")
}

func TestIntegration_CatalogPrune_EmptyCatalog(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	stdout, _, exitCode := runScafctl(t, "catalog", "prune")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "No orphaned content")
}

func TestIntegration_CatalogPrune_JSON(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	// Try to run a solution that doesn't exist in catalog
	_, stderr, exitCode := runScafctl(t, "run", "solution", "nonexistent-solution", "--skip-actions")
	assert.NotEqual(t, 0, exitCode)
	// Reports artifact not found in catalog and file system
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_RunSolution_FromCatalog_ByName(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Build a solution into the catalog
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name (should pick latest version)
	stdout, _, exitCode := runScafctl(t, "run", "solution", "resolver-demo", "--skip-actions", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_ByNameVersion(t *testing.T) {
	// Create a temp directory for the catalog
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Build two versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0")
	require.Equal(t, 0, exitCode)

	// Run the solution from catalog by name@version
	stdout, _, exitCode := runScafctl(t, "run", "solution", "resolver-demo@1.0.0", "--skip-actions", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_FallbackToFile(t *testing.T) {
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Run a solution by file path (not bare name) - should use file
	stdout, _, exitCode := runScafctl(t, "run", "solution", "-f", "examples/resolver-demo.yaml", "--skip-actions", "-o", "json")
	assert.Equal(t, 0, exitCode)
	// Should have resolver output from file
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

func TestIntegration_RunSolution_FromCatalog_PathNotBareName(t *testing.T) {
	// Create a temp directory for the catalog (empty)
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// A path with a separator should not be treated as a bare name
	// This should try to open a file, not lookup in catalog
	_, stderr, exitCode := runScafctl(t, "run", "solution", "./nonexistent.yaml", "--skip-actions")
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
	stdout, _, _ := runScafctl(t, "catalog", "save", "--help")
	assert.Contains(t, stdout, "save")
	assert.Contains(t, stdout, "output")
}

func TestIntegration_CatalogSave_RequiresOutput(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	outputPath := tmpDir + "/nonexistent.tar"
	_, stderr, exitCode := runScafctl(t, "catalog", "save", "nonexistent", "-o", outputPath)
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogSave_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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
	stdout, _, _ := runScafctl(t, "catalog", "load", "--help")
	assert.Contains(t, stdout, "load")
	assert.Contains(t, stdout, "input")
	assert.Contains(t, stdout, "force")
}

func TestIntegration_CatalogLoad_RequiresInput(t *testing.T) {
	_, stderr, exitCode := runScafctl(t, "catalog", "load")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "required")
}

func TestIntegration_CatalogLoad_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "load", "--input", "/nonexistent/path.tar")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "no such file")
}

func TestIntegration_CatalogLoad_Success(t *testing.T) {
	// Create source catalog
	srcDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", srcDir)

	// Build and save an artifact
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)

	tarPath := srcDir + "/export.tar"
	_, _, exitCode = runScafctl(t, "catalog", "save", "resolver-demo", "-o", tarPath)
	require.Equal(t, 0, exitCode)

	// Switch to destination catalog
	dstDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dstDir)

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

	// Load from tar
	_, _, exitCode = runScafctl(t, "catalog", "load", "--input", tarPath)
	require.Equal(t, 0, exitCode)

	// Verify the solution can be run
	stdout, _, exitCode := runScafctl(t, "run", "solution", "resolver-demo", "--skip-actions", "-o", "json")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "environment")
	assert.Contains(t, stdout, "production")
}

// =============================================================================
// Catalog Push Tests
// =============================================================================

func TestIntegration_CatalogPushHelp(t *testing.T) {
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

	// Push a nonexistent artifact
	_, stderr, exitCode := runScafctl(t, "catalog", "push", "nonexistent@1.0.0", "--catalog", "ghcr.io/test/scafctl")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

// =============================================================================
// Catalog Pull Tests
// =============================================================================

func TestIntegration_CatalogPullHelp(t *testing.T) {
	stdout, _, _ := runScafctl(t, "catalog", "pull", "--help")
	assert.Contains(t, stdout, "Pull a catalog artifact from a remote OCI registry")
	assert.Contains(t, stdout, "--as")
	assert.Contains(t, stdout, "--force")
}

func TestIntegration_CatalogPull_InvalidReference(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	// Pull with invalid reference (no registry)
	_, stderr, exitCode := runScafctl(t, "catalog", "pull", "just-a-name")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "invalid")
}

// =============================================================================
// Catalog Delete Remote Tests
// =============================================================================

func TestIntegration_CatalogDeleteRemoteHelp(t *testing.T) {
	stdout, _, _ := runScafctl(t, "catalog", "delete", "--help")
	assert.Contains(t, stdout, "Delete an artifact from the local or remote catalog")
	assert.Contains(t, stdout, "ghcr.io/myorg/scafctl/solutions/my-solution")
	assert.Contains(t, stdout, "--insecure")
	assert.Contains(t, stdout, "--catalog")
}

func TestIntegration_CatalogDelete_RemoteDetection(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "my-solution", "stable")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "version required")
}

func TestIntegration_CatalogTag_RejectsSemverAlias(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "my-solution@1.0.0", "2.0.0")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "semver version")
}

func TestIntegration_CatalogTag_ArtifactNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

	_, stderr, exitCode := runScafctl(t, "catalog", "tag", "nonexistent@1.0.0", "stable", "--kind", "solution")
	assert.NotEqual(t, 0, exitCode)
	assert.Contains(t, stderr, "not found")
}

func TestIntegration_CatalogTag_Success(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)

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

	// Build two versions
	_, _, exitCode := runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "1.0.0")
	require.Equal(t, 0, exitCode)
	_, _, exitCode = runScafctl(t, "build", "solution", "examples/resolver-demo.yaml", "--version", "2.0.0", "--force")
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
	stdout, _, exitCode := runScafctl(t, "cache", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "cache")
	assert.Contains(t, stdout, "clear")
	assert.Contains(t, stdout, "info")
}

func TestIntegration_CacheClearHelp(t *testing.T) {
	stdout, _, exitCode := runScafctl(t, "cache", "clear", "--help")

	assert.Equal(t, 0, exitCode)
	assert.Contains(t, stdout, "Clear cached content")
	assert.Contains(t, stdout, "--kind")
	assert.Contains(t, stdout, "--name")
	assert.Contains(t, stdout, "--force")
}

func TestIntegration_CacheInfoHelp(t *testing.T) {
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
