// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/messageprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/parameterprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/staticprovider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testRegistry creates a registry with built-in static provider for CLI tests
func testRegistry(providers ...provider.Provider) *provider.Registry {
	reg := provider.NewRegistry()
	_ = reg.Register(staticprovider.New())
	for _, p := range providers {
		err := reg.Register(p)
		if err != nil {
			panic(err)
		}
	}
	return reg
}

func newMockProvider(name string) provider.Provider {
	return &provider.MockProvider{
		Name: name,
	}
}

func TestCommandSolution(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandSolution(cliParams, streams, "")

	assert.Equal(t, "solution [name[@version]] [key=value...]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify flags exist
	flags := cmd.Flags()
	assert.NotNil(t, flags.Lookup("file"))
	assert.NotNil(t, flags.ShorthandLookup("a"))
	assert.Equal(t, "action", flags.ShorthandLookup("a").Name)
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
	assert.NotNil(t, flags.Lookup("detailed-exit-code"))
	assert.NotNil(t, flags.Lookup("ci-output"))
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

	ciOutput, err := flags.GetString("ci-output")
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultActionCIOutput, ciOutput)

	backup, err := flags.GetBool("backup")
	require.NoError(t, err)
	assert.False(t, backup)

	outputDir, err := flags.GetString("output-dir")
	require.NoError(t, err)
	assert.Empty(t, outputDir)

	noState, err := flags.GetBool("no-state")
	require.NoError(t, err)
	assert.False(t, noState)
}

func TestCommandSolution_DetailedExitCodeFlag(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()

	t.Run("defaults to false when embedder default unset", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		cmd := CommandSolution(cliParams, streams, "")
		v, err := cmd.Flags().GetBool("detailed-exit-code")
		require.NoError(t, err)
		assert.False(t, v)
	})

	t.Run("flag default reflects embedder default", func(t *testing.T) {
		t.Parallel()
		cliParams := settings.NewCliParams()
		cliParams.DetailedExitCode = true
		cmd := CommandSolution(cliParams, streams, "")
		v, err := cmd.Flags().GetBool("detailed-exit-code")
		require.NoError(t, err)
		assert.True(t, v, "embedder default should seed the flag default")
	})
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

func TestSolutionOptions_Run_InvalidLockMode(t *testing.T) {
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
			LockMode:  "bogus",
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be one of: strict, constrained, bestEffort")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
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

	assert.Contains(t, ValidOutputTypes, "tree")
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
		Verbose: true, // --verbose without --dry-run should not warn
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)
	w := writer.New(streams, cliParams)
	ctx = writer.WithWriter(ctx, w)

	// Run may error (action timeout etc) — we care that no warning is emitted
	_ = opts.Run(ctx)

	output := stdout.String()
	assert.NotContains(t, output, "--verbose has no effect")
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

func TestWriteVerboseActionStatus(t *testing.T) {
	t.Parallel()

	now := time.Now()
	end := now.Add(1 * time.Second)

	tests := []struct {
		name     string
		result   *action.ActionResult
		contains string
	}{
		{
			name: "succeeded",
			result: &action.ActionResult{
				Status:    action.StatusSucceeded,
				StartTime: &now,
				EndTime:   &end,
			},
			contains: "✓ test-action",
		},
		{
			name: "skipped",
			result: &action.ActionResult{
				Status:     action.StatusSkipped,
				SkipReason: action.SkipReasonCondition,
			},
			contains: "○ test-action (skipped: condition)",
		},
		{
			name: "failed",
			result: &action.ActionResult{
				Status: action.StatusFailed,
				Error:  "something broke",
			},
			contains: "✗ test-action (failed: something broke)",
		},
		{
			name: "timeout",
			result: &action.ActionResult{
				Status: action.StatusTimeout,
			},
			contains: "✗ test-action (timeout)",
		},
		{
			name: "cancelled",
			result: &action.ActionResult{
				Status: action.StatusCancelled,
			},
			contains: "○ test-action (cancelled)",
		},
		{
			name: "pending ignored",
			result: &action.ActionResult{
				Status: action.StatusPending,
			},
			contains: "",
		},
		{
			name: "running ignored",
			result: &action.ActionResult{
				Status: action.StatusRunning,
			},
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			streams := &terminal.IOStreams{
				In:     nil,
				Out:    &bytes.Buffer{},
				ErrOut: &stderr,
			}
			cliParams := settings.NewCliParams()
			w := writer.New(streams, cliParams)
			opts := &SolutionOptions{}

			opts.writeVerboseActionStatus(w, "test-action", tt.result)

			if tt.contains == "" {
				assert.Empty(t, stderr.String())
			} else {
				assert.Contains(t, stderr.String(), tt.contains)
			}
		})
	}
}

