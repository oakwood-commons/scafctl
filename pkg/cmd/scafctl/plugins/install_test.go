// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
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
	ctx := provider.WithWorkingDirectory(newInstallTestCtx(t), t.TempDir())

	opts := &InstallOptions{
		CliParams: settings.NewCliParams(),
		IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
		File:      "",
	}

	err := runInstall(ctx, opts, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin names or solution file provided")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestLoadPluginsFromSolution_AutoDiscover(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sol := "apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: no-plugins\n  version: 1.0.0\nspec: {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(sol), 0o600))
	ctx := provider.WithWorkingDirectory(newInstallTestCtx(t), dir)

	plugins, lockPlugins, err := loadPluginsFromSolution(ctx, &InstallOptions{CliParams: settings.NewCliParams()})
	require.NoError(t, err)
	assert.Empty(t, plugins)
	assert.Empty(t, lockPlugins)
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
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
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
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
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

func TestRunInstall_FallbackWarningAndLockedSolutionFailsClosed(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	unreachSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(unreachSrv.Close)
	host := strings.TrimPrefix(unreachSrv.URL, "http://")

	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{Name: "broken-corp", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/corp", Insecure: true},
			{Name: "second", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/second", Insecure: true},
		},
	}

	// 1. Standalone unpinned install emits fallback warning on stderr and leaves stdout clean.
	var stdout, stderr bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &stdout, &stderr, false)
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	lgr := logr.Discard()
	ctx := config.WithConfig(logger.WithLogger(writer.WithWriter(context.Background(), w), &lgr), appCfg)

	err := runInstall(ctx, &InstallOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		NoCache:   true,
		Kind:      "provider",
	}, []string{"exec"})
	require.Error(t, err)
	assert.Contains(t, stderr.String(), `catalog "broken-corp" unavailable, trying next catalog...`)

	// 2. Solution install with solution.lock pins locked version+catalog and fails closed without fallback warning.
	stderr.Reset()
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: locked-sol
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: ^1.0.0
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))
	require.NoError(t, bundler.WriteLockFile(filepath.Join(dir, bundler.DefaultLockFileName), &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Plugins: []bundler.LockPlugin{
			{
				Name:              "exec",
				Kind:              "provider",
				Version:           "1.2.3",
				Constraint:        "^1.0.0",
				ResolvedFrom:      "broken-corp",
				ResolvedCanonical: host + "/corp",
				Digests:           map[string]string{"linux/amd64": "sha256:deadbeef", "darwin/arm64": "sha256:deadbeef"},
			},
		},
	}))

	err = runInstall(ctx, &InstallOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		File:      solFile,
		CacheDir:  t.TempDir(),
		NoCache:   true,
	}, nil)
	require.Error(t, err)
	assert.NotContains(t, stderr.String(), "trying next catalog", "locked solution install must fail closed without falling through")
}

func TestCommandInstall_PreReleaseFlagAndEmbedder(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandInstall(cliParams, ioStreams, "mycli")

	f := cmd.Flags().Lookup("pre-release")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
	assert.Contains(t, cmd.Long, "mycli plugins install")
	assert.Contains(t, cmd.Long, "--pre-release")
}

func TestRunInstall_PreReleaseAndLockPinning(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataDir)

	localCat, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)

	plat := plugin.CurrentPlatform()
	bin040 := []byte("provider-bin-0.4.0")
	bin050Dev := []byte("provider-bin-0.5.0-dev")

	info040, err := localCat.StoreMultiPlatform(context.Background(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "custom-prov",
		Version: semver.MustParse("0.4.0"),
	}, []catalog.PlatformBinary{{Platform: plat, Data: bin040}}, nil, false)
	require.NoError(t, err)
	require.NotEmpty(t, info040.Digest)

	_, err = localCat.StoreMultiPlatform(context.Background(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "custom-prov",
		Version: semver.MustParse("0.5.0-dev"),
	}, []catalog.PlatformBinary{{Platform: plat, Data: bin050Dev}}, nil, false)
	require.NoError(t, err)

	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{Name: "local", Type: config.CatalogTypeFilesystem},
		},
	}

	run := func(opts *InstallOptions, args []string) string {
		t.Helper()
		var outBuf, errBuf bytes.Buffer
		ioStreams := terminal.NewIOStreams(nil, &outBuf, &errBuf, false)
		cliParams := settings.NewCliParams()
		w := writer.New(ioStreams, cliParams)
		lgr := logr.Discard()
		ctx := config.WithConfig(logger.WithLogger(writer.WithWriter(context.Background(), w), &lgr), appCfg)

		opts.CliParams = cliParams
		opts.IOStreams = ioStreams
		opts.Platform = plat
		require.NoError(t, runInstall(ctx, opts, args))
		return outBuf.String()
	}

	// 1. Standalone unpinned and range without --pre-release install 0.4.0.
	outUnpinnedStable := run(&InstallOptions{
		CacheDir: t.TempDir(),
		Kind:     "provider",
	}, []string{"custom-prov"})
	assert.Contains(t, outUnpinnedStable, "custom-prov@0.4.0")

	outStable := run(&InstallOptions{
		CacheDir: t.TempDir(),
		Kind:     "provider",
		Version:  ">=0.4.0",
	}, []string{"custom-prov"})
	assert.Contains(t, outStable, "custom-prov@0.4.0")

	// 2. Standalone unpinned and range with --pre-release install 0.5.0-dev.
	outUnpinnedPre := run(&InstallOptions{
		CacheDir:   t.TempDir(),
		Kind:       "provider",
		PreRelease: true,
	}, []string{"custom-prov"})
	assert.Contains(t, outUnpinnedPre, "custom-prov@0.5.0-dev")

	outPre := run(&InstallOptions{
		CacheDir:   t.TempDir(),
		Kind:       "provider",
		Version:    ">=0.4.0",
		PreRelease: true,
	}, []string{"custom-prov"})
	assert.Contains(t, outPre, "custom-prov@0.5.0-dev")

	// 3. Solution with solution.lock stays pinned to locked 0.4.0 even when --pre-release is set.
	cd040, err := localCat.ResolveContentDigest(context.Background(), catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "custom-prov",
		Version: semver.MustParse("0.4.0"),
	}, plat, catalog.MediaTypeProviderBinary)
	require.NoError(t, err)

	solDir := t.TempDir()
	solFile := filepath.Join(solDir, "solution.yaml")
	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: locked-pre-test
  version: 1.0.0
