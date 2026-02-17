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
	"github.com/oakwood-commons/scafctl/pkg/solution"
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
	assert.NotNil(t, ff.Lookup("skip-transform"))
	assert.NotNil(t, ff.Lookup("dry-run"))

	// Should NOT have solution-specific flags
	assert.Nil(t, ff.Lookup("action-timeout"))
	assert.Nil(t, ff.Lookup("show-execution"))
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

	skipTransform, err := ff.GetBool("skip-transform")
	require.NoError(t, err)
	assert.False(t, skipTransform)

	dryRun, err := ff.GetBool("dry-run")
	require.NoError(t, err)
	assert.False(t, dryRun)

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

func TestResolverOptions_Run_MultipleNamedResolvers(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-named-resolver-test
  version: 1.0.0
spec:
  resolvers:
    env:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: production
    region:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: us-west-2
    host:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: env
    port:
      type: int
      resolve:
        with:
          - provider: static
            inputs:
              value: 8080
    url:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: host
    unrelated:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: should-not-appear
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
		Names: []string{"host", "port"},
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err)

	// Should include host, port, and transitive dep env (host -> env via rslvr:)
	assert.Contains(t, result, "host")
	assert.Contains(t, result, "port")
	assert.Contains(t, result, "env")

	// Should NOT include url, unrelated, or region (not requested, not a dependency)
	assert.NotContains(t, result, "url")
	assert.NotContains(t, result, "unrelated")
	assert.NotContains(t, result, "region")
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

func TestResolverOptions_Run_ExecutionMetadataAlwaysIncluded(t *testing.T) {
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
}

func TestResolverOptions_Run_HideExecution(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hide-execution-test
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
		HideExecution: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "hello")
	assert.NotContains(t, output, "__execution", "__execution should not be present when --hide-execution is set")

	// Parse JSON to validate structure
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	_, hasExecution := result["__execution"]
	assert.False(t, hasExecution, "__execution key should not exist in output")

	// Resolver values should still be present
	assert.Equal(t, "hello", result["greeting"])
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

func TestResolverOptions_Run_SensitiveRedaction_TableRedacts(t *testing.T) {
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

	// Table format should redact sensitive values (human-facing)
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
		HideExecution: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	// Table output (using json since table requires TTY, but test the redaction logic directly)
	// Test buildResolverOutputMap with table format
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"secret": {Name: "secret", Sensitive: true},
		"public": {Name: "public", Sensitive: false},
	}

	tableOpts := &sharedResolverOptions{
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}
	results := tableOpts.buildResolverOutputMap(
		map[string]any{"secret": "super-secret", "public": "public-data"},
		sol,
	)
	assert.Equal(t, "[REDACTED]", results["secret"])
	assert.Equal(t, "public-data", results["public"])

	// Also verify full Run path with json format reveals values
	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	assert.Contains(t, output, "super-secret", "JSON output should reveal sensitive values")
	assert.Contains(t, output, "public-data")
}

func TestResolverOptions_Run_SensitiveRedaction_JSONReveals(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-json-test
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
		HideExecution: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	// JSON output should reveal sensitive values (machine-facing, Terraform model)
	assert.Contains(t, output, "super-secret", "JSON output should reveal sensitive values")
	assert.NotContains(t, output, "[REDACTED]", "JSON output should not redact")
	assert.Contains(t, output, "public-data")
}

func TestResolverOptions_Run_SensitiveRedaction_YAMLReveals(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sensitive-yaml-test
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
			KvxOutputFlags:  flags.KvxOutputFlags{Output: "yaml"},
			ResolverTimeout: 30 * time.Second,
			PhaseTimeout:    5 * time.Minute,
			registry:        testRegistry(),
		},
		HideExecution: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()
	// YAML output should also reveal sensitive values (machine-facing)
	assert.Contains(t, output, "super-secret", "YAML output should reveal sensitive values")
	assert.NotContains(t, output, "[REDACTED]", "YAML output should not redact")
	assert.Contains(t, output, "public-data")
}

func TestResolverOptions_Run_SensitiveRedaction_ShowSensitiveFlag(t *testing.T) {
	t.Parallel()

	// Test that --show-sensitive reveals values even in table format
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"secret": {Name: "secret", Sensitive: true},
		"public": {Name: "public", Sensitive: false},
	}

	resolverData := map[string]any{
		"secret": "super-secret",
		"public": "public-data",
	}

	// Table format with --show-sensitive should reveal values
	opts := &sharedResolverOptions{
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
		ShowSensitive:  true,
	}
	results := opts.buildResolverOutputMap(resolverData, sol)
	assert.Equal(t, "super-secret", results["secret"], "--show-sensitive should reveal in table format")
	assert.Equal(t, "public-data", results["public"])
}

func TestShouldRedactSensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		format        string
		showSensitive bool
		wantRedact    bool
	}{
		{name: "table redacts", format: "table", showSensitive: false, wantRedact: true},
		{name: "empty format redacts (defaults to table)", format: "", showSensitive: false, wantRedact: true},
		{name: "json reveals", format: "json", showSensitive: false, wantRedact: false},
		{name: "yaml reveals", format: "yaml", showSensitive: false, wantRedact: false},
		{name: "quiet reveals", format: "quiet", showSensitive: false, wantRedact: false},
		{name: "table with show-sensitive reveals", format: "table", showSensitive: true, wantRedact: false},
		{name: "json with show-sensitive reveals", format: "json", showSensitive: true, wantRedact: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := &sharedResolverOptions{
				KvxOutputFlags: flags.KvxOutputFlags{Output: tt.format},
				ShowSensitive:  tt.showSensitive,
			}
			assert.Equal(t, tt.wantRedact, opts.shouldRedactSensitive())
		})
	}
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
	_ = opts // verify type exists
	data := buildExecutionData(resolverCtx, resolvers, 300*time.Millisecond)

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

	data := buildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

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
	_ = opts // verify type exists
	data := buildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

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
	_ = opts // verify type exists
	data := buildExecutionData(resolverCtx, resolvers, 20*time.Millisecond)

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

func TestResolverOptions_Run_SkipTransform(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: skip-transform-test
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
      transform:
        with:
          - provider: cel
            inputs:
              expression: "'TRANSFORMED: ' + __self"
      validate:
        with:
          - provider: cel
            inputs:
              expression: "size(__self) > 0"
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
		SkipTransform: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()

	// Should have the raw resolved value, not the transformed one
	assert.Contains(t, output, "hello")
	assert.NotContains(t, output, "TRANSFORMED")

	// Should still have __execution metadata
	assert.Contains(t, output, "__execution")

	// Parse and verify __execution has only resolve phase in metrics
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	execution := result["__execution"].(map[string]any)
	resolversMeta := execution["resolvers"].(map[string]any)
	greetingMeta := resolversMeta["greeting"].(map[string]any)

	// Should have only the resolve phase metric (transform and validate skipped)
	if phaseMetrics, ok := greetingMeta["phaseMetrics"].([]any); ok {
		for _, pm := range phaseMetrics {
			pmMap := pm.(map[string]any)
			assert.NotEqual(t, "transform", pmMap["phase"], "transform phase should be skipped")
			assert.NotEqual(t, "validate", pmMap["phase"], "validate phase should be skipped")
		}
	}
}

func TestResolverOptions_Run_DryRun(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dry-run-test
  version: 1.0.0
spec:
  resolvers:
    env:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: production
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
      transform:
        with:
          - provider: cel
            inputs:
              expression: "'Hello from ' + _.env"
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
		DryRun: true,
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	err = opts.Run(ctx)
	require.NoError(t, err)

	output := stdout.String()

	// Should show execution plan, not actual values
	assert.Contains(t, output, "dryRun")
	assert.Contains(t, output, "executionPlan")
	assert.Contains(t, output, "resolvers")

	// Should NOT contain resolved values (providers not called)
	assert.NotContains(t, output, "production")
	assert.NotContains(t, output, "Hello from")

	// Parse and verify structure
	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.Equal(t, true, result["dryRun"])

	plan := result["executionPlan"].(map[string]any)
	assert.Equal(t, float64(2), plan["totalResolvers"])

	activePhases := plan["activePhases"].([]any)
	assert.Contains(t, activePhases, "resolve")
	assert.Contains(t, activePhases, "transform")
	assert.Contains(t, activePhases, "validate")

	resolversInfo := result["resolvers"].(map[string]any)
	assert.Contains(t, resolversInfo, "env")
	assert.Contains(t, resolversInfo, "greeting")

	greetingInfo := resolversInfo["greeting"].(map[string]any)
	assert.Equal(t, "static", greetingInfo["provider"])
	configuredPhases := greetingInfo["configuredPhases"].([]any)
	assert.Contains(t, configuredPhases, "resolve")
	assert.Contains(t, configuredPhases, "transform")
}

