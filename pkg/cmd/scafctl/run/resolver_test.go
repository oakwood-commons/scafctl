// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandResolver(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandResolver(cliParams, streams, "")

	assert.Equal(t, "resolver [name...]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Contains(t, cmd.Aliases, "res")
	assert.Contains(t, cmd.Aliases, "resolvers")

	// Verify shared resolver flags exist
	ff := cmd.Flags()
	assert.NotNil(t, ff.Lookup("file"))
	assert.NotNil(t, ff.Lookup("resolver"))
	assert.NotNil(t, ff.Lookup("output"))
	assert.NotNil(t, ff.Lookup("resolve-all"))
	assert.NotNil(t, ff.Lookup("progress"))
	assert.NotNil(t, ff.Lookup("warn-value-size"))
	assert.NotNil(t, ff.Lookup("max-value-size"))
	assert.NotNil(t, ff.Lookup("resolver-timeout"))
	assert.NotNil(t, ff.Lookup("phase-timeout"))

	// Verify resolver-specific flags
	assert.NotNil(t, ff.Lookup("verbose"))

	// Should NOT have solution-specific flags
	assert.Nil(t, ff.Lookup("action-timeout"))
	assert.Nil(t, ff.Lookup("dry-run"))
}

func TestCommandResolver_FlagDefaults(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandResolver(cliParams, streams, "")
	ff := cmd.Flags()

	file, err := ff.GetString("file")
	require.NoError(t, err)
	assert.Empty(t, file)

	output, err := ff.GetString("output")
	require.NoError(t, err)
	assert.Equal(t, "table", output)

	verbose, err := ff.GetBool("verbose")
	require.NoError(t, err)
	assert.False(t, verbose)

	progress, err := ff.GetBool("progress")
	require.NoError(t, err)
	assert.False(t, progress)

	resolveAll, err := ff.GetBool("resolve-all")
	require.NoError(t, err)
	assert.False(t, resolveAll)

	resolverTimeout, err := ff.GetDuration("resolver-timeout")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, resolverTimeout)

	phaseTimeout, err := ff.GetDuration("phase-timeout")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, phaseTimeout)
}

func TestResolverOptions_Run_NoFile(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams: streams,
			CliParams: cliParams,
			File:      "",
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err := opts.Run(ctx)
	assert.Error(t, err)
}

func TestResolverOptions_Run_FileNotFound(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ResolverOptions{
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

func TestResolverOptions_Run_AllResolvers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-all-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
    farewell:
      resolve:
        with:
          - provider: static
            inputs:
              value: goodbye
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

	opts := &ResolverOptions{
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
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "goodbye")
}

func TestResolverOptions_Run_NamedResolvers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: named-resolver-test
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

	opts := &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		Names: []string{"dependent"},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Should have base (dependency of dependent) and dependent but not independent
	output := stdout.String()
	assert.Contains(t, output, "base")
	assert.Contains(t, output, "dependent")
	assert.NotContains(t, output, "independent-value")
}

func TestResolverOptions_Run_UnknownName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unknown-resolver-test
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

	opts := &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		Names: []string{"nonexistent"},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown resolver(s): nonexistent")
	assert.Contains(t, err.Error(), "existing")
}

