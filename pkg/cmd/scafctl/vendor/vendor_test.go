// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandVendor(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandVendor(cliParams, ioStreams, "scafctl")
	require.NotNil(t, cmd)
	assert.Equal(t, "vendor", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.True(t, cmd.SilenceUsage)
	assert.Nil(t, cmd.RunE, "parent vendor command should not have RunE")
	subCmds := cmd.Commands()
	require.Len(t, subCmds, 1, "should have 1 subcommand: update")
	assert.Equal(t, "update", subCmds[0].Name())
}

func TestCommandUpdate(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	require.NotNil(t, cmd)
	assert.Equal(t, "update", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandUpdate_Flags(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	tests := []struct {
		name     string
		flagName string
		defVal   string
	}{
		{"dependency", "dependency", "[]"},
		{"dry-run", "dry-run", "false"},
		{"lock-only", "lock-only", "false"},
		{"pre-release", "pre-release", "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := cmd.Flags().Lookup(tt.flagName)
			require.NotNil(t, f, "flag %q should exist", tt.flagName)
			assert.Equal(t, tt.defVal, f.DefValue, "flag %q default value", tt.flagName)
		})
	}
}

func TestCommandUpdate_FileFlag(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")

	f := cmd.Flags().Lookup("file")
	require.NotNil(t, f, "file flag should exist")
	assert.Equal(t, "f", f.Shorthand)
	assert.Equal(t, "", f.DefValue)
}

func TestCommandUpdate_RejectsPositionalArgs(t *testing.T) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	cmd.SetArgs([]string{"solution.yaml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestCommandUpdate_FileNotFound(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	cmd.SetArgs([]string{"-f", "/nonexistent/solution.yaml"})
	cmd.SetContext(writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams)))

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandUpdate_InvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	solPath := dir + "/solution.yaml"
	require.NoError(t, writeTestFile(solPath, "not: valid: solution: yaml: ["))

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	cmd.SetArgs([]string{"-f", solPath})
	cmd.SetContext(writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams)))

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandUpdate_ValidSolutionNoLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := dir + "/solution.yaml"
	solContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`
	require.NoError(t, writeTestFile(solPath, solContent))

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
	cmd.SetArgs([]string{"-f", solPath})
	cmd.SetContext(writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams)))

	// Should fail due to missing lock file, not due to parsing or flag issues
	err := cmd.Execute()
	require.Error(t, err)
	// Verify it got past the file/parse stage
	assert.NotContains(t, err.Error(), "failed to read solution file")
	assert.NotContains(t, err.Error(), "failed to parse solution")
}

// writeTestFile is a helper that writes content to a file path.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

// --- mock and helpers for runVendorUpdateWithFetcher tests ---

type mockCatalogFetcher struct {
	fetchFn func(ctx context.Context, ref string) ([]byte, catalog.ArtifactInfo, error)
}

func (m *mockCatalogFetcher) FetchSolution(ctx context.Context, ref string) ([]byte, catalog.ArtifactInfo, error) {
	if m.fetchFn != nil {
		return m.fetchFn(ctx, ref)
	}
	return nil, catalog.ArtifactInfo{}, fmt.Errorf("fetch not configured")
}

func (m *mockCatalogFetcher) ListSolutions(_ context.Context, _ string) ([]catalog.ArtifactInfo, error) {
	return nil, nil
}

const minimalSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`

func makeTestCtx(t *testing.T) context.Context {
	t.Helper()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
}

func makeTestOpts(solPath string) *UpdateOptions {
	return &UpdateOptions{
		SolutionPath: solPath,
		CliParams:    settings.NewCliParams(),
	}
}

func writeTestLockFile(t *testing.T, dir string, lf *bundler.LockFile) {
	t.Helper()
	require.NoError(t, bundler.WriteLockFile(filepath.Join(dir, bundler.DefaultLockFileName), lf))
}

func contentDigest(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

// --- runVendorUpdateWithFetcher tests ---

func TestRunVendorUpdateWithFetcher_FileNotFound(t *testing.T) {
	t.Parallel()
	ctx := makeTestCtx(t)
	opts := makeTestOpts("/nonexistent/solution.yaml")
	err := runVendorUpdateWithFetcher(ctx, opts, &mockCatalogFetcher{})
	require.Error(t, err)
}

func TestRunVendorUpdateWithFetcher_InvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, "not: valid: ["))

	ctx := makeTestCtx(t)
	err := runVendorUpdateWithFetcher(ctx, makeTestOpts(solPath), &mockCatalogFetcher{})
	require.Error(t, err)
}

