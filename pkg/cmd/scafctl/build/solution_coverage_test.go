// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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

	assert.Equal(t, "solution [file]", cmd.Use)
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
		{"force", "false"},
		{"no-bundle", "false"},
		{"no-vendor", "false"},
		{"bundle-max-size", "50MB"},
		{"dry-run", "false"},
		{"dedupe", "true"},
		{"dedupe-threshold", "4KB"},
		{"no-cache", "false"},
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
	assert.Contains(t, err.Error(), "accepts 1 arg")
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
