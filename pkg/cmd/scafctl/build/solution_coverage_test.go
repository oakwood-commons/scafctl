// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandBuildSolution_Structure(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}

	cmd := CommandBuildSolution(cliParams, ioStreams, "build")

	assert.Equal(t, "solution", cmd.Use)
	assert.Contains(t, cmd.Aliases, "sol")
	assert.Contains(t, cmd.Aliases, "s")
	assert.Contains(t, cmd.Short, "Build a solution")
}

func TestCommandBuildSolution_Flags(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"name", ""},
		{"version", ""},
		{"tag", ""},
		{"force", "false"},
		{"no-bundle", "false"},
		{"no-vendor", "false"},
		{"bundle-max-size", "50MB"},
		{"dry-run", "false"},
		{"dedupe", "true"},
		{"dedupe-threshold", "4KB"},
		{"no-cache", "false"},
		{"skip-lint", "false"},
		{"skip-tests", "false"},
		{"ignore-preflight", "false"},
		{"allow-dev-version", "false"},
	}

	for _, tc := range flagTests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			flag := cmd.Flags().Lookup(tc.name)
			require.NotNil(t, flag, "flag %q should exist", tc.name)
			assert.Equal(t, tc.defValue, flag.DefValue)
		})
	}
}

func TestCommandBuildSolution_RequiresArgs(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	cmd.SetArgs([]string{}) // no args

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no -f/--file specified")
}

func TestCommandBuildSolution_TagConflictsWithName(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	cmd.SetContext(writer.WithWriter(t.Context(), w))
	cmd.SetArgs([]string{"-f", "solution.yaml", "--tag", "my-sol@1.0.0", "--name", "other"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used together")
}

func TestCommandBuildSolution_TagConflictsWithVersion(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	cmd.SetContext(writer.WithWriter(t.Context(), w))
	cmd.SetArgs([]string{"-f", "solution.yaml", "--tag", "my-sol@1.0.0", "--version", "2.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used together")
}

func TestCommandBuildSolution_TagShorthand(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandBuildSolution(cliParams, ioStreams, "build")

	f := cmd.Flags().ShorthandLookup("t")
	require.NotNil(t, f, "shorthand -t should exist")
	assert.Equal(t, "tag", f.Name)
}

func TestRunBuildSolution_FileNotFound(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:      "/nonexistent/path/solution.yaml",
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := runBuildSolution(ctx, opts)
	require.Error(t, err)
}

func TestRunBuildSolution_InvalidSolution(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "bad-solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte("this is not valid yaml: [[["), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:      solFile,
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := runBuildSolution(ctx, opts)
	require.Error(t, err)
}

func TestRunBuildSolution_NoVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:      solFile,
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := runBuildSolution(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dev version not allowed")
}

func TestRunBuildSolution_NoVersion_AllowDevVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:            solFile,
		IOStreams:       ioStreams,
		CliParams:       settings.NewCliParams(),
		AllowDevVersion: true,
		DryRun:          true, BundleMaxSize: "50MB",
	}

	// With AllowDevVersion + DryRun, the build should proceed past the version check
	err := runBuildSolution(ctx, opts)
	require.NoError(t, err)
}

func TestRunBuildSolution_PreflightBlocked(t *testing.T) {
	t.Parallel()

	// Create a solution with a version but an empty spec so lint blocks it.
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:          solFile,
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		SkipTests:     true,
		BundleMaxSize: "50MB",
	}

	err := runBuildSolution(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pre-flight checks failed")
}

func TestRunBuildSolution_PreflightSkipLint(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:          solFile,
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		SkipLint:      true,
		SkipTests:     true,
		BundleMaxSize: "50MB",
	}

	// With lint and tests skipped, preflight runs but skips both checks.
	// Subsequent pipeline steps may fail; we only care about the preflight path.
	_ = runBuildSolution(ctx, opts)
	output := buf.String()
	assert.Contains(t, output, "lint: skipped")
	assert.Contains(t, output, "tests: skipped")
}

func TestRunBuildSolution_PreflightIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:            solFile,
		IOStreams:       ioStreams,
		CliParams:       settings.NewCliParams(),
		SkipTests:       true,
		IgnorePreflight: true,
		BundleMaxSize:   "50MB",
	}

	// With IgnorePreflight, lint errors become warnings and the build is not blocked.
	// Subsequent pipeline steps may fail; we only care about the preflight path.
	_ = runBuildSolution(ctx, opts)
	output := buf.String()
	assert.Contains(t, output, "lint:")
	assert.NotContains(t, output, "build blocked")
}

func TestRunBuildSolution_DryRunSkipsPreflight(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:          solFile,
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		DryRun:        true,
		BundleMaxSize: "50MB",
	}

	// Dry-run should skip preflight entirely — no lint/tests messages in output.
	err := runBuildSolution(ctx, opts)
	require.NoError(t, err)
	output := buf.String()
	assert.NotContains(t, output, "lint:")
	assert.NotContains(t, output, "tests:")
	assert.Contains(t, output, "Dry run:")
}