func BenchmarkWriteVerboseActionStatus(b *testing.B) {
	now := time.Now()
	end := now.Add(1 * time.Second)
	ar := &action.ActionResult{
		Status:    action.StatusSucceeded,
		StartTime: &now,
		EndTime:   &end,
	}

	streams := &terminal.IOStreams{
		In:     nil,
		Out:    io.Discard,
		ErrOut: io.Discard,
	}
	cliParams := settings.NewCliParams()
	w := writer.New(streams, cliParams)
	opts := &SolutionOptions{}

	b.ResetTimer()
	for b.Loop() {
		opts.writeVerboseActionStatus(w, "bench-action", ar)
	}
}

func TestSolutionOptions_Run_ForceOverridesOnConflict(t *testing.T) {
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
			File:      "/nonexistent/solution.yaml",
		},
		Force: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	// Will fail for missing file, but Force should have set OnConflict
	_ = opts.Run(ctx)
	assert.Equal(t, "skip-unchanged", opts.OnConflict)
}

func TestCommandSolution_ForceAndActionFlags(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandSolution(cliParams, streams, "")
	flags := cmd.Flags()

	// Verify --force flag exists with correct default
	forceFlag := flags.Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)

	// Verify --action flag exists
	actionFlag := flags.Lookup("action")
	require.NotNil(t, actionFlag)
}

