// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/httpc"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

func TestErrorExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: exitcode.Success},

		// ConfigError (7)
		{name: "signature policy", err: fmt.Errorf("sig: %w", ErrSignaturePolicy), want: exitcode.ConfigError},
		{name: "unconfigured catalog", err: fmt.Errorf("cat: %w", ErrUnconfiguredCatalog), want: exitcode.ConfigError},
		{name: "unconfigured plugin registry", err: fmt.Errorf("reg: %w", plugin.ErrUnconfiguredRegistry), want: exitcode.ConfigError},
		{name: "catalog not configured", err: fmt.Errorf("cat: %w", catalog.ErrCatalogNotConfigured), want: exitcode.ConfigError},
		{name: "unknown catalog index", err: fmt.Errorf("idx: %w", catalogindex.ErrUnknownCatalog), want: exitcode.ConfigError},
		{name: "catalog policy denied", err: &plugin.StageError{Stage: plugin.StageResolve, Err: &catalog.PolicyError{Message: `catalog "corp" is not in the allowed catalogs list`}}, want: exitcode.ConfigError},
		{name: "plugin not allowed", err: fmt.Errorf("policy: %w", plugin.ErrPluginNotAllowed), want: exitcode.ConfigError},
		{name: "external plugins disabled", err: fmt.Errorf("policy: %w", plugin.ErrExternalDisabled), want: exitcode.ConfigError},
		{name: "local build not allowed", err: fmt.Errorf("policy: %w", plugin.ErrLocalBuildNotAllowed), want: exitcode.ConfigError},
		{name: "http destination blocked by policy", err: fmt.Errorf("url: %w", httpc.ErrBlockedByPolicy), want: exitcode.ConfigError},
		{
			name: "config error wrapped in resolve stage keeps ConfigError precedence",
			err:  &plugin.StageError{Stage: plugin.StageResolve, Err: plugin.ErrPluginNotAllowed},
			want: exitcode.ConfigError,
		},

		// InvalidInput (3)
		{name: "invalid ref", err: fmt.Errorf("select: %w", source.ErrInvalidRef), want: exitcode.InvalidInput},
		{name: "legacy state config", err: fmt.Errorf("state: %w", state.ErrLegacyStateConfig), want: exitcode.InvalidInput},
		{name: "ambiguous solution", err: fmt.Errorf("discover: %w", load.ErrAmbiguousSolution), want: exitcode.InvalidInput},
		{name: "undeclared provider", err: fmt.Errorf("deps: %w", ErrUndeclaredProvider), want: exitcode.InvalidInput},
		{name: "missing lock file", err: fmt.Errorf("deps: %w", ErrMissingLockFile), want: exitcode.InvalidInput},
		{name: "invalid lock", err: fmt.Errorf("deps: %w", ErrInvalidLock), want: exitcode.InvalidInput},
		{name: "invalid lock file format", err: fmt.Errorf("lock: %w", bundler.ErrInvalidLockFile), want: exitcode.InvalidInput},
		{name: "invalid bundle", err: fmt.Errorf("bundle: %w", ErrInvalidBundle), want: exitcode.InvalidInput},
		{name: "invalid plugin source", err: fmt.Errorf("sol: %w", solution.ErrInvalidPluginSource), want: exitcode.InvalidInput},
		{name: "invalid query in resolve stage", err: &plugin.StageError{Stage: plugin.StageResolve, Err: &catalog.InvalidQueryError{Message: "bad semver"}}, want: exitcode.InvalidInput},
		{name: "invalid reference in resolve stage", err: &plugin.StageError{Stage: plugin.StageResolve, Err: &catalog.InvalidReferenceError{Input: "BAD", Message: "uppercase"}}, want: exitcode.InvalidInput},
		{name: "catalog conflict", err: fmt.Errorf("bind: %w", plugin.ErrCatalogConflict), want: exitcode.InvalidInput},
		{name: "lock stale", err: fmt.Errorf("lock: %w", plugin.ErrLockStale), want: exitcode.InvalidInput},
		{
			name: "stage lock wrapping ErrLockStale",
			err:  &plugin.StageError{Stage: plugin.StageLock, Err: plugin.ErrLockStale},
			want: exitcode.InvalidInput,
		},

		// CatalogError (8)
		{name: "catalog unreachable", err: &catalog.UnreachableError{Catalog: "official", Cause: errors.New("offline")}, want: exitcode.CatalogError},
		{name: "credentials rejected", err: fmt.Errorf("auth: %w", catalog.ErrCredentialsRejected), want: exitcode.CatalogError},
		{name: "remote solution fetch", err: fmt.Errorf("http: %w", load.ErrFetch), want: exitcode.CatalogError},
		{name: "catalog build error", err: &catalog.CatalogBuildError{Catalog: "bad", Err: errors.New("tls")}, want: exitcode.CatalogError},
		{name: "plugin stage resolve", err: &plugin.StageError{Stage: plugin.StageResolve, Err: catalog.ErrArtifactNotFound}, want: exitcode.CatalogError},
		{name: "plugin stage download", err: &plugin.StageError{Stage: plugin.StageDownload, Err: errors.New("eof")}, want: exitcode.CatalogError},
		{name: "plugin stage digest", err: &plugin.StageError{Stage: plugin.StageDigest, Err: errors.New("mismatch")}, want: exitcode.CatalogError},
		{name: "plugin stage signature", err: &plugin.StageError{Stage: plugin.StageSignature, Err: errors.New("unsigned")}, want: exitcode.CatalogError},
		{name: "plugin stage cache", err: &plugin.StageError{Stage: plugin.StageCache, Err: errors.New("disk full")}, want: exitcode.CatalogError},
		{name: "plugin stage spawn", err: &plugin.StageError{Stage: plugin.StageSpawn, Err: errors.New("exec format")}, want: exitcode.CatalogError},
		{name: "plugin stage handshake", err: &plugin.StageError{Stage: plugin.StageHandshake, Err: errors.New("rpc")}, want: exitcode.CatalogError},

		// PermissionDenied (10)
		{name: "permission denied", err: fmt.Errorf("open: %w", fs.ErrPermission), want: exitcode.PermissionDenied},

		// FileNotFound (4)
		{name: "solution artifact not found", err: fmt.Errorf("catalog: %w", catalog.ErrArtifactNotFound), want: exitcode.FileNotFound},
		{name: "other", err: errors.New("boom"), want: exitcode.FileNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ErrorExitCode(tc.err))
		})
	}
}

