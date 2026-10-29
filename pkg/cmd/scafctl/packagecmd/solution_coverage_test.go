// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package packagecmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandPackageSolution_Structure(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}

	cmd := CommandPackageSolution(cliParams, ioStreams, "build")

	assert.Equal(t, "solution", cmd.Use)
	assert.Contains(t, cmd.Aliases, "sol")
	assert.Contains(t, cmd.Aliases, "s")
	assert.Contains(t, cmd.Short, "Package a solution")
}

func TestCommandPackageSolution_Flags(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")

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
		{"allow-dirty", "false"},
		{"no-git-metadata", "false"},
		{"base-dir", ""},
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

func TestCommandPackageSolution_RequiresArgs(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	cmd.SetArgs([]string{}) // no args

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution path provided")
}

func TestCommandPackageSolution_AmbiguousDiscovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"solution.yaml", "scafctl.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("apiVersion: scafctl.io/v1\n"), 0o600))
	}

	ioStreams, _, errOut := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	ctx := writer.WithWriter(t.Context(), writer.New(ioStreams, cliParams))
	cmd.SetContext(provider.WithWorkingDirectory(ctx, dir))
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.ErrorIs(t, err, load.ErrAmbiguousSolution)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, errOut.String(), "use -f/--file")
}

func TestCommandPackageSolution_TagConflictsWithName(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	cmd.SetContext(writer.WithWriter(t.Context(), w))
	cmd.SetArgs([]string{"-f", "solution.yaml", "--tag", "my-sol@1.0.0", "--name", "other"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used together")
}

func TestCommandPackageSolution_TagConflictsWithVersion(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	cmd.SetContext(writer.WithWriter(t.Context(), w))
	cmd.SetArgs([]string{"-f", "solution.yaml", "--tag", "my-sol@1.0.0", "--version", "2.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tag cannot be used together")
}

func TestCommandPackageSolution_TagShorthand(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")

	f := cmd.Flags().ShorthandLookup("t")
	require.NotNil(t, f, "shorthand -t should exist")
	assert.Equal(t, "tag", f.Name)
}

func TestRunPackageSolution_FileNotFound(t *testing.T) {
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

	err := runPackageSolution(ctx, opts)
	require.Error(t, err)
}

func TestRunPackageSolution_InvalidSolution(t *testing.T) {
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

	err := runPackageSolution(ctx, opts)
	require.Error(t, err)
}

func TestRunPackageSolution_NoVersion(t *testing.T) {
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
		Version:   "0.0.0-dev",
		IOStreams: ioStreams,
		CliParams: settings.NewCliParams(),
	}

	err := runPackageSolution(ctx, opts)
	require.ErrorIs(t, err, builder.ErrDevVersion)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestRunPackageSolution_NoVersion_AllowDevVersion(t *testing.T) {
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

	// A solution without a version is auto-incremented, so the dry run
	// proceeds without --allow-dev-version.
	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Auto-incremented version to 0.1.0")
	assert.Contains(t, buf.String(), "Dry run: would build test-solution@0.1.0")
}

func TestRunPackageSolution_PreflightBlocked(t *testing.T) {
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

	err := runPackageSolution(ctx, opts)
	require.ErrorIs(t, err, builder.ErrPreflightBlocked)
	assert.Contains(t, buf.String(), "--ignore-preflight to override")
}

func TestRunPackageSolution_PreflightSkipLint(t *testing.T) {
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
	_ = runPackageSolution(ctx, opts)
	output := buf.String()
	assert.Contains(t, output, "lint: skipped")
	assert.Contains(t, output, "tests: skipped")
}

func TestRunPackageSolution_PreflightIgnored(t *testing.T) {
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
	_ = runPackageSolution(ctx, opts)
	output := buf.String()
	assert.Contains(t, output, "lint:")
	assert.NotContains(t, output, "build blocked")
}

func TestRunPackageSolution_DryRunSkipsPreflight(t *testing.T) {
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
	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	output := buf.String()
	assert.NotContains(t, output, "lint:")
	assert.NotContains(t, output, "tests:")
	assert.Contains(t, output, "Dry run:")
}

func TestRunPackageSolution_DevVersionErrorMessage(t *testing.T) {
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
		Version:   "0.0.0-dev",
		IOStreams: ioStreams,
		CliParams: cliParams,
	}

	err := runPackageSolution(ctx, opts)
	require.Error(t, err)
	// The error message should use the configured binary name, not "scafctl"
	assert.Contains(t, buf.String(), "mycli package solution --allow-dev-version")
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

	printDryRunOutput(w, dryRunView{file: "solution.yaml", solution: &solution.Solution{}, discovery: discovery})

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

	printDryRunOutput(w, dryRunView{file: "solution.yaml", solution: &solution.Solution{}, discovery: discovery})

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

	printDryRunOutput(w, dryRunView{file: "solution.yaml", solution: &solution.Solution{}, discovery: discovery, noVendor: true})

	output := buf.String()
	assert.Contains(t, output, "not vendored")
}

func TestPrintDryRunOutput_ComposedFiles(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())

	printDryRunOutput(w, dryRunView{
		file:      "solution.yaml",
		solution:  &solution.Solution{},
		compose:   []string{"resolvers.yaml", "actions.yaml"},
		discovery: &bundler.DiscoveryResult{},
	})

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

	printDryRunOutput(w, dryRunView{file: "solution.yaml", solution: sol, discovery: discovery})

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

	printDryRunOutput(w, dryRunView{file: "solution.yaml", solution: &solution.Solution{}, discovery: &bundler.DiscoveryResult{}})

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
	view := dryRunView{file: "solution.yaml", solution: &solution.Solution{}, discovery: discovery}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		printDryRunOutput(w, view)
	}
}

