// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── checkValueSizes tests ─────────────────────────────────────────────────────

func TestSharedResolverOptions_CheckValueSizes_NoLimits(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		MaxValueSize:  0,
		WarnValueSize: 0,
	}

	lgr := logger.GetNoopLogger()
	err := opts.checkValueSizes(map[string]any{
		"key": "value",
	}, *lgr)
	require.NoError(t, err)
}

func TestSharedResolverOptions_CheckValueSizes_WithinLimit(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		MaxValueSize: 1024 * 1024, // 1MB
	}

	lgr := logger.GetNoopLogger()
	err := opts.checkValueSizes(map[string]any{
		"key": "small value",
	}, *lgr)
	require.NoError(t, err)
}

func TestSharedResolverOptions_CheckValueSizes_ExceedsMax(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		MaxValueSize: 10, // 10 bytes — very small limit
	}

	lgr := logger.GetNoopLogger()
	err := opts.checkValueSizes(map[string]any{
		"key": "this value is much longer than ten bytes",
	}, *lgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum size")
	assert.Contains(t, err.Error(), "key")
}

func TestSharedResolverOptions_CheckValueSizes_WarnSize_NoError(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		WarnValueSize: 5, // 5 bytes — triggers warning but not error
		MaxValueSize:  0, // No max
	}

	lgr := logger.GetNoopLogger()
	// Should succeed (only log a warning internally)
	err := opts.checkValueSizes(map[string]any{
		"key": "this string exceeds 5 bytes",
	}, *lgr)
	require.NoError(t, err)
}

func TestSharedResolverOptions_CheckValueSizes_EmptyMap(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		MaxValueSize: 100,
	}

	lgr := logger.GetNoopLogger()
	err := opts.checkValueSizes(map[string]any{}, *lgr)
	require.NoError(t, err)
}

// ── addSharedResolverFlags tests ─────────────────────────────────────────────

func TestAddSharedResolverFlags(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{}
	cmd := &cobra.Command{Use: "test"}
	addSharedResolverFlags(cmd, opts)

	expectedFlags := []string{
		"file",
		"resolver",
		"output",
		"interactive",
		"expression",
		"resolve-all",
		"progress",
		"validate-all",
		"skip-validation",
		"show-metrics",
		"show-sensitive",
		"no-cache",
		"warn-value-size",
		"max-value-size",
		"resolver-timeout",
		"phase-timeout",
		"pre-release",
	}

	for _, name := range expectedFlags {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(name)
			assert.NotNil(t, f, "flag %q should be added by addSharedResolverFlags", name)
		})
	}
}

func TestAddSharedResolverFlags_FileShorthand(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{}
	cmd := &cobra.Command{Use: "test"}
	addSharedResolverFlags(cmd, opts)

	f := cmd.Flags().ShorthandLookup("f")
	require.NotNil(t, f, "shorthand -f should exist")
	assert.Equal(t, "file", f.Name)
}

func TestAddSharedResolverFlags_ResolverShorthand(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{}
	cmd := &cobra.Command{Use: "test"}
	addSharedResolverFlags(cmd, opts)

	f := cmd.Flags().ShorthandLookup("r")
	require.NotNil(t, f, "shorthand -r should exist")
	assert.Equal(t, "resolver", f.Name)
}

func TestAddSharedResolverFlags_DefaultValues(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{}
	cmd := &cobra.Command{Use: "test"}
	addSharedResolverFlags(cmd, opts)

	flags := cmd.Flags()

	timeout, err := flags.GetDuration("resolver-timeout")
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultResolverTimeout, timeout)

	phase, err := flags.GetDuration("phase-timeout")
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultPhaseTimeout, phase)

	warnSize, err := flags.GetInt64("warn-value-size")
	require.NoError(t, err)
	assert.Equal(t, int64(settings.DefaultWarnValueSize), warnSize)

	maxSize, err := flags.GetInt64("max-value-size")
	require.NoError(t, err)
	assert.Equal(t, int64(settings.DefaultMaxValueSize), maxSize)
}

// ── getEffectiveResolverConfig tests ─────────────────────────────────────────

func TestSharedResolverOptions_GetEffectiveResolverConfig_NilConfig(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		ResolverTimeout: 45 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		WarnValueSize:   500,
		MaxValueSize:    5000,
		ValidateAll:     true,
	}

	// No config in context — should fall back to CLI flag values
	cfg := opts.getEffectiveResolverConfig(context.Background())
	assert.Equal(t, 45*time.Second, cfg.Timeout)
	assert.Equal(t, 5*time.Minute, cfg.PhaseTimeout)
	assert.Equal(t, int64(500), cfg.WarnValueSize)
	assert.Equal(t, int64(5000), cfg.MaxValueSize)
	assert.True(t, cfg.ValidateAll)
}

