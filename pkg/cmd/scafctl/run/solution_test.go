package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistry creates a registry with static provider for CLI tests
func testRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	_ = reg.Register(staticprovider.New())
	return reg
}

func TestCommandSolution(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandSolution(cliParams, streams, "")

	assert.Equal(t, "solution", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify flags exist
	flags := cmd.Flags()
	assert.NotNil(t, flags.Lookup("file"))
	assert.NotNil(t, flags.Lookup("resolver"))
	assert.NotNil(t, flags.Lookup("output"))
	assert.NotNil(t, flags.Lookup("only"))
	assert.NotNil(t, flags.Lookup("resolve-all"))
	assert.NotNil(t, flags.Lookup("progress"))
	assert.NotNil(t, flags.Lookup("warn-value-size"))
	assert.NotNil(t, flags.Lookup("max-value-size"))
	assert.NotNil(t, flags.Lookup("resolver-timeout"))
	assert.NotNil(t, flags.Lookup("phase-timeout"))
}

func TestCommandSolution_FlagDefaults(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandSolution(cliParams, streams, "")
	flags := cmd.Flags()

	// Check defaults
	file, err := flags.GetString("file")
	require.NoError(t, err)
	assert.Empty(t, file)

	output, err := flags.GetString("output")
	require.NoError(t, err)
	assert.Equal(t, "table", output) // Changed from "json" to "table" for kvx integration

	interactive, err := flags.GetBool("interactive")
	require.NoError(t, err)
	assert.False(t, interactive)

	expression, err := flags.GetString("expression")
	require.NoError(t, err)
	assert.Empty(t, expression)

	progress, err := flags.GetBool("progress")
	require.NoError(t, err)
	assert.False(t, progress)

	resolveAll, err := flags.GetBool("resolve-all")
	require.NoError(t, err)
	assert.False(t, resolveAll)

	resolverTimeout, err := flags.GetDuration("resolver-timeout")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, resolverTimeout)

	phaseTimeout, err := flags.GetDuration("phase-timeout")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, phaseTimeout)
}

func TestSolutionOptions_Run_NoFile(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false // Don't exit on error in tests

	opts := &SolutionOptions{
		IOStreams: streams,
		CliParams: cliParams,
		File:      "", // No file specified
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestSolutionOptions_Run_FileNotFound(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false // Don't exit on error in tests

	opts := &SolutionOptions{
		IOStreams: streams,
		CliParams: cliParams,
		File:      "/nonexistent/solution.yaml",
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestSolutionOptions_Run_EmptySolution(t *testing.T) {
	t.Parallel()

	// Create a solution file with no resolvers
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-solution
  version: 1.0.0
spec:
  resolvers: {}
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false // Don't exit on error in tests

	opts := &SolutionOptions{
		IOStreams: streams,
		CliParams: cliParams,
		File:      solutionPath,
		Output:    "json",
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Should output empty JSON object
	assert.Contains(t, stdout.String(), "{}")
}

func TestCalculateValueSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   any
		minSize int64
		maxSize int64
	}{
		{
			name:    "string",
			value:   "hello",
			minSize: 5,
			maxSize: 10,
		},
		{
			name:    "number",
			value:   42,
			minSize: 1,
			maxSize: 5,
		},
		{
			name:    "empty map",
			value:   map[string]any{},
			minSize: 1,
			maxSize: 5,
		},
		{
			name:    "map with values",
			value:   map[string]any{"key": "value"},
			minSize: 10,
			maxSize: 30,
		},
		{
			name:    "array",
			value:   []any{1, 2, 3},
			minSize: 3,
			maxSize: 15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			size := calculateValueSize(tt.value)
			assert.GreaterOrEqual(t, size, tt.minSize)
			assert.LessOrEqual(t, size, tt.maxSize)
		})
	}
}

func TestValidOutputTypes(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ValidOutputTypes, "json")
	assert.Contains(t, ValidOutputTypes, "yaml")
	assert.Contains(t, ValidOutputTypes, "quiet")
}

func TestExitCodes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, exitcode.Success)
	assert.Equal(t, 1, exitcode.GeneralError)
	assert.Equal(t, 2, exitcode.ValidationFailed)
	assert.Equal(t, 3, exitcode.InvalidInput)
	assert.Equal(t, 4, exitcode.FileNotFound)
	assert.Equal(t, 6, exitcode.ActionFailed)
}

// CLI Scenario Tests - Phase 6 Testing & Validation