func TestRunVendorUpdateWithFetcher_NoLockFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	ctx := makeTestCtx(t)
	err := runVendorUpdateWithFetcher(ctx, makeTestOpts(solPath), &mockCatalogFetcher{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no lock file")
}

func TestRunVendorUpdateWithFetcher_InvalidLockFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	// version: 2 triggers the unsupported version error in LoadLockFile
	require.NoError(t, writeTestFile(filepath.Join(dir, bundler.DefaultLockFileName), "version: 2\n"))

	ctx := makeTestCtx(t)
	err := runVendorUpdateWithFetcher(ctx, makeTestOpts(solPath), &mockCatalogFetcher{})
	require.Error(t, err)
}

func TestRunVendorUpdateWithFetcher_FilterDependencyNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:abc", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	ctx := makeTestCtx(t)
	opts := makeTestOpts(solPath)
	opts.Dependencies = []string{"unknown-dep@9.9.9"}
	err := runVendorUpdateWithFetcher(ctx, opts, &mockCatalogFetcher{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown-dep@9.9.9")
}

func TestRunVendorUpdateWithFetcher_DryRun_AllUpToDate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	depContent := []byte("dep content v1")
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: contentDigest(depContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return depContent, catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.DryRun = true
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))
}

func TestRunVendorUpdateWithFetcher_DryRun_NeedsUpdate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte("updated content"), catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.DryRun = true
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))
}

func TestRunVendorUpdateWithFetcher_ApplyUpdates_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte("updated content"), catalog.ArtifactInfo{}, nil
		},
	}
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), fetcher))

	// Lock file should be rewritten with a valid format
	newLock, err := bundler.LoadLockFile(filepath.Join(dir, bundler.DefaultLockFileName))
	require.NoError(t, err)
	require.NotNil(t, newLock)
	require.Len(t, newLock.Dependencies, 1)
	assert.NotEqual(t, "sha256:oldhash", newLock.Dependencies[0].Digest)
}

func TestRunVendorUpdateWithFetcher_LockOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte("updated content"), catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.LockOnly = true
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))

	// Vendor dir must NOT be created in lock-only mode
	_, statErr := os.Stat(filepath.Join(dir, ".scafctl", "vendor"))
	assert.True(t, os.IsNotExist(statErr), "vendor dir should not be created in lock-only mode")
}

func TestRunVendorUpdateWithFetcher_WithPlugins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Plugins: []bundler.LockPlugin{
			{Name: "azure", Kind: "provider", Version: "1.2.3", Digest: "sha256:pluginhash"},
		},
	})

	opts := makeTestOpts(solPath)
	opts.DryRun = true
	// No deps means no fetch calls; plugin messages should still be printed
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, &mockCatalogFetcher{}))
}

func TestRunVendorUpdateWithFetcher_FetcherError_EntrySkipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:abc", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return nil, catalog.ArtifactInfo{}, fmt.Errorf("catalog unavailable")
		},
	}
	opts := makeTestOpts(solPath)
	opts.DryRun = true
	// CheckForUpdates swallows fetch errors; overall call should succeed
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))
}

func TestRunVendorUpdateWithFetcher_FilteredDependency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	newContent := []byte("new content")
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
			{Ref: "dep-b@2.0.0", Digest: contentDigest(newContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-b@2.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return newContent, catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.DryRun = true
	opts.Dependencies = []string{"dep-a@1.0.0"} // only check dep-a
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))
}

func BenchmarkRunVendorUpdateWithFetcher(b *testing.B) {
	dir := b.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	if err := writeTestFile(solPath, minimalSolutionYAML); err != nil {
		b.Fatal(err)
	}
	depContent := []byte("bench content")
	if err := bundler.WriteLockFile(filepath.Join(dir, bundler.DefaultLockFileName), &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: contentDigest(depContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	}); err != nil {
		b.Fatal(err)
	}
	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return depContent, catalog.ArtifactInfo{}, nil
		},
	}
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
	opts := &UpdateOptions{SolutionPath: solPath, DryRun: true, CliParams: cliParams}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = runVendorUpdateWithFetcher(ctx, opts, fetcher)
	}
}

func BenchmarkCommandVendor(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandVendor(cliParams, ioStreams, "scafctl")
	}
}