func TestSharedResolverOptions_GetEffectiveResolverConfig_NoFlagsChanged(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		ResolverTimeout: 30 * time.Second,
		PhaseTimeout:    5 * time.Minute,
		WarnValueSize:   settings.DefaultWarnValueSize,
		MaxValueSize:    settings.DefaultMaxValueSize,
		// flagsChanged is nil — simulate test mode where values are respected as-is
	}

	cfg := opts.getEffectiveResolverConfig(context.Background())
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.Equal(t, 5*time.Minute, cfg.PhaseTimeout)
}

// ── shouldRedactSensitive tests ───────────────────────────────────────────────
// (supplementary tests in addition to those in resolver_test.go)

func TestSharedResolverOptions_ShouldRedactSensitive_ShowSensitiveOverride(t *testing.T) {
	t.Parallel()

	// --show-sensitive always reveals, even in table mode
	opts := &sharedResolverOptions{
		ShowSensitive:  true,
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}
	assert.False(t, opts.shouldRedactSensitive())
}

func TestSharedResolverOptions_ShouldRedactSensitive_TableRedacts(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}
	assert.True(t, opts.shouldRedactSensitive())
}

func TestSharedResolverOptions_ShouldRedactSensitive_EmptyOutputRedacts(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		KvxOutputFlags: flags.KvxOutputFlags{Output: ""},
	}
	assert.True(t, opts.shouldRedactSensitive())
}

// ── exitWithCode tests ────────────────────────────────────────────────────────

func TestSharedResolverOptions_ExitWithCode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(context.Background(), w)

	opts := &sharedResolverOptions{}
	err := opts.exitWithCode(ctx, assert.AnError, exitcode.InvalidInput)
	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, buf.String(), assert.AnError.Error())
}

func TestSharedResolverOptions_ExitWithCode_NilWriter(t *testing.T) {
	t.Parallel()

	// No writer in context — should not panic
	opts := &sharedResolverOptions{}
	err := opts.exitWithCode(context.Background(), assert.AnError, exitcode.GeneralError)
	require.Error(t, err)
	assert.Equal(t, exitcode.GeneralError, exitcode.GetCode(err))
}

// ── buildResolverOutputMap tests ─────────────────────────────────────────────
// (supplementary tests)

func TestSharedResolverOptions_BuildResolverOutputMap_NonSensitive(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		// Table format — would redact sensitive, but this resolver isn't sensitive
		KvxOutputFlags: flags.KvxOutputFlags{Output: "table"},
	}

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"public": {Name: "public", Sensitive: false},
	}

	result := opts.buildResolverOutputMap(map[string]any{
		"public": "visible-value",
	}, sol)
	assert.Equal(t, "visible-value", result["public"])
}

func TestSharedResolverOptions_BuildResolverOutputMap_JSONReveals(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
	}

	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"secret": {Name: "secret", Sensitive: true},
	}

	result := opts.buildResolverOutputMap(map[string]any{
		"secret": "secret-value",
	}, sol)
	// json format → never redact
	assert.Equal(t, "secret-value", result["secret"])
}

// ── makeRunEFunc tests ────────────────────────────────────────────────────────

type mockRunner struct {
	runFn func(ctx context.Context) error
}

func (m *mockRunner) Run(ctx context.Context) error {
	if m.runFn != nil {
		return m.runFn(ctx)
	}
	return nil
}

func TestMakeRunEFunc_CallsRunner(t *testing.T) {
	t.Parallel()

	var ran bool
	runner := &mockRunner{
		runFn: func(_ context.Context) error {
			ran = true
			return nil
		},
	}

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      "test",
		runner:    runner,
		getOutputFn: func() string {
			return "quiet"
		},
		setIOStreamFn: func(_ *terminal.IOStreams, _ *settings.Run) {},
	}

	runE := makeRunEFunc(cfg, "cmd")

	cmd := &cobra.Command{Use: "cmd"}
	cmd.SetContext(
		writer.WithWriter(context.Background(), w),
	)

	err := runE(cmd, []string{})
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestMakeRunEFunc_PropagatesRunnerError(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{
		runFn: func(_ context.Context) error {
			return assert.AnError
		},
	}

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      "test",
		runner:    runner,
		getOutputFn: func() string {
			return ""
		},
		setIOStreamFn: func(_ *terminal.IOStreams, _ *settings.Run) {},
	}

	runE := makeRunEFunc(cfg, "cmd")

	cmd := &cobra.Command{Use: "cmd"}
	cmd.SetContext(
		writer.WithWriter(context.Background(), w),
	)

	err := runE(cmd, []string{})
	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

// ── generateTestOutput tests ──────────────────────────────────────────────────