func TestSolutionOptions_Run_ActionFlagScopesResolversAndResolveAll(t *testing.T) {
	t.Parallel()

	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: solution-action-scoped
  version: 1.0.0
spec:
  resolvers:
    broken:
      resolve:
        with:
          - provider: broken-from
  workflow:
    actions:
      clean:
        provider: message
        inputs:
          message: "echo clean"
      deploy:
        provider: message
        inputs:
          message:
            rslvr: broken
`

	t.Run("--action clean executes 0 resolvers and skips broken resolver", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &SolutionOptions{
			ActionNames:   []string{"clean"},
			ActionTimeout: 10 * time.Second,
			ShowExecution: true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load())
		assert.Contains(t, stdout.String(), `"resolverCount": 0`)
	})

	t.Run("--action clean --resolve-all executes all resolvers and fails on broken", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &SolutionOptions{
			ActionNames:   []string{"clean"},
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				ResolveAll:      true,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		err := opts.Run(ctx)
		require.Error(t, err)
		assert.Equal(t, int32(1), brokenCalls.Load())
	})

	t.Run("--action clean --dry-run skips broken resolver", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		solPath := filepath.Join(tmpDir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		var brokenCalls atomic.Int32
		var stdout, stderr bytes.Buffer
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}

		opts := &SolutionOptions{
			ActionNames:   []string{"clean"},
			ActionTimeout: 10 * time.Second,
			DryRun:        true,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       streams,
				CliParams:       cliParams,
				File:            solPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 10 * time.Second,
				PhaseTimeout:    30 * time.Second,
				registry: actionScopedTestRegistry(
					newCountingFailResolverProvider(&brokenCalls),
				),
			},
		}

		w := writer.New(streams, cliParams)
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), w)
		require.NoError(t, opts.Run(ctx))
		assert.Zero(t, brokenCalls.Load())
	})
}

func TestSolutionOptions_Run_InvalidCIOutput(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &SolutionOptions{
		CIOutput: "invalid-ci",
		sharedResolverOptions: sharedResolverOptions{
			IOStreams: streams,
			CliParams: cliParams,
			File:      "./testdata/basic.yaml",
		},
	}

	lgr := logger.Get(0)
	ctx := writer.WithWriter(logger.WithLogger(context.Background(), lgr), writer.New(streams, cliParams))
	err := opts.Run(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid CI output mode")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestSolutionOptions_getEffectiveActionConfig_CIOutputPrecedence(t *testing.T) {
	t.Parallel()

	appCfg := &config.Config{
		Action: config.ActionConfig{
			CIOutput: "none",
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)

	opts := &SolutionOptions{CIOutput: "auto"}
	opts.flagsChanged = map[string]bool{}
	assert.Equal(t, "none", opts.getEffectiveActionConfig(ctx).CIOutput)

	optsFlag := &SolutionOptions{CIOutput: "github-actions"}
	optsFlag.flagsChanged = map[string]bool{"ci-output": true}
	assert.Equal(t, "github-actions", optsFlag.getEffectiveActionConfig(ctx).CIOutput)

	badCfgCtx := config.WithConfig(context.Background(), &config.Config{
		Action: config.ActionConfig{
			DefaultTimeout: "bad-duration",
			CIOutput:       "none",
		},
	})
	optsFallback := &SolutionOptions{CIOutput: "auto"}
	optsFallback.flagsChanged = map[string]bool{}
	assert.Equal(t, "auto", optsFallback.getEffectiveActionConfig(badCfgCtx).CIOutput)
}

func TestCommandSolution_PositionalParamsActionShorthandAndValidation(t *testing.T) {
	t.Parallel()

	t.Run("executes with positional key=value and -a shorthand", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: param-sol
  version: 1.0.0
spec:
  resolvers:
    env:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
              default: dev
  workflow:
    actions:
      greet:
        provider: static
        inputs:
          value:
            rslvr: env
      other:
        provider: static
        inputs:
          value: skipped
`
		require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "scafctl/run")
		cmd.SetArgs([]string{"-f", solPath, "-a", "greet", "env=production", "-o", "json"})
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		cmd.SetContext(ctx)

		require.NoError(t, cmd.Execute(), stderr.String())
		assert.Contains(t, stdout.String(), "greet")
		assert.NotContains(t, stdout.String(), `"other"`)
	})

	t.Run("rejects multiple positional bare names with -a hint and embedder name", func(t *testing.T) {
		t.Parallel()
		streams, _, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "mycli/run")
		cmd.SetArgs([]string{"my-app", "init-repo"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error()+errBuf.String(), "mycli run solution my-app -a init-repo")
	})

	t.Run("rejects -f with multiple bare positional words without bogus catalog hint", func(t *testing.T) {
		t.Parallel()
		streams, _, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "mycli/run")
		cmd.SetArgs([]string{"-f", "./solution.yaml", "lint", "test"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error()+errBuf.String(), "cannot use both -f/--file and a positional argument")
		assert.NotContains(t, err.Error()+errBuf.String(), "run solution lint -a test")
	})

	t.Run("rejects local path positional before -a hint", func(t *testing.T) {
		t.Parallel()
		streams, _, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "mycli/run")
		cmd.SetArgs([]string{"./solution.yaml", "deploy"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error()+errBuf.String(), "mycli run solution -f ./solution.yaml")
		assert.NotContains(t, err.Error()+errBuf.String(), "./solution.yaml -a deploy")
	})

	t.Run("rejects catalog ref passed via -f", func(t *testing.T) {
		t.Parallel()
		streams, _, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "mycli/run")
		cmd.SetArgs([]string{"-f", "my-app"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, err.Error()+errBuf.String(), "mycli run solution my-app")
	})

	t.Run("rejects -f - combined with positional @-", func(t *testing.T) {
		t.Parallel()
		streams, _, _ := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "scafctl/run")
		cmd.SetArgs([]string{"-f", "-", "@-"})
		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both -f - and @-")
	})

	t.Run("writeActionTestOutput includes DynamicArgs", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte("apiVersion: scafctl.io/v1\n"), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		opts := &SolutionOptions{
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:      streams,
				CliParams:      cliParams,
				File:           solPath,
				ResolverParams: []string{"region=us-east1"},
			},
			DynamicArgs: []string{"--env=prod"},
		}
		ctx := writer.WithWriter(context.Background(), writer.New(streams, cliParams))
		res := &action.ExecutionResult{
			Actions: map[string]*action.ActionResult{
				"greet": {Status: action.StatusSucceeded},
			},
			FinalStatus: action.ExecutionSucceeded,
		}
		require.NoError(t, opts.writeActionTestOutput(ctx, res, nil))
		assert.Contains(t, stdout.String(), "region=us-east1")
		assert.Contains(t, stdout.String(), "env=prod")
		assert.NotContains(t, stdout.String(), "--env=prod")
	})
}

