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

	err := runInstall(ctx, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution path provided")
}

func TestRunInstall_FileNotFound(t *testing.T) {
	t.Parallel()
	ctx := newInstallTestCtx(t)

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      "/nonexistent/path/solution.yaml",
	}

	err := runInstall(ctx, opts)
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

	err = runInstall(ctx, opts)
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

	err = runInstall(ctx, opts)
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