func TestResolverOptions_Run_Verbose(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: verbose-resolver-test
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

	var stdout, stderr bytes.Buffer
	streams := &terminal.IOStreams{
		In:     nil,
		Out:    &stdout,
		ErrOut: &stderr,
	}
	cliParams := settings.NewCliParams()
	cliParams.ExitOnError = false

	opts := &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		Verbose: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	// Verbose metadata is included in stdout output, not stderr
	output := stdout.String()
	assert.Contains(t, output, "hello")
	assert.Contains(t, output, "__execution")
	assert.Contains(t, output, "resolvers")
	assert.Contains(t, output, "summary")

	// Parse JSON to validate structure
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	// Verify __execution exists and has expected sections
	execution, ok := result["__execution"].(map[string]any)
	require.True(t, ok, "__execution should be a map")

	// Check resolvers section
	resolversMeta, ok := execution["resolvers"].(map[string]any)
	require.True(t, ok, "resolvers section should be a map")

	greetingMeta, ok := resolversMeta["greeting"].(map[string]any)
	require.True(t, ok, "greeting resolver metadata should exist")
	assert.Equal(t, "static", greetingMeta["provider"])
	assert.Equal(t, "success", greetingMeta["status"])
	assert.Contains(t, greetingMeta, "phase")
	assert.Contains(t, greetingMeta, "duration")
	assert.Contains(t, greetingMeta, "dependencies")

	// Check summary section
	summary, ok := execution["summary"].(map[string]any)
	require.True(t, ok, "summary section should be a map")
	assert.Contains(t, summary, "totalDuration")
	assert.Contains(t, summary, "resolverCount")
	assert.Contains(t, summary, "phaseCount")
	assert.Equal(t, float64(1), summary["resolverCount"])

	// stderr should NOT have verbose text anymore
	assert.NotContains(t, stderr.String(), "Resolver Execution Plan")
	assert.NotContains(t, stderr.String(), "Execution completed in")
}

func TestResolverOptions_Run_EmptySolution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-resolver-test
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
	cliParams.ExitOnError = false

	opts := &ResolverOptions{
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
	require.NoError(t, err)

	assert.Contains(t, stdout.String(), "{}")
}

func TestResolverOptions_Run_SensitiveRedaction(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-resolver-test
  version: 1.0.0
spec:
  resolvers:
    secret:
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: super-secret
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

	opts := &ResolverOptions{
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
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "[REDACTED]")
	assert.NotContains(t, output, "super-secret")
	assert.Contains(t, output, "public-data")
}

func TestResolverOptions_Run_MaxValueSizeExceeded(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: max-size-resolver-test
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

	opts := &ResolverOptions{
		sharedResolverOptions: sharedResolverOptions{
			IOStreams:       streams,
			CliParams:       cliParams,
			File:            solutionPath,
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
			MaxValueSize:    10,
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestResolverProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resolver *resolver.Resolver
		expected string
	}{
		{
			name: "with provider",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static"}},
				},
			},
			expected: "static",
		},
		{
			name:     "nil resolve",
			resolver: &resolver.Resolver{},
			expected: "unknown",
		},
		{
			name: "empty with",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{},
				},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, resolverProviderName(tt.resolver))
		})
	}
}

func TestResolverNamesString(t *testing.T) {
	t.Parallel()

	resolvers := []*resolver.Resolver{
		{Name: "alpha"},
		{Name: "beta"},
		{Name: "gamma"},
	}

	result := resolverNamesString(resolvers)
	assert.Equal(t, "alpha, beta, gamma", result)
}

func TestResolverNamesString_Empty(t *testing.T) {
	t.Parallel()

	result := resolverNamesString(nil)
	assert.Equal(t, "", result)
}