func BenchmarkErrorExitCode(b *testing.B) {
	errs := []error{
		nil,
		fmt.Errorf("sig: %w", ErrSignaturePolicy),
		fmt.Errorf("deps: %w", ErrUndeclaredProvider),
		&catalog.UnreachableError{Catalog: "official", Cause: errors.New("dial tcp: connection refused")},
		&plugin.StageError{Stage: plugin.StageResolve, Err: catalog.ErrArtifactNotFound},
		fmt.Errorf("open: %w", fs.ErrPermission),
		errors.New("open solution.yaml: no such file or directory"),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, err := range errs {
			_ = ErrorExitCode(err)
		}
	}
}

func TestReportLoaded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		verbose  bool
		solName  string
		version  string
		path     string
		disc     load.Discovery
		want     []string
		notWant  []string
		wantNone bool
	}{
		{
			name:    "name version and path",
			verbose: true,
			solName: "demo",
			version: "1.2.3",
			path:    "/work/solution.yaml",
			want:    []string{"Solution loaded: demo (version=1.2.3, dir=/work)", "Solution: demo@1.2.3 (/work/solution.yaml)"},
		},
		{
			name:    "name and version",
			verbose: true,
			solName: "demo",
			version: "1.2.3",
			want:    []string{"Solution: demo@1.2.3"},
		},
		{
			name:    "name and path",
			verbose: true,
			solName: "demo",
			path:    "/work/solution.yaml",
			want:    []string{"Solution: demo (/work/solution.yaml)"},
		},
		{
			name:    "name only",
			verbose: true,
			solName: "demo",
			want:    []string{"Solution: demo"},
			notWant: []string{"also found at"},
		},
		{
			name:    "path only",
			verbose: true,
			path:    "/work/solution.yaml",
			want:    []string{"Solution: /work/solution.yaml"},
		},
		{
			name:    "actions file alternate",
			verbose: true,
			solName: "demo",
			disc:    load.Discovery{AlternatePath: "actions.yaml"},
			want:    []string{"(actions.yaml also found at actions.yaml; use 'mycli run action' to execute actions)"},
		},
		{
			name:    "solution file alternate",
			verbose: true,
			solName: "demo",
			disc:    load.Discovery{AlternatePath: "scafctl.yaml", IsActionFile: true},
			want:    []string{"(solution file also found at scafctl.yaml; use 'mycli run solution' to run it)"},
		},
		{
			name:     "verbose disabled",
			solName:  "demo",
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			cliParams := settings.NewCliParams()
			cliParams.Verbose = tt.verbose
			cliParams.NoColor = true
			cliParams.BinaryName = "mycli"
			ctx := writer.WithWriter(context.Background(), writer.New(terminal.NewIOStreams(nil, &buf, &buf, false), cliParams))
			ctx = settings.IntoContext(ctx, cliParams)

			sol := &solution.Solution{}
			sol.Metadata.Name = tt.solName
			if tt.version != "" {
				sol.Metadata.Version = semver.MustParse(tt.version)
			}
			sol.SetPath(tt.path)

			reportLoaded(ctx, &Result{Solution: sol, SolutionDir: "/work", DiscoveredFrom: tt.disc})

			if tt.wantNone {
				assert.Empty(t, buf.String())
				return
			}
			for _, s := range tt.want {
				assert.Contains(t, buf.String(), s)
			}
			for _, s := range tt.notWant {
				assert.NotContains(t, buf.String(), s)
			}
		})
	}

	t.Run("no writer in context", func(t *testing.T) {
		t.Parallel()
		assert.NotPanics(t, func() {
			reportLoaded(context.Background(), &Result{Solution: &solution.Solution{}})
		})
	})
}

