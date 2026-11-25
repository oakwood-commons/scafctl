// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package vendor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
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
	assert.NotNil(t, cmd.RunE, "parent vendor command needs a help-only RunE so NoArgs rejects unknown subcommands")
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

func TestCommandUpdate_EmbedderBinaryName(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandUpdate(cliParams, ioStreams, "mycli/vendor")
	assert.Contains(t, cmd.Long, "mycli vendor update")
	assert.NotContains(t, cmd.Long, "scafctl vendor update")
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
		{"output", "output", ""},
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

	outFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outFlag)
	assert.Equal(t, "o", outFlag.Shorthand)
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

func TestCommandUpdate_ValidSolutionNoLock_PackageSolutionHint(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, errOut := terminal.NewTestIOStreams()
	ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
	opts := &UpdateOptions{
		SolutionPath: solPath,
		CliParams:    cliParams,
		IOStreams:    ioStreams,
	}

	err := runVendorUpdateWithFetcher(ctx, opts, &mockCatalogFetcher{})
	require.Error(t, err)
	assert.Contains(t, errOut.String(), "run 'mycli package solution' first")
	assert.NotContains(t, errOut.String(), "build solution")
}

// writeTestFile is a helper that writes content to a file path.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestCommandUpdate_AutoDiscover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		withSolution bool
		wantReadErr  bool
	}{
		{name: "discovered solution is read", withSolution: true},
		{name: "nothing discovered falls back to the default path", wantReadErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.withSolution {
				require.NoError(t, writeTestFile(filepath.Join(dir, "solution.yaml"), minimalSolutionYAML))
			}

			cliParams := settings.NewCliParams()
			ioStreams, _, errOut := terminal.NewTestIOStreams()
			cmd := CommandUpdate(cliParams, ioStreams, "scafctl/vendor")
			cmd.SetArgs([]string{})
			ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
			cmd.SetContext(provider.WithWorkingDirectory(ctx, dir))

			_ = cmd.Execute()
			if tt.wantReadErr {
				assert.Contains(t, errOut.String(), "failed to read solution file")
				assert.Contains(t, errOut.String(), defaultSolutionPath)
				return
			}
			assert.NotContains(t, errOut.String(), "failed to read solution file")
			assert.NotContains(t, errOut.String(), "failed to parse solution")
		})
	}
}

// --- mock and helpers for runVendorUpdateWithFetcher / runVendorUpdateWithDeps tests ---

type mockCatalogFetcher struct {
	fetchFn func(ctx context.Context, ref string) ([]byte, catalog.ArtifactInfo, error)
	listFn  func(ctx context.Context, name string) ([]catalog.ArtifactInfo, error)
}

func (m *mockCatalogFetcher) FetchSolution(ctx context.Context, ref string) ([]byte, catalog.ArtifactInfo, error) {
	if m.fetchFn != nil {
		return m.fetchFn(ctx, ref)
	}
	return nil, catalog.ArtifactInfo{}, fmt.Errorf("fetch not configured")
}

func (m *mockCatalogFetcher) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	if m.listFn != nil {
		return m.listFn(ctx, name)
	}
	return nil, nil
}

type fakePluginSource struct {
	name    string
	version string
	digest  string
}

func (f *fakePluginSource) Name() string { return f.name }

func (f *fakePluginSource) FetchByPlatform(_ context.Context, _ catalog.Reference, _ string) ([]byte, catalog.ArtifactInfo, error) {
	return nil, catalog.ArtifactInfo{}, fmt.Errorf("unexpected binary download")
}

func (f *fakePluginSource) ListPlatforms(_ context.Context, _ catalog.Reference) ([]string, error) {
	return []string{"linux/amd64", "darwin/arm64"}, nil
}