func TestCommandSolution_HelpAndCompletion_TreeOutput(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandSolution(cliParams, streams, "mycli/run")
	assert.Contains(t, cmd.Long, "OUTPUT FORMATS:")
	assert.Contains(t, cmd.Long, "tree")
	assert.Contains(t, cmd.Long, "mycli run solution -f ./my-solution.yaml -o tree")
	assert.NotContains(t, cmd.Long, settings.CliBinaryName)

	outFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outFlag)
	assert.Contains(t, outFlag.Usage, "tree")

	compFn, ok := cmd.GetFlagCompletionFunc("output")
	require.True(t, ok, "output flag should register a shell completion function")

	allMatches, directive := compFn(cmd, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Equal(t, solutionOutputFormats, allMatches)

	prefixMatches, prefixDirective := compFn(cmd, nil, "tr")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, prefixDirective)
	assert.Equal(t, []string{"tree"}, prefixMatches)

	// Verify default outputCompletionFn fallback when unset (e.g. CommandResolver).
	resCmd := CommandResolver(cliParams, streams, "mycli/run")
	resCompFn, resOK := resCmd.GetFlagCompletionFunc("output")
	require.True(t, resOK)
	resMatches, resDir := resCompFn(resCmd, nil, "")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, resDir)
	assert.Equal(t, ValidOutputTypes, resMatches)
}

