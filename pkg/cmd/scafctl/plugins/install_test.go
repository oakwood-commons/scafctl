// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newInstallTestCtx(t testing.TB) context.Context {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	lgr := logr.Discard()
	ctx = logger.WithLogger(ctx, &lgr)
	return ctx
}

func TestRunInstall_NoFileProvided_NoAutoDiscover(t *testing.T) {
	t.Parallel()

	// Use a temp dir with no solution files so auto-discovery fails
	dir := t.TempDir()
	oldWd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(oldWd) }()

	ctx := newInstallTestCtx(t)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      "",
	}

	err := runInstall(ctx, opts, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin names or solution file provided")
}

func TestRunInstall_FileNotFound(t *testing.T) {
	t.Parallel()
	ctx := newInstallTestCtx(t)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      "/nonexistent/path/solution.yaml",
	}

	err := runInstall(ctx, opts, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading solution")
}

func TestRunInstall_InvalidYAML(t *testing.T) {
	t.Parallel()
	ctx := newInstallTestCtx(t)

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	err := os.WriteFile(solFile, []byte("not: [valid: yaml: {{{{"), 0o644)
	require.NoError(t, err)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      solFile,
	}

	err = runInstall(ctx, opts, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing solution")
}

func TestRunInstall_NoPluginsDeclared(t *testing.T) {
	t.Parallel()
	ctx := newInstallTestCtx(t)

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
spec:
  resolvers: {}
`
	err := os.WriteFile(solFile, []byte(content), 0o644)
	require.NoError(t, err)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      solFile,
	}

	err = runInstall(ctx, opts, nil)
	require.NoError(t, err, "no plugins declared should succeed with no-op")
}

func TestLoadSolution_Success(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-sol
spec:
  resolvers: {}
`
	err := os.WriteFile(solFile, []byte(content), 0o644)
	require.NoError(t, err)

	sol, err := loadSolution(solFile)
	require.NoError(t, err)
	assert.Equal(t, "test-sol", sol.Metadata.Name)
}

func TestLoadSolution_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := loadSolution("/nonexistent/solution.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading solution")
}

func TestLoadSolution_InvalidContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	solFile := filepath.Join(dir, "bad.yaml")
	err := os.WriteFile(solFile, []byte("{{{{not yaml"), 0o644)
	require.NoError(t, err)

	_, err = loadSolution(solFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing solution")
}

func TestResolveStandalonePlugins_OfficialProvider(t *testing.T) {
	t.Parallel()

	deps, err := resolveStandalonePlugins(t.Context(), []string{"github"}, "provider", "")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "provider", string(deps[0].Kind))
	assert.NotEmpty(t, deps[0].Name)
}

func TestResolveStandalonePlugins_MultiplePlugins(t *testing.T) {
	t.Parallel()

	deps, err := resolveStandalonePlugins(t.Context(), []string{"github", "exec", "env"}, "provider", "")
	require.NoError(t, err)
	assert.Len(t, deps, 3)
}

func TestResolveStandalonePlugins_VersionOverride(t *testing.T) {
	t.Parallel()

	deps, err := resolveStandalonePlugins(t.Context(), []string{"github"}, "provider", ">=0.3.0")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, ">=0.3.0", deps[0].Version)
}

func TestResolveStandalonePlugins_InlineVersion(t *testing.T) {
	t.Parallel()

	deps, err := resolveStandalonePlugins(t.Context(), []string{"github@0.2.0"}, "provider", "")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "0.2.0", deps[0].Version)
}

func TestResolveStandalonePlugins_UnknownPlugin(t *testing.T) {
	t.Parallel()

	// Unknown names should still produce a dependency (catalog will resolve or fail)
	deps, err := resolveStandalonePlugins(t.Context(), []string{"my-custom-plugin"}, "provider", "")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "my-custom-plugin", deps[0].Name)
	assert.Equal(t, "provider", string(deps[0].Kind))
}

func TestResolveStandalonePlugins_AuthHandler(t *testing.T) {
	t.Parallel()

	deps, err := resolveStandalonePlugins(t.Context(), []string{"github"}, "auth-handler", "")
	require.NoError(t, err)
	require.Len(t, deps, 1)
	assert.Equal(t, "auth-handler", string(deps[0].Kind))
}

func TestResolveStandalonePlugins_InvalidKind(t *testing.T) {
	t.Parallel()

	_, err := resolveStandalonePlugins(t.Context(), []string{"github"}, "invalid-kind", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid plugin kind")
}

func TestParseNameVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		name    string
		version string
	}{
		{"github", "github", ""},
		{"github@0.2.0", "github", "0.2.0"},
		{"github@>=0.3.0", "github", ">=0.3.0"},
		{"my-plugin@latest", "my-plugin", "latest"},
		{"@invalid", "@invalid", ""}, // edge: no name before @
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			name, version := parseNameVersion(tt.input)
			assert.Equal(t, tt.name, name)
			assert.Equal(t, tt.version, version)
		})
	}
}

func TestRunInstall_DryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	ctx := writer.WithWriter(t.Context(), w)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: ioStreams,
		DryRun:    true, Kind: "provider",
	}

	err := runInstall(ctx, opts, []string{"github", "exec"})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Dry run")
	assert.Contains(t, output, "github")
	assert.Contains(t, output, "exec")
}