func (f *fakePluginSource) ResolveContentDigest(_ context.Context, ref catalog.Reference, _, _ string) (catalog.ContentDigestInfo, error) {
	return catalog.ContentDigestInfo{
		ArtifactInfo: catalog.ArtifactInfo{
			Reference: catalog.Reference{Kind: ref.Kind, Name: ref.Name, Version: semver.MustParse(f.version)},
			Catalog:   f.name,
			Canonical: "ghcr.io/acme",
		},
		ContentDigest: f.digest,
	}, nil
}

func (f *fakePluginSource) Find(_ context.Context, q catalog.ArtifactQuery) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{
		Reference: catalog.Reference{Kind: q.Kind, Name: q.Name, Version: semver.MustParse(f.version)},
		Catalog:   f.name,
		Canonical: "ghcr.io/acme",
	}, nil
}

const minimalSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec: {}
`

const depASolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: dep-a@1.0.0
`

const depABSolutionYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers:
    childA:
      resolve:
        with:
          - provider: solution
            inputs:
              source: dep-a@1.0.0
    childB:
      resolve:
        with:
          - provider: solution
            inputs:
              source: dep-b@2.0.0
`

func makeTestCtx(t *testing.T) context.Context {
	t.Helper()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
}

func makeTestOpts(solPath string) *UpdateOptions {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	return &UpdateOptions{
		SolutionPath: solPath,
		CliParams:    settings.NewCliParams(),
		IOStreams:    ioStreams,
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

func TestRunVendorUpdateWithFetcher_InvalidOutputFlag(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	opts := makeTestOpts(solPath)
	opts.Output = "xml"
	err := runVendorUpdateWithFetcher(makeTestCtx(t), opts, &mockCatalogFetcher{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid output type")
}

func TestRunVendorUpdateWithFetcher_NoLockFile_EmptyBinaryNameFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = ""
	ioStreams, _, errOut := terminal.NewTestIOStreams()
	ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))
	opts := &UpdateOptions{
		SolutionPath: solPath,
		CliParams:    cliParams,
		IOStreams:    ioStreams,
	}

	err := runVendorUpdateWithFetcher(ctx, opts, &mockCatalogFetcher{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no lock file")
	assert.Contains(t, errOut.String(), "run 'scafctl package solution' first")
}

func TestRunVendorUpdateWithFetcher_InvalidLockFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	require.NoError(t, writeTestFile(filepath.Join(dir, bundler.DefaultLockFileName), "version: 2\n"))

	ctx := makeTestCtx(t)
	err := runVendorUpdateWithFetcher(ctx, makeTestOpts(solPath), &mockCatalogFetcher{})
	require.Error(t, err)
}

func TestRunVendorUpdateWithFetcher_FilterDependencyNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))
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
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))

	depContent := []byte(minimalSolutionYAML + "# v1\n")
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: contentDigest(depContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
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

func TestRunVendorUpdateWithFetcher_NoOpSkipsLockRewrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))

	depContent := []byte(minimalSolutionYAML + "# v1\n")
	lockPath := filepath.Join(dir, bundler.DefaultLockFileName)
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: contentDigest(depContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	past := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	require.NoError(t, os.Chtimes(lockPath, past, past))
	beforeInfo, err := os.Stat(lockPath)
	require.NoError(t, err)

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return depContent, catalog.ArtifactInfo{}, nil
		},
	}
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), fetcher))

	afterInfo, err := os.Stat(lockPath)
	require.NoError(t, err)
	assert.Equal(t, beforeInfo.ModTime(), afterInfo.ModTime(), "solution.lock must not be rewritten when 0 updates apply")
}

func TestRunVendorUpdateWithFetcher_PrunesUnmatchedLockAndVendorFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".scafctl", "vendor"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scafctl", "vendor", "dep-a@1.0.0.yaml"), []byte(minimalSolutionYAML), 0o600))

	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: contentDigest([]byte(minimalSolutionYAML)), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), &mockCatalogFetcher{}))

	newLock, err := bundler.LoadLockFile(filepath.Join(dir, bundler.DefaultLockFileName))
	require.NoError(t, err)
	assert.Empty(t, newLock.Dependencies, "unused lock entries must be pruned")
	assert.NoFileExists(t, filepath.Join(dir, ".scafctl", "vendor", "dep-a@1.0.0.yaml"))
}

func TestRunVendorUpdateWithFetcher_ComposeAndStructuredJSONOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	rootYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: composed-solution
  version: 1.0.0
compose:
  - resolvers.yaml
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^1.0.0
spec:
  workflow:
    actions:
      runChild:
        provider: static
        dependsOn:
          - child
        inputs:
          value: ok
`
	partialYAML := `spec:
  resolvers:
    child:
      resolve:
        with:
          - provider: solution
            inputs:
              source: dep-a@1.0.0
`
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, rootYAML))
	require.NoError(t, writeTestFile(filepath.Join(dir, "resolvers.yaml"), partialYAML))

	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
		Plugins: []bundler.LockPlugin{
			{
				Name:              "exec",
				Kind:              "provider",
				Version:           "1.1.0",
				Constraint:        "^1.0.0",
				Digest:            "sha256:old-plugin",
				ResolvedFrom:      "acme",
				ResolvedCanonical: "ghcr.io/acme",
			},
		},
	})

	src := &fakePluginSource{name: "acme", version: "1.2.0", digest: "sha256:new-plugin"}
	pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, src)
	require.NoError(t, err)
	aliasIdx := catalogindex.FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{{Name: "acme", URL: "oci://ghcr.io/acme"}},
	})

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))

	opts := &UpdateOptions{
		SolutionPath: solPath,
		DryRun:       true,
		Output:       "json",
		CliParams:    cliParams,
		IOStreams:    ioStreams,
	}

	deps := vendorUpdateDeps{
		solutionFetcher: &mockCatalogFetcher{
			fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
				return []byte(minimalSolutionYAML + "# updated composed dep\n"), catalog.ArtifactInfo{}, nil
			},
		},
		buildPluginOpts: func(_ context.Context, _ *writer.Writer) (bundler.VendorAllPluginsOptions, error) {
			return bundler.VendorAllPluginsOptions{
				CatalogResolver: aliasIdx,
				PluginFetcher:   pf,
				Platform:        "linux/amd64",
			}, nil
		},
	}

	require.NoError(t, runVendorUpdateWithDeps(ctx, opts, deps))

	// Human output goes to stderr with proper line breaks; stdout contains ONLY valid JSON.
	assert.Contains(t, errBuf.String(), "Checking vendored dependencies")
	assert.Contains(t, errBuf.String(), "\n  dep-a@1.0.0:\n")
	assert.Contains(t, errBuf.String(), "\n  exec (provider):\n")
	trimmed := strings.TrimSpace(outBuf.String())
	require.True(t, json.Valid([]byte(trimmed)), "stdout must be valid JSON, got: %s", outBuf.String())

	var report bundler.VendorUpdateResult
	require.NoError(t, json.Unmarshal([]byte(trimmed), &report))
	assert.True(t, report.DryRun)
	assert.Equal(t, 2, report.UpdateCount)
	require.Len(t, report.Entries, 1)
	assert.Equal(t, "dep-a@1.0.0", report.Entries[0].Ref)
	require.Len(t, report.PluginEntries, 1)
	assert.Equal(t, "exec", report.PluginEntries[0].Name)
	assert.Equal(t, "1.2.0", report.PluginEntries[0].LatestVersion)
}