func TestBuildDryRunPlan(t *testing.T) {
	t.Parallel()

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
			Transform: &resolver.TransformPhase{},
			Validate:  &resolver.ValidatePhase{},
		},
	}

	phases := []*resolver.PhaseGroup{
		{Phase: 1, Resolvers: []*resolver.Resolver{resolvers[0]}},
		{Phase: 2, Resolvers: []*resolver.Resolver{resolvers[1]}},
	}

	t.Run("no skips", func(t *testing.T) {
		t.Parallel()
		plan := buildDryRunPlan(phases, resolvers, false, false)
		assert.Equal(t, true, plan["dryRun"])

		execPlan := plan["executionPlan"].(map[string]any)
		activePhases := execPlan["activePhases"].([]string)
		assert.Equal(t, []string{"resolve", "transform", "validate"}, activePhases)
		skippedPhases := execPlan["skippedPhases"].([]string)
		assert.Empty(t, skippedPhases)
	})

	t.Run("skip validation", func(t *testing.T) {
		t.Parallel()
		plan := buildDryRunPlan(phases, resolvers, false, true)
		execPlan := plan["executionPlan"].(map[string]any)
		activePhases := execPlan["activePhases"].([]string)
		assert.Equal(t, []string{"resolve", "transform"}, activePhases)
		skippedPhases := execPlan["skippedPhases"].([]string)
		assert.Equal(t, []string{"validate"}, skippedPhases)
	})

	t.Run("skip transform", func(t *testing.T) {
		t.Parallel()
		plan := buildDryRunPlan(phases, resolvers, true, false)
		execPlan := plan["executionPlan"].(map[string]any)
		activePhases := execPlan["activePhases"].([]string)
		assert.Equal(t, []string{"resolve"}, activePhases)
		skippedPhases := execPlan["skippedPhases"].([]string)
		assert.Equal(t, []string{"transform", "validate"}, skippedPhases)
	})
}

func TestCommandResolver_GraphSnapshotFlagDefaults(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandResolver(cliParams, streams, "")
	ff := cmd.Flags()

	graph, err := ff.GetBool("graph")
	require.NoError(t, err)
	assert.False(t, graph)

	graphFormat, err := ff.GetString("graph-format")
	require.NoError(t, err)
	assert.Equal(t, "ascii", graphFormat)

	snapshot, err := ff.GetBool("snapshot")
	require.NoError(t, err)
	assert.False(t, snapshot)

	snapshotFile, err := ff.GetString("snapshot-file")
	require.NoError(t, err)
	assert.Empty(t, snapshotFile)

	redact, err := ff.GetBool("redact")
	require.NoError(t, err)
	assert.False(t, redact)
}

func TestResolverOptions_Run_MutualExclusivity(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mutex-test
  version: 1.0.0
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: static
            inputs:
              value: x
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	makeOpts := func(dryRun, graph, snapshot bool) *ResolverOptions {
		return &ResolverOptions{
			sharedResolverOptions: sharedResolverOptions{
				IOStreams:       &terminal.IOStreams{In: nil, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
				CliParams:       func() *settings.Run { p := settings.NewCliParams(); p.ExitOnError = false; return p }(),
				File:            solutionPath,
				KvxOutputFlags:  flags.KvxOutputFlags{Output: "json"},
				ResolverTimeout: 30 * time.Second,
				PhaseTimeout:    5 * time.Minute,
				registry:        testRegistry(),
			},
			DryRun:   dryRun,
			Graph:    graph,
			Snapshot: snapshot,
		}
	}

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	t.Run("dry-run and graph", func(t *testing.T) {
		t.Parallel()
		err := makeOpts(true, true, false).Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("dry-run and snapshot", func(t *testing.T) {
		t.Parallel()
		err := makeOpts(true, false, true).Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("graph and snapshot", func(t *testing.T) {
		t.Parallel()
		err := makeOpts(false, true, true).Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "mutually exclusive")
	})

	t.Run("snapshot without file", func(t *testing.T) {
		t.Parallel()
		err := makeOpts(false, false, true).Run(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "--snapshot-file")
	})
}

func TestResolverOptions_Run_ExecutionIncludesGraph(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: graph-embed-test
  version: 1.0.0
spec:
  resolvers:
    base:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: base-val
    dependent:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: dep-val
      dependsOn:
        - base
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	var stdout bytes.Buffer
	streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &bytes.Buffer{}}
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

	var result map[string]any
	err = json.Unmarshal(stdout.Bytes(), &result)
	require.NoError(t, err)

	execution, ok := result["__execution"].(map[string]any)
	require.True(t, ok)

	// Verify dependencyGraph embedded
	graphData, ok := execution["dependencyGraph"].(map[string]any)
	require.True(t, ok, "__execution.dependencyGraph should be present")
	assert.Contains(t, graphData, "nodes")
	assert.Contains(t, graphData, "edges")
	assert.Contains(t, graphData, "stats")

	stats := graphData["stats"].(map[string]any)
	assert.Equal(t, float64(2), stats["totalResolvers"])
	assert.Contains(t, stats, "criticalPath")
	assert.Contains(t, stats, "criticalDepth")

	// Verify diagrams embedded
	diagrams, ok := graphData["diagrams"].(map[string]any)
	require.True(t, ok, "__execution.dependencyGraph.diagrams should be present")
	assert.NotEmpty(t, diagrams["ascii"])
	assert.Contains(t, diagrams["dot"].(string), "digraph")
	assert.Contains(t, diagrams["mermaid"].(string), "graph")

	// Verify providerSummary embedded
	providerSummary, ok := execution["providerSummary"].(map[string]any)
	require.True(t, ok, "__execution.providerSummary should be present")
	assert.Contains(t, providerSummary, "static")
}

func TestResolverOptions_Run_GraphMode(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: graph-mode-test
  version: 1.0.0
spec:
  resolvers:
    a:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: val-a
    b:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: val-b
      dependsOn:
        - a
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	formats := []struct {
		format   string
		contains string
	}{
		{"ascii", "a"},
		{"dot", "digraph"},
		{"mermaid", "graph"},
	}

	for _, f := range formats {
		t.Run(f.format, func(t *testing.T) {
			t.Parallel()
			var stdout bytes.Buffer
			streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &bytes.Buffer{}}
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
				Graph:       true,
				GraphFormat: f.format,
			}

			lgr := logger.Get(0)
			ctx := logger.WithLogger(context.Background(), lgr)

			err := opts.Run(ctx)
			require.NoError(t, err)
			assert.Contains(t, stdout.String(), f.contains)
		})
	}

	t.Run("json format", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &bytes.Buffer{}}
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
			Graph:       true,
			GraphFormat: "json",
		}

		lgr := logger.Get(0)
		ctx := logger.WithLogger(context.Background(), lgr)

		err := opts.Run(ctx)
		require.NoError(t, err)

		var graphData map[string]any
		err = json.Unmarshal(stdout.Bytes(), &graphData)
		require.NoError(t, err)
		assert.Contains(t, graphData, "nodes")
		assert.Contains(t, graphData, "edges")
		assert.Contains(t, graphData, "stats")
	})
}

