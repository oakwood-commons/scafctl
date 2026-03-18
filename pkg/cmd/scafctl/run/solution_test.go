// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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

	assert.Equal(t, "solution [name[@version]]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify flags exist
	flags := cmd.Flags()
	assert.NotNil(t, flags.Lookup("file"))
	assert.NotNil(t, flags.Lookup("resolver"))
	assert.NotNil(t, flags.Lookup("output"))
	assert.NotNil(t, flags.Lookup("resolve-all"))
	assert.NotNil(t, flags.Lookup("progress"))
	assert.NotNil(t, flags.Lookup("warn-value-size"))
	assert.NotNil(t, flags.Lookup("max-value-size"))
	assert.NotNil(t, flags.Lookup("resolver-timeout"))
	assert.NotNil(t, flags.Lookup("phase-timeout"))
	assert.NotNil(t, flags.Lookup("show-execution"))
	assert.NotNil(t, flags.Lookup("on-conflict"))
	assert.NotNil(t, flags.Lookup("backup"))
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
	assert.Equal(t, "auto", output) // Changed from "json" to "auto" for kvx integration

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

	showExecution, err := flags.GetBool("show-execution")
	require.NoError(t, err)
	assert.False(t, showExecution)

	onConflict, err := flags.GetString("on-conflict")
	require.NoError(t, err)
	assert.Empty(t, onConflict)

	backup, err := flags.GetBool("backup")
	require.NoError(t, err)
	assert.False(t, backup)

	outputDir, err := flags.GetString("output-dir")
	require.NoError(t, err)
	assert.Empty(t, outputDir)
}

func TestSolutionOptions_getEffectiveActionConfig_OutputDir(t *testing.T) {
	t.Parallel()

	t.Run("config OutputDir used when flag not set", func(t *testing.T) {
		t.Parallel()
		appCfg := &config.Config{
			Action: config.ActionConfig{
				OutputDir: "/from/config",
			},
		}
		ctx := config.WithConfig(context.Background(), appCfg)

		opts := &SolutionOptions{}
		opts.flagsChanged = map[string]bool{}

		result := opts.getEffectiveActionConfig(ctx)
		assert.Equal(t, "/from/config", result.OutputDir)
	})

	t.Run("CLI flag set skips config OutputDir", func(t *testing.T) {
		t.Parallel()
		appCfg := &config.Config{
			Action: config.ActionConfig{
				OutputDir: "/from/config",
			},
		}
		ctx := config.WithConfig(context.Background(), appCfg)

		opts := &SolutionOptions{}
		opts.OutputDir = "/from/flag"
		opts.flagsChanged = map[string]bool{"output-dir": true}

		result := opts.getEffectiveActionConfig(ctx)
		// When flag is explicitly set, getEffectiveActionConfig does NOT
		// override with config — OutputDir stays at zero value because:
		// - result is initialized without OutputDir
		// - config override is skipped for changed flags
		// The actual CLI value (o.OutputDir) is used directly in Run().
		assert.Empty(t, result.OutputDir)
	})

	t.Run("empty config OutputDir returns empty", func(t *testing.T) {
		t.Parallel()
		appCfg := &config.Config{}
		ctx := config.WithConfig(context.Background(), appCfg)

		opts := &SolutionOptions{}
		opts.flagsChanged = map[string]bool{}

		result := opts.getEffectiveActionConfig(ctx)
		assert.Empty(t, result.OutputDir)
	})
}

func TestSolutionOptions_Run_InvalidOnConflict(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams: streams,
			CliParams: cliParams,
			File:      "./testdata/basic.yaml",
		},
		OnConflict: "invalid-strategy",
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --on-conflict value")
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams: streams,
			CliParams: cliParams,
			File:      "", // No file specified
		},
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams: streams,
			CliParams: cliParams,
			File:      "/nonexistent/solution.yaml",
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestSolutionOptions_Run_EmptySolution(t *testing.T) {
	t.Parallel()

	// Create a solution file with no resolvers and no workflow
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:      streams,
			CliParams:      cliParams,
			File:           solutionPath,
			KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
			registry:       testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
	assert.Contains(t, err.Error(), "scafctl run resolver")
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

			size := execute.CalculateValueSize(tt.value)
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

func TestSolutionOptions_Run_StdinInput_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            "-", // stdin indicator
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
	assert.Contains(t, err.Error(), "scafctl run resolver")
}