func TestNewCLIWiring(t *testing.T) {
	t.Parallel()

	t.Run("nil params", func(t *testing.T) {
		t.Parallel()
		w := NewCLIWiring(nil)
		assert.Nil(t, w.PluginConfig)
		assert.False(t, w.DebugLogging)
	})

	t.Run("maps CLI params", func(t *testing.T) {
		t.Parallel()
		params := &settings.Run{IsQuiet: true, NoColor: true, BinaryName: "mycli", MinLogLevel: "debug"}
		w := NewCLIWiring(params)
		require.NotNil(t, w.PluginConfig)
		assert.True(t, w.PluginConfig.Quiet)
		assert.True(t, w.PluginConfig.NoColor)
		assert.Equal(t, "mycli", w.PluginConfig.BinaryName)
		assert.True(t, w.DebugLogging)
	})
}

func TestErrorExitCode_RealPaths(t *testing.T) {
	t.Parallel()

	solYAML := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: real-path-demo
  version: 1.0.0
bundle:
  plugins:
    - name: echo
      kind: provider
      version: "^1.0.0"
spec:
  resolvers:
    out:
      resolve:
        with:
          - provider: echo
`

	t.Run("locked plugin rejected by catalog allowlist and plugin policy exits ConfigError", func(t *testing.T) {
		t.Parallel()
		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
			Catalogs: []config.CatalogConfig{
				{Name: "corp", Type: config.CatalogTypeOCI, URL: "oci://registry.example.com/plugins"},
				{Name: "trusted", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/example/plugins"},
			},
		}
		catFetcher, err := catalog.BuildRemotePluginFetcher(appCfg, nil, logr.Discard())
		require.NoError(t, err)

		fetcher := plugin.NewFetcher(plugin.FetcherConfig{
			CatalogFetcher:  catFetcher,
			Cache:           plugin.NewCache(t.TempDir()),
			AllowedCatalogs: []string{"trusted"},
			Logger:          logr.Discard(),
		})
		deps := []solution.PluginDependency{{Name: "echo", Kind: solution.PluginKindProvider, Version: "1.0.0"}}
		locks := []bundler.LockPlugin{{
			Name:              "echo",
			Kind:              "provider",
			Version:           "1.0.0",
			Digest:            "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ResolvedFrom:      "corp",
			ResolvedCanonical: "registry.example.com/plugins",
		}}
		_, err = fetcher.FetchPlugins(t.Context(), deps, locks)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrCatalogPolicyDenied)
		assert.Equal(t, exitcode.ConfigError, ErrorExitCode(err))

		fetcherPluginGate := plugin.NewFetcher(plugin.FetcherConfig{
			CatalogFetcher: catFetcher,
			Cache:          plugin.NewCache(t.TempDir()),
			PerCatalogArtifacts: map[string]catalog.PluginPolicy{
				"corp": {Plugins: []string{"other-plugin"}},
			},
			Logger: logr.Discard(),
		})
		_, err = fetcherPluginGate.FetchPlugins(t.Context(), deps, locks)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrCatalogPolicyDenied)
		assert.Equal(t, exitcode.ConfigError, ErrorExitCode(err))

		perCat := map[string]catalog.PluginPolicy{
			"corp": {Plugins: []string{"other-plugin"}},
		}
		gatedCatFetcher, err := catalog.BuildRemotePluginFetcher(appCfg, nil, logr.Discard(), catalog.WithPerCatalogArtifacts(perCat))
		require.NoError(t, err)
		rangeFetcher := plugin.NewFetcher(plugin.FetcherConfig{
			CatalogFetcher:      gatedCatFetcher,
			Cache:               plugin.NewCache(t.TempDir()),
			PerCatalogArtifacts: perCat,
			Logger:              logr.Discard(),
		})
		rangeDeps := []solution.PluginDependency{{
			Name:    "echo",
			Kind:    solution.PluginKindProvider,
			Version: "^1.0.0",
			Catalog: "corp",
		}}
		_, err = rangeFetcher.FetchPlugins(t.Context(), rangeDeps, nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrCatalogPolicyDenied)
		assert.Equal(t, exitcode.ConfigError, ErrorExitCode(err))
	})

	t.Run("malformed and unsupported local lock files exit InvalidInput", func(t *testing.T) {
		t.Parallel()
		for _, lockContent := range []string{"version: [", "version: 99\n"} {
			dir := t.TempDir()
			solPath := filepath.Join(dir, "solution.yaml")
			lockPath := filepath.Join(dir, bundler.DefaultLockFileName)
			require.NoError(t, os.WriteFile(solPath, []byte(solYAML), 0o600))
			require.NoError(t, os.WriteFile(lockPath, []byte(lockContent), 0o600))

			_, err := load.NewFileLoader().Load(t.Context(), source.Classification{
				Kind: source.KindLocalFile,
				Ref:  solPath,
			})
			require.Error(t, err)
			assert.Equal(t, exitcode.InvalidInput, ErrorExitCode(err))
		}
	})

	t.Run("embedded OCI lock layer and stale strict/constrained locks exit InvalidInput", func(t *testing.T) {
		t.Parallel()
		var sol solution.Solution
		require.NoError(t, sol.LoadFromBytes([]byte(solYAML)))

		for _, badLayer := range [][]byte{[]byte("{bad json"), []byte(`{"version":99}`)} {
			loader := loaderFunc(func(context.Context, source.Classification) (*load.MaterializedSolution, error) {
				var cp solution.Solution
				if err := cp.LoadFromBytes([]byte(solYAML)); err != nil {
					return nil, err
				}
				return &load.MaterializedSolution{
					Solution: &cp,
					Lock:     badLayer,
					Source:   source.Classification{Kind: source.KindBareName, Ref: "real-path-demo"},
				}, nil
			})
			_, err := SolutionFromLoader(t.Context(), loader, source.Classification{Kind: source.KindBareName, Ref: "real-path-demo"})
			require.Error(t, err)
			assert.Equal(t, exitcode.InvalidInput, ErrorExitCode(err))
		}

		staleLock := &bundler.LockFile{
			Version: bundler.LockFileVersion,
			Plugins: []bundler.LockPlugin{{
				Name:              "echo",
				Kind:              "provider",
				Version:           "0.5.0",
				Constraint:        "^0.5.0",
				ResolvedFrom:      "local",
				ResolvedCanonical: "",
			}},
		}
		staleLockJSON, err := bundler.MarshalLockJSON(staleLock)
		require.NoError(t, err)

		for _, mode := range []LockMode{LockModeStrict, LockModeConstrained} {
			loader := loaderFunc(func(context.Context, source.Classification) (*load.MaterializedSolution, error) {
				var cp solution.Solution
				if loadErr := cp.LoadFromBytes([]byte(solYAML)); loadErr != nil {
					return nil, loadErr
				}
				return &load.MaterializedSolution{
					Solution: &cp,
					Lock:     staleLockJSON,
					Source:   source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
				}, nil
			})
			_, prepErr := SolutionFromLoader(
				t.Context(),
				loader,
				source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
				WithLockMode(mode),
			)
			require.Error(t, prepErr)
			assert.Equal(t, exitcode.InvalidInput, ErrorExitCode(prepErr), "mode=%v", mode)
		}

		unconfiguredLock := &bundler.LockFile{
			Version: bundler.LockFileVersion,
			Plugins: []bundler.LockPlugin{{
				Name:              "echo",
				Kind:              "provider",
				Version:           "1.0.0",
				Constraint:        "^1.0.0",
				ResolvedFrom:      "missing-cat",
				ResolvedCanonical: "registry.unconfigured.invalid/plugins",
			}},
		}
		unconfiguredJSON, err := bundler.MarshalLockJSON(unconfiguredLock)
		require.NoError(t, err)
		unconfiguredLoader := loaderFunc(func(context.Context, source.Classification) (*load.MaterializedSolution, error) {
			var cp solution.Solution
			if loadErr := cp.LoadFromBytes([]byte(solYAML)); loadErr != nil {
				return nil, loadErr
			}
			return &load.MaterializedSolution{
				Solution: &cp,
				Lock:     unconfiguredJSON,
				Source:   source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
			}, nil
		})
		_, unconfErr := SolutionFromLoader(
			t.Context(),
			unconfiguredLoader,
			source.Classification{Kind: source.KindLocalFile, Ref: "solution.yaml"},
			WithLockMode(LockModeStrict),
		)
		require.Error(t, unconfErr)
		assert.Equal(t, exitcode.ConfigError, ErrorExitCode(unconfErr))
	})

	t.Run("URL loader classifies 503 as CatalogError, 404 and parse errors as FileNotFound, and policy block as ConfigError", func(t *testing.T) {
		t.Parallel()
		policy, err := httpc.PolicyFromAppConfig(&config.HTTPClientConfig{
			AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
		})
		require.NoError(t, err)
		cfg := httpc.DefaultConfig()
		cfg.EnableCache = false
		cfg.RetryMax = 0
		cfg.IPPolicy = policy
		allowClient := httpc.NewClient(cfg)

		srv503 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv503.Close)
		_, err = load.NewURLLoader(allowClient).Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: srv503.URL})
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, ErrorExitCode(err))

		srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		t.Cleanup(srv404.Close)
		_, err = load.NewURLLoader(allowClient).Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: srv404.URL})
		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, ErrorExitCode(err))

		srvBadYAML := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not valid yaml: ["))
		}))
		t.Cleanup(srvBadYAML.Close)
		_, err = load.NewURLLoader(allowClient).Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: srvBadYAML.URL})
		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, ErrorExitCode(err))

		_, err = load.NewURLLoader(nil).Load(t.Context(), source.Classification{Kind: source.KindURL, Ref: srv503.URL})
		require.Error(t, err)
		assert.Equal(t, exitcode.ConfigError, ErrorExitCode(err))
	})
}

type loaderFunc func(context.Context, source.Classification) (*load.MaterializedSolution, error)

func (f loaderFunc) Load(ctx context.Context, c source.Classification) (*load.MaterializedSolution, error) {
	return f(ctx, c)
}