func TestResolverOptions_Run_SnapshotMode(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	solutionPath := filepath.Join(tmpDir, "solution.yaml")
	solutionContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: snapshot-test
  version: 2.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: hello
    secret:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: static
            inputs:
              value: s3cr3t
`
	err := os.WriteFile(solutionPath, []byte(solutionContent), 0o600)
	require.NoError(t, err)

	lgr := logger.Get(0)
	ctx := logger.WithLogger(context.Background(), lgr)

	t.Run("basic snapshot", func(t *testing.T) {
		t.Parallel()
		snapshotFile := filepath.Join(t.TempDir(), "snap.json")
		var stdout bytes.Buffer
		streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &bytes.Buffer{}}
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
			Snapshot:     true,
			SnapshotFile: snapshotFile,
		}

		err := opts.Run(ctx)
		require.NoError(t, err)

		// Verify file was created
		_, statErr := os.Stat(snapshotFile)
		assert.NoError(t, statErr)

		// Verify output mentions snapshot
		assert.Contains(t, stdout.String(), "Snapshot saved to")
		assert.Contains(t, stdout.String(), "snapshot-test")

		// Read and parse snapshot
		data, err := os.ReadFile(snapshotFile)
		require.NoError(t, err)
		var snapshot map[string]any
		err = json.Unmarshal(data, &snapshot)
		require.NoError(t, err)

		meta := snapshot["metadata"].(map[string]any)
		assert.Equal(t, "snapshot-test", meta["solution"])
		assert.Equal(t, "2.0.0", meta["version"])
		assert.Equal(t, "success", meta["status"])
	})

	t.Run("snapshot with redact", func(t *testing.T) {
		t.Parallel()
		snapshotFile := filepath.Join(t.TempDir(), "snap-redacted.json")
		var stdout bytes.Buffer
		streams := &terminal.IOStreams{In: nil, Out: &stdout, ErrOut: &bytes.Buffer{}}
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
			Snapshot:     true,
			SnapshotFile: snapshotFile,
			Redact:       true,
		}

		err := opts.Run(ctx)
		require.NoError(t, err)

		// Read and parse snapshot
		data, err := os.ReadFile(snapshotFile)
		require.NoError(t, err)
		var snapshot map[string]any
		err = json.Unmarshal(data, &snapshot)
		require.NoError(t, err)

		// The sensitive resolver's value should be redacted
		resolversMap := snapshot["resolvers"].(map[string]any)
		secretResolver := resolversMap["secret"].(map[string]any)
		// Redacted values use "<redacted>"
		assert.Equal(t, "<redacted>", secretResolver["value"])
	})
}

func TestResolverAdapter(t *testing.T) {
	t.Parallel()

	a := &resolverAdapter{name: "test", sensitive: true}
	assert.Equal(t, "test", a.GetName())
	assert.True(t, a.GetSensitive())

	b := &resolverAdapter{name: "other", sensitive: false}
	assert.Equal(t, "other", b.GetName())
	assert.False(t, b.GetSensitive())
}