bundle:
  plugins:
    - name: custom-prov
      kind: provider
      version: ">=0.4.0"
spec: {}
`
	require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))
	require.NoError(t, bundler.WriteLockFile(filepath.Join(solDir, bundler.DefaultLockFileName), &bundler.LockFile{
		Version: bundler.LockFileVersion,
		Plugins: []bundler.LockPlugin{
			{
				Name:         "custom-prov",
				Kind:         "provider",
				Version:      "0.4.0",
				Constraint:   ">=0.4.0",
				ResolvedFrom: "local",
				Digests:      map[string]string{plat: cd040.ContentDigest},
			},
		},
	}))

	outLocked := run(&InstallOptions{
		File:       solFile,
		CacheDir:   t.TempDir(),
		PreRelease: true,
	}, nil)
	assert.Contains(t, outLocked, "custom-prov@0.4.0", "locked solution must stay pinned to 0.4.0 despite --pre-release")
}

func TestRunInstall_ExitCodes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	t.Run("unconfigured source registry exits ConfigError", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unconfigured-reg
  version: 1.0.0
bundle:
  plugins:
    - name: custom
      kind: provider
      version: ^1.0.0
      source:
        registry: registry.unconfigured.invalid/plugins
spec: {}
`
		require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))

		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
		}
		ctx := config.WithConfig(newInstallTestCtx(t), appCfg)
		err := runInstall(ctx, &InstallOptions{
			CliParams: settings.NewCliParams(),
			IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
			File:      solFile,
			CacheDir:  t.TempDir(),
		}, nil)
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))
	})

	t.Run("invalid signature policy exits ConfigError", func(t *testing.T) {
		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
			Plugins: config.PluginsConfig{
				Signatures: config.PluginSignaturesConfig{
					Mode: "invalid-mode",
				},
			},
		}
		ctx := config.WithConfig(newInstallTestCtx(t), appCfg)
		err := runInstall(ctx, &InstallOptions{
			CliParams: settings.NewCliParams(),
			IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
			CacheDir:  t.TempDir(),
			Kind:      "provider",
		}, []string{"exec"})
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, exitcode.GetCode(err))
	})

	t.Run("unreachable catalog exits CatalogError with offline hint", func(t *testing.T) {
		unreachSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(unreachSrv.Close)
		host := strings.TrimPrefix(unreachSrv.URL, "http://")

		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
			Catalogs: []config.CatalogConfig{
				{Name: "offline-cat", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/plugins", Insecure: true},
			},
		}
		ctx := config.WithConfig(newInstallTestCtx(t), appCfg)
		err := runInstall(ctx, &InstallOptions{
			CliParams: settings.NewCliParams(),
			IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
			CacheDir:  t.TempDir(),
			NoCache:   true,
			Kind:      "provider",
		}, []string{"exec"})
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "plugins install")
	})

	t.Run("permission denied on solution load exits PermissionDenied", func(t *testing.T) {
		wrapped := exitcode.WithCode(fmt.Errorf("reading solution: %w", fs.ErrPermission), exitcode.InvalidInput)
		got := reclassifyLoadError(wrapped)
		require.Error(t, got)
		assert.Equal(t, exitcode.PermissionDenied, exitcode.GetCode(got))
	})

	t.Run("malformed plugin version selector exits InvalidInput", func(t *testing.T) {
		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
		}
		ctx := config.WithConfig(newInstallTestCtx(t), appCfg)
		for _, badArg := range []string{"exec@not-a-version", "exec@", "@1.0.0"} {
			err := runInstall(ctx, &InstallOptions{
				CliParams: settings.NewCliParams(),
				IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
				CacheDir:  t.TempDir(),
				NoCache:   true,
				Kind:      "provider",
			}, []string{badArg})
			require.Error(t, err, "arg=%s", badArg)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err), "arg=%s", badArg)
		}
	})

	t.Run("malformed adjacent solution.lock exits InvalidInput", func(t *testing.T) {
		dir := t.TempDir()
		solFile := filepath.Join(dir, "solution.yaml")
		lockFile := filepath.Join(dir, bundler.DefaultLockFileName)
		solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bad-lock-install
  version: 1.0.0
bundle:
  plugins:
    - name: exec
      kind: provider
      version: "^1.0.0"
spec: {}
`
		require.NoError(t, os.WriteFile(solFile, []byte(solYAML), 0o600))
		require.NoError(t, os.WriteFile(lockFile, []byte("version: ["), 0o600))

		err := runInstall(newInstallTestCtx(t), &InstallOptions{
			CliParams: settings.NewCliParams(),
			IOStreams: &terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}},
			File:      solFile,
			CacheDir:  t.TempDir(),
		}, nil)
		require.Error(t, err)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	})
}