func TestRunVendorUpdateWithDeps_NonDryRunPluginApplyAndErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("applies plugin update to solution.lock and emits YAML report", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plugin-apply
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^1.0.0
spec: {}
`
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, writeTestFile(solPath, solYAML))
		writeTestLockFile(t, dir, &bundler.LockFile{
			Plugins: []bundler.LockPlugin{
				{
					Name:              "exec",
					Kind:              "provider",
					Version:           "1.1.0",
					Constraint:        "^1.0.0",
					Digest:            "sha256:old-plugin",
					ResolvedFrom:      "acme",
					ResolvedCanonical: "ghcr.io/acme",
				},
				{
					Name:         "orphan",
					Kind:         "provider",
					Version:      "0.1.0",
					Digest:       "sha256:orphan",
					ResolvedFrom: "acme",
				},
			},
		})

		src := &fakePluginSource{name: "acme", version: "1.2.0", digest: "sha256:new-plugin"}
		pf, err := catalog.NewPluginFetcher(logr.Discard(), nil, src)
		require.NoError(t, err)
		aliasIdx := catalogindex.FromConfig(&config.Config{
			Catalogs: []config.CatalogConfig{{Name: "acme", URL: "oci://ghcr.io/acme"}},
		})

		cliParams := settings.NewCliParams()
		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		ctx := writer.WithWriter(context.Background(), writer.New(ioStreams, cliParams))

		opts := &UpdateOptions{
			SolutionPath: solPath,
			Output:       "yaml",
			CliParams:    cliParams,
			IOStreams:    ioStreams,
		}
		deps := vendorUpdateDeps{
			solutionFetcher: &mockCatalogFetcher{},
			buildPluginOpts: func(_ context.Context, _ *writer.Writer) (bundler.VendorAllPluginsOptions, error) {
				return bundler.VendorAllPluginsOptions{
					CatalogResolver: aliasIdx,
					PluginFetcher:   pf,
					Platform:        "linux/amd64",
				}, nil
			},
		}

		require.NoError(t, runVendorUpdateWithDeps(ctx, opts, deps))
		assert.Contains(t, errBuf.String(), "Updated solution.lock")
		assert.Contains(t, outBuf.String(), "latestVersion: 1.2.0")

		updatedLock, err := bundler.LoadLockFile(filepath.Join(dir, bundler.DefaultLockFileName))
		require.NoError(t, err)
		require.Len(t, updatedLock.Plugins, 1, "exec upgraded and unused orphan plugin pruned on full update")
		assert.Equal(t, "1.2.0", updatedLock.Plugins[0].Version)
	})

	t.Run("plugin resolver initialization error returns CatalogError", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plugin-err
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^1.0.0
spec: {}
`
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, writeTestFile(solPath, solYAML))
		writeTestLockFile(t, dir, &bundler.LockFile{
			Plugins: []bundler.LockPlugin{{Name: "exec", Kind: "provider", Version: "1.0.0", Digest: "sha256:old"}},
		})

		deps := vendorUpdateDeps{
			solutionFetcher: &mockCatalogFetcher{},
			buildPluginOpts: func(_ context.Context, _ *writer.Writer) (bundler.VendorAllPluginsOptions, error) {
				return bundler.VendorAllPluginsOptions{}, fmt.Errorf("catalog init boom")
			},
		}
		err := runVendorUpdateWithDeps(makeTestCtx(t), makeTestOpts(solPath), deps)
		require.ErrorContains(t, err, "catalog init boom")
	})

	t.Run("symlinked solution.lock is rejected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		solPath := filepath.Join(dir, "solution.yaml")
		require.NoError(t, writeTestFile(solPath, minimalSolutionYAML))
		target := filepath.Join(dir, "other.yaml")
		require.NoError(t, writeTestFile(target, "version: 1\n"))
		require.NoError(t, os.Symlink("other.yaml", filepath.Join(dir, bundler.DefaultLockFileName)))

		err := runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), &mockCatalogFetcher{})
		require.ErrorContains(t, err, "symlink")
	})
}