func TestSolutionOptions_Run_TreeOutput(t *testing.T) {
	t.Parallel()

	const treeSolYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-sol
  version: 1.0.0
spec:
  resolvers:
    files:
      resolve:
        with:
          - provider: static
            inputs:
              value:
                src:
                  - main.go
                  - helper.go
                docs:
                  - README.md
  workflow:
    actions:
      announce:
        provider: message
        inputs:
          message: "STREAMED_BANNER"
          type: info
      list-files:
        dependsOn: [announce]
        provider: static
        inputs:
          value:
            rslvr: files
`

	t.Run("renders nested action results as tree with --no-color and suppresses direct stdout streaming", func(t *testing.T) {
		t.Parallel()
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(treeSolYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "scafctl/run")
		cmd.SetArgs([]string{"-f", solPath, "-a", "list-files", "-o", "tree"})
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		cmd.SetContext(ctx)

		require.NoError(t, cmd.Execute(), stderr.String())
		out := stdout.String()
		assert.NotContains(t, out, "\x1b[", "tree output with --no-color must not contain ANSI escape codes")
		assert.Contains(t, out, "actions")
		assert.Contains(t, out, "list-files")
		assert.Contains(t, out, "main.go")
		assert.Contains(t, out, "README.md")
		assert.Contains(t, out, "succeeded")
		assert.True(t, bytes.Contains(stdout.Bytes(), []byte("├──")) || bytes.Contains(stdout.Bytes(), []byte("└──")),
			"expected ASCII/UTF-8 tree branch connectors in output:\n%s", out)
		assert.Contains(t, stderr.String(), "==> list-files", "progress banners must stay on stderr")
		assert.NotContains(t, out, "==> list-files", "progress banners must not corrupt tree stdout")
	})

	t.Run("filters tree output with -e expression", func(t *testing.T) {
		t.Parallel()
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(treeSolYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "scafctl/run")
		cmd.SetArgs([]string{"-f", solPath, "-a", "list-files", "-o", "tree", "-e", `_.actions["list-files"].results`})
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		cmd.SetContext(ctx)

		require.NoError(t, cmd.Execute(), stderr.String())
		out := stdout.String()
		assert.Contains(t, out, "src")
		assert.Contains(t, out, "main.go")
		assert.NotContains(t, out, "startTime", "expression filter should project only the selected subtree")
	})

	t.Run("renders dry-run report as tree with --dry-run -o tree", func(t *testing.T) {
		t.Parallel()
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(treeSolYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "mycli/run")
		cmd.SetArgs([]string{"-f", solPath, "--dry-run", "-o", "tree"})
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))
		cmd.SetContext(ctx)

		require.NoError(t, cmd.Execute(), stderr.String())
		out := stdout.String()
		assert.NotContains(t, out, "\x1b[")
		assert.Contains(t, out, "tree-sol")
		assert.Contains(t, out, "actionPlan")
		assert.Contains(t, out, "list-files")
	})

	t.Run("writeTreeData handles nil streams without panicking", func(t *testing.T) {
		t.Parallel()
		opts := &SolutionOptions{}
		opts.Output = "tree"
		require.NoError(t, opts.writeActionOutput(context.Background(), &action.ExecutionResult{}, nil))
	})

	t.Run("redirects resolver streaming provider stdout to stderr and injects validation warnings under -o tree", func(t *testing.T) {
		t.Parallel()
		const resolverStreamWarnYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-warn-sol
  version: 1.0.0
spec:
  resolvers:
    notice:
      resolve:
        with:
          - provider: message
            inputs:
              message: "RESOLVER_STREAM_TEXT"
              type: info
    bad:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "x"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^longer-than-one$"
              message: "bad must be longer"
  workflow:
    actions:
      done:
        provider: static
        inputs:
          value:
            ok: true
`
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(resolverStreamWarnYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		reg := testRegistryWithValidation(t)
		require.NoError(t, reg.Register(messageprovider.NewMessageProvider()))

		opts := &SolutionOptions{
			ActionTimeout: 10 * time.Second,
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:         streams,
				CliParams:         cliParams,
				File:              solPath,
				OnValidationError: "warn",
				KvxOutputFlags:    flags.KvxOutputFlags{Output: "tree"},
				ResolverTimeout:   10 * time.Second,
				PhaseTimeout:      30 * time.Second,
				registry:          reg,
			},
		}
		ctx := writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams))

		require.NoError(t, opts.Run(ctx), stderr.String())
		out := stdout.String()
		assert.True(t, bytes.HasPrefix(stdout.Bytes(), []byte(".\n")),
			"resolver streaming output must be routed to stderr so stdout starts cleanly with the tree root:\n%s", out)
		assert.Contains(t, stderr.String(), "RESOLVER_STREAM_TEXT")
		assert.Contains(t, out, "diagnostics", "validation warning diagnostics must be included in tree output")
		assert.Contains(t, out, "bad must be longer")
	})

	t.Run("resolver failure is stderr-only while action failure renders failed action tree", func(t *testing.T) {
		t.Parallel()
		const resolverFailYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-res-fail
  version: 1.0.0
spec:
  resolvers:
    bad:
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "1 / 0"
  workflow:
    actions:
      noop:
        provider: static
        inputs:
          value: ok
`
		resSolPath := filepath.Join(t.TempDir(), "res-fail.yaml")
		require.NoError(t, os.WriteFile(resSolPath, []byte(resolverFailYAML), 0o600))

		var resOut, resErr bytes.Buffer
		resStreams := &terminal.IOStreams{Out: &resOut, ErrOut: &resErr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		resCmd := CommandSolution(cliParams, resStreams, "scafctl/run")
		resCmd.SetArgs([]string{"-f", resSolPath, "-o", "tree"})
		resCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(resStreams, cliParams)))
		err := resCmd.Execute()
		require.Error(t, err)
		assert.Empty(t, resOut.String(), "resolver-phase failure under -o tree must keep stdout empty")
		assert.NotEmpty(t, resErr.String())

		const actionFailYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: tree-act-fail
  version: 1.0.0
spec:
  resolvers: {}
  workflow:
    actions:
      explode:
        provider: go-template
        inputs:
          template: "{{ undefinedFunc .x }}"
          data:
            x: val
`
		actSolPath := filepath.Join(t.TempDir(), "act-fail.yaml")
		require.NoError(t, os.WriteFile(actSolPath, []byte(actionFailYAML), 0o600))

		var actOut, actErr bytes.Buffer
		actStreams := &terminal.IOStreams{Out: &actOut, ErrOut: &actErr}
		actCmd := CommandSolution(cliParams, actStreams, "scafctl/run")
		actCmd.SetArgs([]string{"-f", actSolPath, "-o", "tree"})
		actCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(actStreams, cliParams)))
		actExecErr := actCmd.Execute()
		require.Error(t, actExecErr)
		assert.Equal(t, exitcode.ActionFailed, exitcode.GetCode(actExecErr))
		assert.Contains(t, actOut.String(), "failed")
		assert.Contains(t, actOut.String(), "explode")
	})

	t.Run("invalid -e expression with -o tree returns error", func(t *testing.T) {
		t.Parallel()
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(treeSolYAML), 0o600))

		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false

		cmd := CommandSolution(cliParams, streams, "scafctl/run")
		cmd.SetArgs([]string{"-f", solPath, "-a", "list-files", "-o", "tree", "-e", "_.nonexistent_key"})
		cmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(streams, cliParams)))

		err := cmd.Execute()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expression evaluation failed")
	})
}