func BenchmarkCommandPackageSolution(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := &settings.Run{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandPackageSolution(cliParams, ioStreams, "build")
	}
}

func TestRunPackageSolution_StdinDryRun(t *testing.T) {
	t.Parallel()

	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-solution
  version: 1.0.0
spec: {}
`
	stdinBuf := io.NopCloser(bytes.NewBufferString(content))
	var outBuf bytes.Buffer
	ioStreams := terminal.NewIOStreams(stdinBuf, &outBuf, &outBuf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:          "-",
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		DryRun:        true,
		NoBundle:      true,
		NoGitMetadata: true,
		BundleMaxSize: "50MB",
	}

	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "Dry run:")
	assert.Contains(t, outBuf.String(), "stdin-solution@1.0.0")
}

func TestRunPackageSolution_BaseDirOverridesBundleRoot(t *testing.T) {
	t.Parallel()

	// Create solution file in one directory and a separate base-dir
	solDir := t.TempDir()
	baseDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: basedir-test
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
		BaseDir:       baseDir,
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		DryRun:        true,
		BundleMaxSize: "50MB",
	}

	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Dry run:")
	assert.Contains(t, buf.String(), "basedir-test@1.0.0")
}

func TestRunPackageSolution_StdinImpliesNoBundle(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cmd := CommandPackageSolution(cliParams, ioStreams, "build")
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"-f", "-"})

	// We can't easily test the full RunE (stdin would be empty),
	// but we can verify that the command creates correctly and the
	// flag description mentions stdin.
	f := cmd.Flags().Lookup("file")
	require.NotNil(t, f)
	assert.Contains(t, f.Usage, "stdin")
}

func TestRunBuildSolution_DirtyTreeWarns(t *testing.T) {
	t.Parallel()

	// Create a git repo with a dirty file.
	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, string(out))
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "test")
	runGit("config", "commit.gpgsign", "false")

	solFile := filepath.Join(dir, "solution.yaml")
	content := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: dirty-test\n  version: 1.0.0\nspec: {}\n"
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	// Make it dirty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("dirty"), 0o644))

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

	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dirty")
}

func TestRunBuildSolution_AllowDirtyProceeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, string(out))
	}
	runGit("init", "-b", "main")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "test")
	runGit("config", "commit.gpgsign", "false")

	solFile := filepath.Join(dir, "solution.yaml")
	content := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: dirty-ok\n  version: 1.0.0\nspec: {}\n"
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))
	runGit("add", ".")
	runGit("commit", "-m", "initial")

	// Make dirty.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("dirty"), 0o644))

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &SolutionOptions{
		File:          solFile,
		IOStreams:     ioStreams,
		CliParams:     settings.NewCliParams(),
		DryRun:        true,
		AllowDirty:    true,
		BundleMaxSize: "50MB",
	}

	err := runPackageSolution(ctx, opts)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dirty-ok@1.0.0")
}