func TestSolutionOptions_Run_YAMLOutput_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "yaml"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_Run_QuietOutput_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "quiet"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

// testRegistryWithParameters creates a registry with static and parameter providers
func testRegistryWithParameters() *provider.Registry {
	reg := provider.NewRegistry()
	_ = reg.Register(staticprovider.New())
	_ = reg.Register(parameterprovider.NewParameterProvider())
	return reg
}

func TestSolutionOptions_Run_ParameterFromFile_NoWorkflow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create parameter file
	paramsPath := filepath.Join(tmpDir, "params.yaml")
	paramsContent := `environment: production
region: us-west-2
`
	err := os.WriteFile(paramsPath, []byte(paramsContent), 0o600)
	require.NoError(t, err)

	// Create solution that uses parameter provider to access CLI params but has no workflow
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverParams:  []string{"@" + paramsPath},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithParameters(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_Run_ParameterKeyValue_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverParams:  []string{"app_name=my-application"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistryWithParameters(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_Run_SensitiveRedaction_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_Run_MaxValueSizeExceeded_NoWorkflow(t *testing.T) {
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
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			MaxValueSize:    10, // Very small limit to trigger error
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	// Should error about no workflow, not about max value size
	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_Run_InvalidOutputFormat(t *testing.T) {
	t.Parallel()

	// Use a solution with a workflow so we don't hit "no workflow" error first
	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: invalid-output-test
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      greet:
        use: static
        inputs:
          value: hello
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	cmd := CommandSolution(cliParams, streams, "")
	cmd.SetArgs([]string{"-f", solutionPath, "-o", "invalid"})

	// Invalid output format should produce an error
	err = cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

func TestSolutionOptions_Run_ShowExecution_NoWorkflow(t *testing.T) {
	// Solutions without workflows should error even with --show-execution
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: show-execution-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &bytes.Buffer{},
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		ShowExecution: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
	assert.Contains(t, err.Error(), "scafctl run resolver")
}

func TestSolutionOptions_Run_NoShowExecution_NoWorkflow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: no-show-execution-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &bytes.Buffer{},
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		ShowExecution: false,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no workflow defined")
}

func TestSolutionOptions_resolveOutputDir(t *testing.T) {
	t.Parallel()

	t.Run("empty output dir returns empty string", func(t *testing.T) {
		t.Parallel()
		opts := &SolutionOptions{}
		result, err := opts.resolveOutputDir(context.Background(), false)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("resolves to absolute path and creates directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "new-output")

		opts := &SolutionOptions{}
		opts.OutputDir = target
		result, err := opts.resolveOutputDir(context.Background(), false)
		require.NoError(t, err)
		assert.Equal(t, target, result)

		info, statErr := os.Stat(target)
		require.NoError(t, statErr)
		assert.True(t, info.IsDir())
	})

	t.Run("dry run resolves path without creating directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "dryrun-no-create")

		opts := &SolutionOptions{}
		opts.OutputDir = target
		result, err := opts.resolveOutputDir(context.Background(), true)
		require.NoError(t, err)
		assert.Equal(t, target, result)

		_, statErr := os.Stat(target)
		assert.True(t, os.IsNotExist(statErr), "directory should not be created in dry-run mode")
	})
}

func TestSolutionOptions_Run_VerboseWithoutDryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: verbose-test
  version: 1.0.0
spec:
  resolvers:
    val:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
  workflow:
    actions:
      greet:
        provider: static
        inputs:
          value: world
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &bytes.Buffer{},
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		Verbose: true, // --verbose without --dry-run
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)
	w := writer.New(streams, cliParams)
	ctx = writer.WithWriter(ctx, w)

	// Run may error (action timeout etc) — we only care about the warning
	_ = opts.Run(ctx)

	output := stdout.String()
	assert.Contains(t, output, "--verbose has no effect without --dry-run")
}

func BenchmarkSolutionOptions_resolveOutputDir(b *testing.B) {
	tmpDir := b.TempDir()
	target := filepath.Join(tmpDir, "bench-output")
	opts := &SolutionOptions{}
	opts.OutputDir = target

	// Create once so the benchmark measures the resolve path, not mkdir
	_ = os.MkdirAll(target, 0o755)
	b.ResetTimer()
	for b.Loop() {
		_, _ = opts.resolveOutputDir(context.Background(), false)
	}
}