func TestSolutionOptions_Run_StdinInput(t *testing.T) {
	t.Parallel()

	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello-from-stdin
`
	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     io.NopCloser(bytes.NewBufferString(solutionContent)),
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            "-", // stdin indicator
		Output:          "json",
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "hello-from-stdin")
}

func TestSolutionOptions_Run_OnlyFlag(t *testing.T) {
	t.Parallel()

	// Create a solution with multiple resolvers
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: only-flag-test
  version: 1.0.0
spec:
  resolvers:
    base:
      resolve:
        with:
          - provider: static
            inputs:
              value: base-value
    dependent:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: base
    independent:
      resolve:
        with:
          - provider: static
            inputs:
              value: independent-value
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "json",
		Only:            "dependent", // Only dependent and its dependency (base)
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Should have base (dependency) and dependent, but not independent
	output := stdout.String()
	assert.Contains(t, output, "base")
	assert.Contains(t, output, "dependent")
	assert.NotContains(t, output, "independent-value")
}

func TestSolutionOptions_Run_OnlyNonexistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: only-nonexistent-test
  version: 1.0.0
spec:
  resolvers:
    existing:
      resolve:
        with:
          - provider: static
            inputs:
              value: existing-value
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams: streams,
		CliParams: cliParams,
		File:      solutionPath,
		Output:    "json",
		Only:      "nonexistent", // This resolver doesn't exist
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Should output empty object since resolver not found
	assert.Contains(t, stdout.String(), "{}")
}

func TestSolutionOptions_Run_YAMLOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: yaml-output-test
  version: 1.0.0
spec:
  resolvers:
    test:
      resolve:
        with:
          - provider: static
            inputs:
              value: yaml-test-value
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "yaml",
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// YAML output should contain the value without JSON braces
	output := stdout.String()
	assert.Contains(t, output, "test:")
	assert.Contains(t, output, "yaml-test-value")
}

func TestSolutionOptions_Run_QuietOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: quiet-output-test
  version: 1.0.0
spec:
  resolvers:
    test:
      resolve:
        with:
          - provider: static
            inputs:
              value: quiet-test-value
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "quiet",
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Quiet output should be empty
	assert.Empty(t, stdout.String())
}

// testRegistryWithParameters creates a registry with static and parameter providers
func testRegistryWithParameters() *provider.Registry {
	reg := provider.NewRegistry()
	_ = reg.Register(staticprovider.New())
	_ = reg.Register(parameterprovider.NewParameterProvider())
	return reg
}

func TestSolutionOptions_Run_ParameterFromFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create parameter file
	paramsPath := filepath.Join(tmpDir, "params.yaml")
	paramsContent := `environment: production
region: us-west-2
`
	err := os.WriteFile(paramsPath, []byte(paramsContent), 0o600)
	require.NoError(t, err)

	// Create solution that uses parameter provider to access CLI params
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: params-file-test
  version: 1.0.0
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
    region:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: region
`
	err = os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "json",
		ResolverParams:  []string{"@" + paramsPath},
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistryWithParameters(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "production")
	assert.Contains(t, output, "us-west-2")
}

func TestSolutionOptions_Run_ParameterKeyValue(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: params-kv-test
  version: 1.0.0
spec:
  resolvers:
    app:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: app_name
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "json",
		ResolverParams:  []string{"app_name=my-application"},
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistryWithParameters(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "my-application")
}

func TestSolutionOptions_Run_SensitiveRedaction(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-test
  version: 1.0.0
spec:
  resolvers:
    secret:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: super-secret-password
    public:
      resolve:
        with:
          - provider: static
            inputs:
              value: public-data
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "json",
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	// Sensitive value should be redacted
	assert.Contains(t, output, "[REDACTED]")
	assert.NotContains(t, output, "super-secret-password")
	// Public value should be visible
	assert.Contains(t, output, "public-data")
}

func TestSolutionOptions_Run_MaxValueSizeExceeded(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: max-size-test
  version: 1.0.0
spec:
  resolvers:
    large:
      resolve:
        with:
          - provider: static
            inputs:
              value: "this-is-a-very-long-value"
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		IOStreams:       streams,
		CliParams:       cliParams,
		File:            solutionPath,
		Output:          "json",
		MaxValueSize:    10, // Very small limit to trigger error
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		registry:        testRegistry(),
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestSolutionOptions_Run_InvalidOutputFormat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: invalid-output-test
  version: 1.0.0
spec:
  resolvers: {}
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	streams, out, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	cmd := CommandSolution(cliParams, streams, "")
	cmd.SetArgs([]string{"-f", solutionPath, "-o", "invalid"})

	// With the shared kvx output handler, invalid formats default to table
	// which falls back to JSON when output is not a TTY.
	// This is better UX than failing on an invalid format.
	err = cmd.Execute()
	require.NoError(t, err)
	// Should produce valid JSON output (fallback from table in non-TTY)
	assert.Contains(t, out.String(), "{")
}