func TestRunBuildSolution_DevVersionErrorMessage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:      solFile,
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	err := runBuildSolution(ctx, opts)
	require.Error(t, err)
	// The error message should use the configured binary name, not "scafctl"
	assert.Contains(t, buf.String(), "mycli build solution --allow-dev-version")
}

func TestPrintDryRunOutput_StaticFiles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{
		LocalFiles: []bundler.FileEntry{
			{RelPath: "scripts/deploy.sh", Source: bundler.StaticAnalysis},
			{RelPath: "data/config.json", Source: bundler.ExplicitInclude},
			{RelPath: "tests/test.yaml", Source: bundler.TestInclude},
		},
	}

	sol := &solution.Solution{}
	opts := &SolutionOptions{File: "solution.yaml"}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "scripts/deploy.sh")
	assert.Contains(t, output, "data/config.json")
	assert.Contains(t, output, "tests/test.yaml")
	assert.Contains(t, output, "Static analysis discovered")
	assert.Contains(t, output, "Explicit includes")
	assert.Contains(t, output, "Test file includes")
	assert.Contains(t, output, "3 bundled file(s)")
}

func TestPrintDryRunOutput_CatalogRefs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{
		CatalogRefs: []bundler.CatalogRefEntry{
			{Ref: "my-dep@1.0.0", VendorPath: ".vendor/my-dep"},
		},
	}

	sol := &solution.Solution{}
	opts := &SolutionOptions{File: "solution.yaml"}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "my-dep@1.0.0")
	assert.Contains(t, output, ".vendor/my-dep")
	assert.Contains(t, output, "1 vendored dependency")
}

func TestPrintDryRunOutput_CatalogRefsNotVendored(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{
		CatalogRefs: []bundler.CatalogRefEntry{
			{Ref: "dep@2.0.0", VendorPath: ".vendor/dep"},
		},
	}

	sol := &solution.Solution{}
	opts := &SolutionOptions{File: "solution.yaml", NoVendor: true}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "not vendored")
}

func TestPrintDryRunOutput_ComposedFiles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{}
	sol := &solution.Solution{}
	sol.Compose = []string{"resolvers.yaml", "actions.yaml"}
	opts := &SolutionOptions{File: "solution.yaml"}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "Composed files")
	assert.Contains(t, output, "resolvers.yaml")
	assert.Contains(t, output, "actions.yaml")
	assert.Contains(t, output, "merged into solution")
}

func TestPrintDryRunOutput_Plugins(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{}
	sol := &solution.Solution{}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "my-plugin", Kind: "provider", Version: "1.0.0"},
	}
	opts := &SolutionOptions{File: "solution.yaml"}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "Plugin dependencies")
	assert.Contains(t, output, "my-plugin")
	assert.Contains(t, output, "provider")
	assert.Contains(t, output, "1 plugin(s)")
}

func TestPrintDryRunOutput_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{}
	sol := &solution.Solution{}
	opts := &SolutionOptions{File: "solution.yaml"}

	printDryRunOutput(w, discovery, sol, opts)

	output := buf.String()
	assert.Contains(t, output, "0 bundled file(s)")
}

// Benchmarks

func BenchmarkPrintDryRunOutput(b *testing.B) {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	discovery := &bundler.DiscoveryResult{
		LocalFiles: []bundler.FileEntry{
			{RelPath: "scripts/deploy.sh", Source: bundler.StaticAnalysis},
			{RelPath: "data/config.json", Source: bundler.ExplicitInclude},
		},
	}
	sol := &solution.Solution{}
	opts := &SolutionOptions{File: "solution.yaml"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		printDryRunOutput(w, discovery, sol, opts)
	}
}

func BenchmarkCommandBuildSolution(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandBuildSolution(cliParams, ioStreams, "build")
	}
}