func TestSharedResolverOptions_GenerateTestOutput_Basic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	lgr := logger.GetNoopLogger()
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, lgr)

	tmpDir := t.TempDir()
	opts := &sharedResolverOptions{
		IOStreams:      ioStreams,
		CliParams:      settings.NewCliParams(),
		KvxOutputFlags: flags.KvxOutputFlags{Output: "json"},
		ResolverParams: []string{"env=production"},
		File:           filepath.Join(tmpDir, "solution.yaml"),
	}

	results := map[string]any{
		"greeting": "hello",
		"__execution": map[string]any{
			"duration": "1.0s",
		},
	}

	err := opts.generateTestOutput(ctx, []string{"run", "resolver"}, []string{}, results)
	require.NoError(t, err)

	// Output should contain generated test YAML
	output := buf.String()
	assert.NotEmpty(t, output)
	assert.Contains(t, output, "greeting")
}

func TestSharedResolverOptions_GenerateTestOutput_NoFile(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	lgr := logger.GetNoopLogger()
	ctx := writer.WithWriter(context.Background(), w)
	ctx = logger.WithLogger(ctx, lgr)

	tmpDir := t.TempDir()
	opts := &sharedResolverOptions{
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
		File:      filepath.Join(tmpDir, "solution.yaml"),
	}

	results := map[string]any{
		"key": "value",
	}

	err := opts.generateTestOutput(ctx, []string{"run", "resolver"}, nil, results)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkAddSharedResolverFlags(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		opts := &sharedResolverOptions{}
		cmd := &cobra.Command{Use: "test"}
		addSharedResolverFlags(cmd, opts)
	}
}

func BenchmarkCheckValueSizes(b *testing.B) {
	opts := &sharedResolverOptions{
		MaxValueSize:  1024 * 1024,
		WarnValueSize: 512,
	}
	data := map[string]any{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}
	lgr := logr.Discard()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = opts.checkValueSizes(data, lgr)
	}
}

// ── ResolverParametersHelp shared constant tests ──────────────────────────────

func TestResolverParametersHelp_EmbeddedInCommands(t *testing.T) {
	t.Parallel()

	streams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	resolverCmd := CommandResolver(cliParams, streams, "")
	solutionCmd := CommandSolution(cliParams, streams, "")

	// run resolver uses the full help (with positional params)
	assert.Contains(t, resolverCmd.Long, ResolverParametersHelp,
		"run resolver Long description should contain ResolverParametersHelp")

	// run solution uses the flag-only help (no positional params)
	assert.Contains(t, solutionCmd.Long, ResolverParametersFlagHelp,
		"run solution Long description should contain ResolverParametersFlagHelp")
	assert.NotContains(t, solutionCmd.Long, "Positional key=value",
		"run solution should not advertise positional key=value parameters")
}

func TestResolverParametersHelp_ContainsStdinConventions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
	}{
		{"positional key=value", "key=value"},
		{"stdin raw value", "key=@-"},
		{"stdin parsed", "@-"},
		{"file raw value", "key=@file"},
		{"file parsed YAML", "@file.yaml"},
		{"flag key=value", "-r key=value"},
		{"flag stdin", "-r @-"},
		{"array merge", "merged into an array"},
		{"stdin conflict note", "@- cannot be combined with -f -"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, ResolverParametersHelp, tt.pattern)
		})
	}
}

// ── resolveVersionConstraintForFile tests ─────────────────────────────────────

func TestResolveVersionConstraintForFile_EmptyConstraint(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		File:              "my-app",
		VersionConstraint: "",
	}
	err := opts.resolveVersionConstraintForFile(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "my-app", opts.File, "should not modify File when constraint is empty")
}

func TestResolveVersionConstraintForFile_InvalidConstraint(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		File:              "my-app",
		VersionConstraint: "not-valid!!",
	}
	err := opts.resolveVersionConstraintForFile(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version constraint")
}

func TestResolveVersionConstraintForFile_ConflictWithAtVersion(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		File:              "my-app@1.0.0",
		VersionConstraint: "^1.0.0",
	}
	err := opts.resolveVersionConstraintForFile(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot use --version with an explicit version")
}

func TestResolveVersionConstraintForFile_RequiresCatalogName(t *testing.T) {
	t.Parallel()

	opts := &sharedResolverOptions{
		File:              "",
		VersionConstraint: "^1.0.0",
	}
	err := opts.resolveVersionConstraintForFile(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--version requires a catalog name")
}

func TestResolveVersionConstraintForFile_RejectsFilePaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
	}{
		{"relative path", "./solution.yaml"},
		{"absolute path", "/tmp/solution.yaml"},
		{"OCI reference", "ghcr.io/org/app"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := &sharedResolverOptions{
				File:              tc.file,
				VersionConstraint: "^1.0.0",
			}
			err := opts.resolveVersionConstraintForFile(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--version can only be used with catalog names")
		})
	}
}
