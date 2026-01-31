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
	// Exit code 0 = no errors, 1 = errors found (both are valid lint results)
	assert.True(t, exitCode == 0 || exitCode == 1)
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
	assert.True(t, exitCode == 0 || exitCode == 1)
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

	assert.True(t, exitCode == 0 || exitCode == 1)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_CheckAlias(t *testing.T) {
	// Test the 'check' alias works
	stdout, _, exitCode := runScafctl(t, "check", "-f", "examples/resolver-demo.yaml", "-o", "json")

	assert.True(t, exitCode == 0 || exitCode == 1)
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

	// Should complete successfully (exit code 0 or 1 depending on findings)
	assert.True(t, exitCode == 0 || exitCode == 1)
	assert.Contains(t, stdout, "findings")
}

func TestIntegration_Lint_ComplexSolution(t *testing.T) {
	// Test linting a more complex solution
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/solutions/comprehensive/solution.yaml", "-o", "json")

	// Should complete and report findings
	assert.True(t, exitCode == 0 || exitCode == 1)
	assert.Contains(t, stdout, "findings")
	assert.Contains(t, stdout, "errorCount")
}

func TestIntegration_Lint_TableOutput(t *testing.T) {
	// Test default table output (explicit)
	stdout, _, exitCode := runScafctl(t, "lint", "-f", "examples/resolver-demo.yaml", "-o", "table")

	assert.True(t, exitCode == 0 || exitCode == 1)
	// Table output should produce some text
	assert.NotEmpty(t, stdout)
}