func TestBuildExecutionData(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("db", &resolver.ExecutionResult{
		Value:             "conn-string",
		Status:            resolver.ExecutionStatusSuccess,
		Phase:             2,
		TotalDuration:     250 * time.Millisecond,
		ProviderCallCount: 1,
		ValueSizeBytes:    42,
		DependencyCount:   1,
		PhaseMetrics: []resolver.PhaseMetrics{
			{Phase: "resolve", Duration: 200 * time.Millisecond},
			{Phase: "validate", Duration: 50 * time.Millisecond},
		},
	})
	resolverCtx.SetResult("env", &resolver.ExecutionResult{
		Value:             "prod",
		Status:            resolver.ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     5 * time.Millisecond,
		ProviderCallCount: 1,
		ValueSizeBytes:    4,
		DependencyCount:   0,
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "env",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "db",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "cel"}},
			},
		},
	}

	opts := &ResolverOptions{}
	data := opts.buildExecutionData(resolverCtx, resolvers, 300*time.Millisecond)

	// Check top-level sections
	assert.Contains(t, data, "resolvers")
	assert.Contains(t, data, "summary")

	// Check resolvers section
	resolversMeta, ok := data["resolvers"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, resolversMeta, 2)

	// Check env resolver
	envMeta, ok := resolversMeta["env"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, envMeta["phase"])
	assert.Equal(t, "success", envMeta["status"])
	assert.Equal(t, "static", envMeta["provider"])
	assert.Equal(t, 0, envMeta["dependencyCount"])

	// Check db resolver
	dbMeta, ok := resolversMeta["db"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 2, dbMeta["phase"])
	assert.Equal(t, "success", dbMeta["status"])
	assert.Equal(t, "cel", dbMeta["provider"])
	assert.Equal(t, "250ms", dbMeta["duration"])
	assert.Equal(t, 1, dbMeta["dependencyCount"])

	// Check phaseMetrics present for db
	phaseMetrics, ok := dbMeta["phaseMetrics"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, phaseMetrics, 2)
	assert.Equal(t, "resolve", phaseMetrics[0]["phase"])
	assert.Equal(t, "validate", phaseMetrics[1]["phase"])

	// Check summary
	summary, ok := data["summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "300ms", summary["totalDuration"])
	assert.Equal(t, 2, summary["resolverCount"])
	assert.Equal(t, 2, summary["phaseCount"])
}

func TestBuildExecutionData_MissingResult(t *testing.T) {
	t.Parallel()

	// Resolver exists but has no result in context
	resolverCtx := resolver.NewContext()
	resolvers := []*resolver.Resolver{
		{
			Name: "missing",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		},
	}

	opts := &ResolverOptions{}
	data := opts.buildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	missingMeta, ok := resolversMeta["missing"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0, missingMeta["phase"])
	assert.Equal(t, "0s", missingMeta["duration"])
	assert.Equal(t, "unknown", missingMeta["status"])
	assert.Equal(t, "http", missingMeta["provider"])
}

func TestBuildExecutionData_WithFailedAttempts(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("fallback", &resolver.ExecutionResult{
		Value:         "final-value",
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 100 * time.Millisecond,
		FailedAttempts: []resolver.ProviderAttempt{
			{
				Provider:   "http",
				Phase:      "resolve",
				Error:      "connection refused",
				Duration:   50 * time.Millisecond,
				OnError:    "continue",
				SourceStep: 0,
			},
		},
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
	}

	opts := &ResolverOptions{}
	data := opts.buildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	fbMeta := resolversMeta["fallback"].(map[string]any)

	attempts, ok := fbMeta["failedAttempts"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, attempts, 1)
	assert.Equal(t, "http", attempts[0]["provider"])
	assert.Equal(t, "connection refused", attempts[0]["error"])
	assert.Equal(t, "continue", attempts[0]["onError"])
}

func TestBuildExecutionData_CELDependencies(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("env", &resolver.ExecutionResult{
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 5 * time.Millisecond,
	})
	resolverCtx.SetResult("region", &resolver.ExecutionResult{
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 5 * time.Millisecond,
	})

	celExpr := celexp.Expression("'api.' + _.env + '.' + _.region + '.example.com'")
	resolverCtx.SetResult("hostname", &resolver.ExecutionResult{
		Status:          resolver.ExecutionStatusSuccess,
		Phase:           2,
		TotalDuration:   10 * time.Millisecond,
		DependencyCount: 2,
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "env",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "region",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "hostname",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*resolver.ValueRef{
							"expression": {Expr: &celExpr},
						},
					},
				},
			},
		},
	}

	opts := &ResolverOptions{}
	data := opts.buildExecutionData(resolverCtx, resolvers, 20*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	hostnameMeta := resolversMeta["hostname"].(map[string]any)

	// dependencies should now contain the CEL-extracted deps
	deps, ok := hostnameMeta["dependencies"].([]string)
	require.True(t, ok)
	assert.Len(t, deps, 2)
	assert.Contains(t, deps, "env")
	assert.Contains(t, deps, "region")
	assert.Equal(t, 2, hostnameMeta["dependencyCount"])
}