func TestRunVendorUpdateWithFetcher_DryRun_NeedsUpdate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte(minimalSolutionYAML + "# updated\n"), catalog.ArtifactInfo{}, nil
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
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte(minimalSolutionYAML + "# updated\n"), catalog.ArtifactInfo{}, nil
		},
	}
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), fetcher))

	newLock, err := bundler.LoadLockFile(filepath.Join(dir, bundler.DefaultLockFileName))
	require.NoError(t, err)
	require.NotNil(t, newLock)
	require.Len(t, newLock.Dependencies, 1)
	assert.NotEqual(t, "sha256:oldhash", newLock.Dependencies[0].Digest)
	assert.Equal(t, "1.0.0", newLock.Dependencies[0].ResolvedVersion)
}

func TestRunVendorUpdateWithFetcher_LockOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return []byte(minimalSolutionYAML + "# updated\n"), catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.LockOnly = true
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))

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
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, &mockCatalogFetcher{}))
}

func TestRunVendorUpdateWithFetcher_FetcherError_FailsAtomicallyBeforeDiskWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depASolutionYAML))
	initialLock := &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: "sha256:abc", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
		},
	}
	writeTestLockFile(t, dir, initialLock)

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return nil, catalog.ArtifactInfo{}, fmt.Errorf("catalog unavailable")
		},
	}
	err := runVendorUpdateWithFetcher(makeTestCtx(t), makeTestOpts(solPath), fetcher)
	require.ErrorContains(t, err, "catalog unavailable")

	afterErrLock, loadErr := bundler.LoadLockFile(filepath.Join(dir, bundler.DefaultLockFileName))
	require.NoError(t, loadErr)
	assert.Equal(t, initialLock, afterErrLock, "lock file must remain untouched on resolution error")
}