func TestRunTree_SensitiveResolverAndQuiet(t *testing.T) {
	t.Parallel()

	const sensitiveSolYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-tree-test
  version: 1.0.0
spec:
  resolvers:
    secretToken:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: super-secret-resolver-token
    shortPin:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: xy
  workflow:
    actions:
      echoSecret:
        provider: static
        inputs:
          value:
            token:
              rslvr: secretToken
            pin:
              rslvr: shortPin
            public: visible-value
`

	t.Run("run solution and run action mask sensitive resolver values in -o tree unless --show-sensitive or json", func(t *testing.T) {
		t.Parallel()
		solPath := filepath.Join(t.TempDir(), "solution.yaml")
		require.NoError(t, os.WriteFile(solPath, []byte(sensitiveSolYAML), 0o600))

		// 1. run solution -o tree masks both long and short sensitive resolver outputs
		var solOut, solErr bytes.Buffer
		solStreams := &terminal.IOStreams{Out: &solOut, ErrOut: &solErr}
		cliParams := settings.NewCliParams()
		cliParams.NoColor = true
		cliParams.ExitOnError = false
		solCmd := CommandSolution(cliParams, solStreams, "scafctl/run")
		solCmd.SetArgs([]string{"-f", solPath, "-o", "tree"})
		solCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(solStreams, cliParams)))
		require.NoError(t, solCmd.Execute())
		assert.NotContains(t, solOut.String(), "super-secret-resolver-token")
		assert.NotContains(t, solOut.String(), ": xy")
		assert.Contains(t, solOut.String(), redact.RedactedPlaceholder)
		assert.Contains(t, solOut.String(), "visible-value")

		// 2. run action echoSecret -o tree also masks sensitive resolver outputs
		var actOut, actErr bytes.Buffer
		actStreams := &terminal.IOStreams{Out: &actOut, ErrOut: &actErr}
		actCmd := CommandAction(cliParams, actStreams, "scafctl/run")
		actCmd.SetArgs([]string{"echoSecret", "-f", solPath, "-o", "tree"})
		actCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(actStreams, cliParams)))
		require.NoError(t, actCmd.Execute())
		assert.NotContains(t, actOut.String(), "super-secret-resolver-token")
		assert.Contains(t, actOut.String(), redact.RedactedPlaceholder)

		// 3. --show-sensitive reveals in -o tree, and -o json preserves raw values
		var revealOut bytes.Buffer
		revealStreams := &terminal.IOStreams{Out: &revealOut, ErrOut: &solErr}
		revealCmd := CommandSolution(cliParams, revealStreams, "scafctl/run")
		revealCmd.SetArgs([]string{"-f", solPath, "-o", "tree", "--show-sensitive"})
		revealCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(revealStreams, cliParams)))
		require.NoError(t, revealCmd.Execute())
		assert.Contains(t, revealOut.String(), "super-secret-resolver-token")

		var jsonOut bytes.Buffer
		jsonStreams := &terminal.IOStreams{Out: &jsonOut, ErrOut: &solErr}
		jsonCmd := CommandSolution(cliParams, jsonStreams, "scafctl/run")
		jsonCmd.SetArgs([]string{"-f", solPath, "-o", "json"})
		jsonCmd.SetContext(writer.WithWriter(logger.WithLogger(context.Background(), logger.Get(0)), writer.New(jsonStreams, cliParams)))
		require.NoError(t, jsonCmd.Execute())
		assert.Contains(t, jsonOut.String(), "super-secret-resolver-token")
	})

	t.Run("global --quiet suppresses tree stdout", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		streams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
		cliParams := settings.NewCliParams()
		cliParams.IsQuiet = true
		ctx := writer.WithWriter(context.Background(), writer.New(streams, cliParams))

		opts := &SolutionOptions{}
		opts.IOStreams = streams
		opts.CliParams = cliParams
		opts.Output = "tree"

		require.NoError(t, opts.writeTreeData(ctx, map[string]any{"status": "succeeded"}))
		assert.Empty(t, stdout.String())
	})
}