func TestRunVendorUpdateWithFetcher_FilteredDependency(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	require.NoError(t, writeTestFile(solPath, depABSolutionYAML))

	newContent := []byte(minimalSolutionYAML + "# v2\n")
	writeTestLockFile(t, dir, &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: "sha256:oldhash", ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
			{Ref: "dep-b@2.0.0", ResolvedVersion: "2.0.0", Digest: contentDigest(newContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-b@2.0.0.yaml"},
		},
	})

	fetcher := &mockCatalogFetcher{
		fetchFn: func(_ context.Context, _ string) ([]byte, catalog.ArtifactInfo, error) {
			return newContent, catalog.ArtifactInfo{}, nil
		},
	}
	opts := makeTestOpts(solPath)
	opts.DryRun = true
	opts.Dependencies = []string{"dep-a@1.0.0"}
	require.NoError(t, runVendorUpdateWithFetcher(makeTestCtx(t), opts, fetcher))
}

func BenchmarkRunVendorUpdateWithFetcher(b *testing.B) {
	dir := b.TempDir()
	solPath := filepath.Join(dir, "solution.yaml")
	if err := writeTestFile(solPath, depASolutionYAML); err != nil {
		b.Fatal(err)
	}
	depContent := []byte(minimalSolutionYAML + "# bench\n")
	if err := bundler.WriteLockFile(filepath.Join(dir, bundler.DefaultLockFileName), &bundler.LockFile{
		Dependencies: []bundler.LockDependency{
			{Ref: "dep-a@1.0.0", ResolvedVersion: "1.0.0", Digest: contentDigest(depContent), ResolvedFrom: "local", VendoredAt: ".scafctl/vendor/dep-a@1.0.0.yaml"},
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
	opts := &UpdateOptions{SolutionPath: solPath, DryRun: true, CliParams: cliParams, IOStreams: ioStreams}
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

// TestCommandVendor_UnknownSubcommandErrors verifies that an unknown
// subcommand errors (non-zero) while a bare invocation shows help and exits 0.
func TestCommandVendor_UnknownSubcommandErrors(t *testing.T) {
	t.Parallel()
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandVendor(cliParams, ioStreams, "scafctl")
	cmd.SetArgs([]string{"bogus-xyz"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")

	cmd2 := CommandVendor(cliParams, ioStreams, "scafctl")
	cmd2.SetArgs([]string{})
	cmd2.SilenceErrors = true
	cmd2.SilenceUsage = true
	assert.NoError(t, cmd2.Execute())
}
