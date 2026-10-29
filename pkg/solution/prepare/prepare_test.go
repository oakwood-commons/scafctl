package prepare

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/solutionprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func minimalSolution() *solution.Solution {
	sol := &solution.Solution{}
	sol.Metadata.Name = "test-solution"
	sol.Metadata.Version = semver.MustParse("1.0.0")
	return sol
}

// localFile is the classification of a local solution file at ref.
func localFile(ref string) source.Classification {
	return source.Classification{Kind: source.KindLocalFile, Ref: ref}
}

func TestSolutionFromLoader(t *testing.T) {
	t.Run("loads solution from loader", func(t *testing.T) {
		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-solution", result.Solution.Metadata.Name)
		assert.NotNil(t, result.Registry)
		assert.NotNil(t, result.Cleanup)
		assert.NotNil(t, result.ProviderCtx, "ProviderCtx should be set when solution provider is in registry")

		// Verify ProviderCtx enriches context with loader and registry.
		enrichedCtx := result.ProviderCtx(context.Background())
		assert.NotNil(t, solutionprovider.LoaderFromContext(enrichedCtx))
		assert.NotNil(t, solutionprovider.ProviderRegistryFromContext(enrichedCtx))
		assert.NotNil(t, solutionprovider.ChildClientTrackerFromContext(enrichedCtx))

		result.Cleanup()
	})

	t.Run("ProviderCtx injects optional plugin deps", func(t *testing.T) {
		offProviders := official.NewRegistry()
		pluginFetcher := &plugin.Fetcher{}
		pluginCfg := &plugin.ProviderConfig{}
		clientOpt := plugin.WithDebugLogging()

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"),
			WithOfficialProviders(offProviders),
			WithPluginFetcher(pluginFetcher),
			WithPluginConfig(pluginCfg),
			WithClientOptions(clientOpt),
		)
		require.NoError(t, err)
		require.NotNil(t, result.ProviderCtx)

		enrichedCtx := result.ProviderCtx(context.Background())
		assert.NotNil(t, solutionprovider.OfficialProvidersFromContext(enrichedCtx))
		assert.NotNil(t, solutionprovider.PluginFetcherFromContext(enrichedCtx))
		assert.NotNil(t, solutionprovider.PluginConfigFromContext(enrichedCtx))
		assert.NotNil(t, solutionprovider.ClientOptionsFromContext(enrichedCtx))

		result.Cleanup()
	})

	t.Run("ProviderCtx non-nil and carries execution settings without solution provider", func(t *testing.T) {
		// Empty registry with no solution provider registered.
		emptyReg := provider.NewRegistry()

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"),
			WithRegistry(emptyReg),
		)
		require.NoError(t, err)
		// ProviderCtx is always non-nil: even without the solution provider it
		// must deliver per-execution provider settings (pool-mode metadata fix).
		require.NotNil(t, result.ProviderCtx, "ProviderCtx should always be non-nil")

		enrichedCtx := result.ProviderCtx(context.Background())
		settings, ok := provider.ExecutionSettingsFromContext(enrichedCtx)
		require.True(t, ok, "execution settings should be attached")
		assert.Contains(t, settings, "metadata", "execution settings should carry the metadata blob")

		// Solution provider deps must not be attached when it is not registered.
		assert.Nil(t, solutionprovider.ProviderRegistryFromContext(enrichedCtx))
		result.Cleanup()
	})

	t.Run("loads with logger context", func(t *testing.T) {
		lgr := logr.Discard()
		ctx := logger.WithLogger(context.Background(), &lgr)
		result, err := SolutionFromLoader(ctx, &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"))
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-solution", result.Solution.Metadata.Name)
		result.Cleanup()
	})

	t.Run("returns error when loader fails", func(t *testing.T) {
		result, err := SolutionFromLoader(context.Background(), &fakeLoader{err: assert.AnError}, localFile("test.yaml"))
		require.ErrorIs(t, err, assert.AnError)
		assert.Nil(t, result)
	})

	t.Run("cleanup function is always callable", func(t *testing.T) {
		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"))
		require.NoError(t, err)

		result.Cleanup()
		result.Cleanup()
	})

	t.Run("with custom registry", func(t *testing.T) {
		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"),
			WithRegistry(provider.NewRegistry()),
		)
		require.NoError(t, err)
		assert.NotNil(t, result.Registry)
		result.Cleanup()
	})

	t.Run("sets SolutionDir from file path", func(t *testing.T) {
		// Use an absolute path to simulate -f /some/dir/solution.yaml
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "solution.yaml")

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile(filePath))
		require.NoError(t, err)
		assert.Equal(t, tmpDir, result.SolutionDir)
		result.Cleanup()
	})

	t.Run("SolutionDir respects context working directory", func(t *testing.T) {
		// Simulate --cwd /custom/dir with a relative file path
		ctx := provider.WithWorkingDirectory(context.Background(), "/custom/dir")
		result, err := SolutionFromLoader(ctx, &fakeLoader{sol: minimalSolution()}, localFile("subdir/solution.yaml"))
		require.NoError(t, err)
		assert.Equal(t, filepath.FromSlash("/custom/dir/subdir"), result.SolutionDir)
		result.Cleanup()
	})

	catalogRef := source.Classification{Kind: source.KindBareName, Ref: "starter-kit@1.0.0"}

	t.Run("SolutionDir empty for catalog reference without bundle", func(t *testing.T) {
		sol := minimalSolution()
		sol.SetPath("catalog:starter-kit@1.0.0")

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: sol}, catalogRef)
		require.NoError(t, err)
		assert.Empty(t, result.SolutionDir, "catalog references must not derive solutionDir from CWD")
		result.Cleanup()
	})

	t.Run("SolutionDir set to bundle dir for catalog reference with bundle", func(t *testing.T) {
		sol := minimalSolution()
		sol.SetPath("catalog:starter-kit@1.0.0")

		// Create a minimal bundle tar with one file
		bundleRoot := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(bundleRoot, "template.txt"), []byte("hello"), 0o600))
		files, err := bundler.ReadFileSnapshot(bundleRoot, []bundler.FileEntry{
			{RelPath: "template.txt"},
		})
		require.NoError(t, err)
		bundleData, _, err := bundler.CreateBundleTar(files, nil)
		require.NoError(t, err)

		// The bundle directory becomes the working directory until cleanup.
		origDir, err := os.Getwd()
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: sol, bundle: bundleData}, catalogRef)
		require.NoError(t, err)
		assert.NotEmpty(t, result.SolutionDir, "bundled catalog solutions must set solutionDir to extraction dir")
		assert.FileExists(t, filepath.Join(result.SolutionDir, "template.txt"))
		result.Cleanup()
		assert.NoDirExists(t, result.SolutionDir, "cleanup must remove the bundle dir")
	})

	t.Run("metrics output is written on cleanup", func(t *testing.T) {
		metricsOut := &bytes.Buffer{}

		result, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: minimalSolution()}, localFile("test.yaml"),
			WithMetrics(metricsOut),
		)
		require.NoError(t, err)
		// No provider executions, so metrics output may be empty.
		result.Cleanup()
	})

	t.Run("plugin dependency without fetcher errors", func(t *testing.T) {
		// A solution that references an external provider plugin needs a
		// plugin fetcher. (Builtin-only / pure-CEL solutions do not.)
		sol := minimalSolution()
		sol.Bundle.Plugins = []solution.PluginDependency{
			{Name: "test-plugin", Version: "1.0.0", Kind: solution.PluginKindProvider},
		}
		sol.Spec.Resolvers = map[string]*resolver.Resolver{
			"r1": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "test-plugin"}}}},
		}

		_, err := SolutionFromLoader(context.Background(), &fakeLoader{sol: sol}, localFile("test.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "plugin fetcher")
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithMetrics enables metrics", func(t *testing.T) {
		cfg := &prepareConfig{}
		out := &bytes.Buffer{}
		WithMetrics(out)(cfg)
		assert.True(t, cfg.showMetrics)
		assert.Equal(t, out, cfg.metricsOut)
	})

	t.Run("WithRegistry sets registry", func(t *testing.T) {
		cfg := &prepareConfig{}
		reg := provider.NewRegistry()
		WithRegistry(reg)(cfg)
		assert.Equal(t, reg, cfg.registry)
	})

	t.Run("WithAuthRegistry sets authRegistry", func(t *testing.T) {
		cfg := &prepareConfig{}
		aReg := auth.NewRegistry()
		WithAuthRegistry(aReg)(cfg)
		assert.Equal(t, aReg, cfg.authRegistry)
	})

	t.Run("WithLockPlugins sets lockPlugins", func(t *testing.T) {
		cfg := &prepareConfig{}
		plugins := []bundler.LockPlugin{{Name: "my-plugin"}}
		WithLockPlugins(plugins)(cfg)
		assert.Equal(t, plugins, cfg.lockPlugins)
	})

	t.Run("WithPluginFetcher sets pluginFetcher", func(t *testing.T) {
		cfg := &prepareConfig{}
		f := plugin.NewFetcher(plugin.FetcherConfig{})
		WithPluginFetcher(f)(cfg)
		assert.Equal(t, f, cfg.pluginFetcher)
	})
}

func TestWriteMetrics_Empty(t *testing.T) {
	// writeMetrics with empty GlobalMetrics should return early without writing anything
	var buf bytes.Buffer
	writeMetrics(&buf)
	assert.Empty(t, buf.String())
}

func TestWriteMetrics_WithData(t *testing.T) {
	// Enable global metrics, record some executions, then verify output
	provider.GlobalMetrics.Enable()
	defer func() {
		provider.GlobalMetrics.Reset()
		provider.GlobalMetrics.Disable()
	}()

	// Record some metrics
	provider.GlobalMetrics.Record(context.Background(), "test-provider-a", 100*time.Millisecond, true)
	provider.GlobalMetrics.Record(context.Background(), "test-provider-a", 200*time.Millisecond, false)
	provider.GlobalMetrics.Record(context.Background(), "test-provider-b", 50*time.Millisecond, true)

	var buf bytes.Buffer
	writeMetrics(&buf)

	output := buf.String()
	assert.Contains(t, output, "Provider Execution Metrics:")
	assert.Contains(t, output, "test-provider-a")
	assert.Contains(t, output, "test-provider-b")
	// Provider A: 2 total, 1 success, 1 failure
	assert.Contains(t, output, "50.0%") // 1/2 success rate for provider A
}

// ── missingOfficialProviders tests ───────────────────────────────────────────

func TestMissingOfficialProviders_FindsMissing(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	require.Len(t, missing, 1)
	assert.Equal(t, "env", missing[0].Name)
}

func TestMissingOfficialProviders_SkipsRegistered(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	// Register a mock "env" provider
	require.NoError(t, reg.Register(&mockProvider{name: "env"}))
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Empty(t, missing)
}

func TestMissingOfficialProviders_SkipsNonOfficial(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"custom": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "my-custom"}},
					},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Empty(t, missing)
}

func TestMissingOfficialProviders_DeduplicatesProviders(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"first": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
				"second": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Len(t, missing, 1)
}

func TestMissingOfficialProviders_AllPhases(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"complex": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{{Provider: "exec"}},
					},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Len(t, missing, 2)

	names := make([]string, len(missing))
	for i, m := range missing {
		names[i] = m.Name
	}
	assert.Contains(t, names, "env")
	assert.Contains(t, names, "exec")
}

func TestMissingOfficialProviders_EmptyResolvers(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Empty(t, missing)
}

func TestMissingOfficialProviders_ScansWorkflowActions(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"greeting": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {Provider: "exec"},
				},
				Finally: map[string]*action.Action{
					"cleanup": {Provider: "directory"},
				},
			},
		},
	}

	missing := missingOfficialProviders(sol, reg, officialReg)
	assert.Len(t, missing, 3)

	names := make([]string, len(missing))
	for i, m := range missing {
		names[i] = m.Name
	}
	assert.Contains(t, names, "env")
	assert.Contains(t, names, "exec")
	assert.Contains(t, names, "directory")
}

// ── With* option tests ───────────────────────────────────────────────────────

func TestWithOfficialProviders(t *testing.T) {
	t.Parallel()

	officialReg := official.NewRegistry()
	cfg := &prepareConfig{}
	opt := WithOfficialProviders(officialReg)
	opt(cfg)

	assert.Equal(t, officialReg, cfg.officialProviders)
}

func TestWithStrict(t *testing.T) {
	t.Parallel()

	cfg := &prepareConfig{}
	opt := WithStrict(true)
	opt(cfg)

	assert.True(t, cfg.strict)
}

func TestWithStrict_False(t *testing.T) {
	t.Parallel()

	cfg := &prepareConfig{}
	opt := WithStrict(false)
	opt(cfg)

	assert.False(t, cfg.strict)
}

// ── autoResolveOfficialProviders tests ───────────────────────────────────────

func TestAutoResolveOfficialProviders_NilRegistry(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	cfg := &prepareConfig{}
	sol := &solution.Solution{}

	clients, err := autoResolveOfficialProviders(t.Context(), sol, reg, cfg)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialProviders_NoMissing(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{name: "env"}))
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{officialProviders: officialReg}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	clients, err := autoResolveOfficialProviders(t.Context(), sol, reg, cfg)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialProviders_StrictModeError(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()
	cfg := &prepareConfig{
		officialProviders: officialReg,
		strict:            true,
	}

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	clients, err := autoResolveOfficialProviders(t.Context(), sol, reg, cfg)
	assert.Error(t, err)
	assert.Nil(t, clients)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Contains(t, err.Error(), "env")
}

func TestAutoResolveOfficialProviders_NoFetcher(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()

	lgr := logr.Discard()
	ctx := logger.WithLogger(t.Context(), &lgr)

	cfg := &prepareConfig{officialProviders: officialReg}
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"myenv": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	clients, err := autoResolveOfficialProviders(ctx, sol, reg, cfg)
	require.NoError(t, err)
	assert.Nil(t, clients, "should return nil when no fetcher is available")
}

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name string
}

func (m *mockProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         m.name,
		APIVersion:   "scafctl.io/v1",
		Version:      semver.MustParse("0.1.0"),
		Description:  "mock provider for testing",
		Capabilities: []provider.Capability{provider.CapabilityFrom},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: {Type: "object"},
		},
	}
}

func (m *mockProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{}, nil
}

// ── BuildPluginFetcher tests ──────────────────────────────────────────────────

func TestBuildPluginFetcher_NilLoggerInContext(t *testing.T) {
	t.Parallel()

	// No logger in context exercises the logr.Discard() branch. The function
	// should not panic regardless of whether a local catalog exists.
	ctx := context.Background()
	fetcher, err := BuildPluginFetcher(ctx)
	// Either succeeds (local catalog found) or fails gracefully -- no panic.
	if err != nil {
		assert.Nil(t, fetcher)
		assert.Contains(t, err.Error(), "building catalog chain")
	} else {
		assert.NotNil(t, fetcher)
	}
}

func TestBuildPluginFetcher_WithLogger(t *testing.T) {
	t.Parallel()

	// Logger in context exercises the *lgr branch. The function should not panic.
	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	fetcher, err := BuildPluginFetcher(ctx)
	if err != nil {
		assert.Nil(t, fetcher)
	} else {
		assert.NotNil(t, fetcher)
	}
}

// ── ResolveOfficialProviders tests ───────────────────────────────────────────

func TestResolveOfficialProviders_NilOfficialRegistryInContext(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	// No official registry in context → should return nil, nil.
	ctx := context.Background()
	clients, err := ResolveOfficialProviders(ctx, sol, reg)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialProviders_EmptyOfficialRegistry(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	sol := &solution.Solution{}
	officialReg := official.NewRegistry()

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = official.WithRegistry(ctx, officialReg)

	clients, err := ResolveOfficialProviders(ctx, sol, reg)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialProviders_NoMissingProviders(t *testing.T) {
	t.Parallel()

	// Provider "env" is already registered → nothing to resolve.
	reg := provider.NewRegistry()
	mp := &mockProvider{name: "env"}
	require.NoError(t, reg.Register(mp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "env"}},
					},
				},
			},
		},
	}

	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "env", CatalogRef: "env", DefaultVersion: "latest"},
	})

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = official.WithRegistry(ctx, officialReg)

	clients, err := ResolveOfficialProviders(ctx, sol, reg)
	require.NoError(t, err)
	assert.Nil(t, clients, "all providers already registered, nothing to fetch")
}

func TestResolveOfficialProviders_MissingProviderNoFetcherPanic(t *testing.T) {
	t.Parallel()

	// A provider is missing from the registry and is official. The function
	// should either succeed by fetching it (if a catalog is available) or
	// return nil,nil (non-fatal) when no catalog is reachable -- but never panic.
	reg := provider.NewRegistry()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "git"}},
					},
				},
			},
		},
	}

	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
	})

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = official.WithRegistry(ctx, officialReg)

	// Must not panic -- either succeeds, returns nil,nil, or returns an error
	// (e.g. when a local catalog exists but the artifact isn't in it).
	assert.NotPanics(t, func() {
		_, _ = ResolveOfficialProviders(ctx, sol, reg)
	})
}

func TestInjectHostMetadataSettings_NilConfig(t *testing.T) {
	// Must not panic with nil config.
	injectHostMetadataSettings(nil, nil)
}

func TestInjectHostMetadataSettings_PopulatesSettings(t *testing.T) {
	cfg := &plugin.ProviderConfig{BinaryName: "mycli"}

	ver := semver.MustParse("1.2.3")
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:        "test-sol",
			DisplayName: "Test Solution",
			Description: "A test",
			Category:    "testing",
			Tags:        []string{"a", "b"},
			Version:     ver,
		},
	}

	injectHostMetadataSettings(cfg, sol)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["metadata"]
	require.True(t, ok)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))

	assert.Equal(t, "cli", meta["entrypoint"])
	solMap, ok := meta["solution"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test-sol", solMap["name"])
	assert.Equal(t, "1.2.3", solMap["version"])
}

func TestInjectHostMetadataSettings_EmptyBinaryName(t *testing.T) {
	cfg := &plugin.ProviderConfig{BinaryName: ""}

	injectHostMetadataSettings(cfg, nil)

	require.NotNil(t, cfg.Settings)
	raw := cfg.Settings["metadata"]

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, "unknown", meta["entrypoint"])
}

func TestInjectHostMetadataSettings_NilSolution(t *testing.T) {
	cfg := &plugin.ProviderConfig{BinaryName: "scafctl"}
	injectHostMetadataSettings(cfg, nil)

	require.NotNil(t, cfg.Settings)
	raw := cfg.Settings["metadata"]

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	solMap, ok := meta["solution"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", solMap["name"])
}

func TestHostStaticProviderConfig(t *testing.T) {
	cfg := HostStaticProviderConfig("mycli", EntrypointMCP)

	assert.Equal(t, "mycli", cfg.BinaryName)
	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings[hostMetadataSettingsKey]
	require.True(t, ok)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))

	// Entrypoint is the one supplied by the host, not derived from BinaryName.
	assert.Equal(t, "mcp", meta["entrypoint"])

	// Solution metadata is intentionally empty: pool-mode hosts serve many
	// solutions and deliver per-solution metadata per-execution instead.
	solMap, ok := meta["solution"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", solMap["name"])

	// Tags is documented as string[] and must serialize as an array (not null),
	// even in pool mode where the solution is empty, so consumers can rely on
	// the shape.
	tags, ok := solMap["tags"].([]any)
	require.True(t, ok, "tags must be a JSON array, got %T", solMap["tags"])
	assert.Empty(t, tags)

	// Host-static build fields are present (keys always serialized).
	assert.Contains(t, meta, "buildVersion")
	assert.Contains(t, meta, "command")
	assert.Contains(t, meta, "args")
}

func TestHostStaticProviderConfig_APIEntrypoint(t *testing.T) {
	cfg := HostStaticProviderConfig("scafctl", EntrypointAPI)

	raw := cfg.Settings[hostMetadataSettingsKey]
	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, "api", meta["entrypoint"])
}

func TestHostStaticProviderConfig_EmptyEntrypointDefaultsToUnknown(t *testing.T) {
	// An empty entrypoint must serialize as EntrypointUnknown rather than a
	// blank/invalid value, even though HostStaticProviderConfig is exported and
	// a caller could pass "".
	cfg := HostStaticProviderConfig("scafctl", "")

	raw := cfg.Settings[hostMetadataSettingsKey]
	var meta map[string]any
	require.NoError(t, json.Unmarshal(raw, &meta))
	assert.Equal(t, EntrypointUnknown, meta["entrypoint"])
}

func TestBuildPerSolutionSettings(t *testing.T) {
	t.Run("carries full metadata blob with solution values", func(t *testing.T) {
		sol := minimalSolution()
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		settings := buildPerSolutionSettings(EntrypointMCP, sol)
		require.NotNil(t, settings)
		raw, ok := settings[hostMetadataSettingsKey]
		require.True(t, ok, "metadata key must be present")

		var meta map[string]any
		require.NoError(t, json.Unmarshal(raw, &meta))

		// Host-static entrypoint must be preserved so the key-level settings
		// override on the plugin side does not clobber it under pool mode.
		assert.Equal(t, "mcp", meta["entrypoint"])

		// Per-solution fields must be populated.
		solMap, ok := meta["solution"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "test-solution", solMap["name"])
		assert.Equal(t, "1.0.0", solMap["version"])
		// With no local path, source falls back to the author-declared source.
		assert.Equal(t, "github.com/acme/solutions//demo", solMap["source"])
	})

	t.Run("blob source uses local path provenance when available", func(t *testing.T) {
		sol := minimalSolution()
		sol.SetPath("/path/to/solution.yaml")
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		settings := buildPerSolutionSettings(EntrypointMCP, sol)
		require.NotNil(t, settings)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(settings[hostMetadataSettingsKey], &meta))
		solMap, ok := meta["solution"].(map[string]any)
		require.True(t, ok)
		// Path provenance takes precedence, matching the proto SolutionMeta.Source
		// channel so the metadata provider reports a consistent origin.
		assert.Equal(t, "/path/to/solution.yaml", solMap["source"])
	})

	t.Run("nil solution yields empty solution object", func(t *testing.T) {
		settings := buildPerSolutionSettings(EntrypointCLI, nil)
		require.NotNil(t, settings)

		var meta map[string]any
		require.NoError(t, json.Unmarshal(settings[hostMetadataSettingsKey], &meta))
		solMap, ok := meta["solution"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "", solMap["name"])
	})
}

func TestHostEntrypoint(t *testing.T) {
	t.Run("reads authoritative entrypoint from pool base config", func(t *testing.T) {
		reg := provider.NewRegistry()
		pool := plugin.NewPool(context.Background(), nil, reg, logr.Discard(),
			plugin.WithBaseProviderConfig(HostStaticProviderConfig("scafctl", EntrypointMCP)),
		)
		got := hostEntrypoint(&prepareConfig{pluginPool: pool})
		assert.Equal(t, EntrypointMCP, got)
	})

	t.Run("reads entrypoint set via SetBaseProviderConfigIfAbsent (pkg/mcp path)", func(t *testing.T) {
		// Regression for issue #715: a host (e.g. pkg/mcp) declaring its
		// entrypoint on a bare pool via SetBaseProviderConfigIfAbsent must be
		// honored by hostEntrypoint on the per-execution path, so the metadata
		// provider reports "mcp" instead of "unknown".
		reg := provider.NewRegistry()
		pool := plugin.NewPool(context.Background(), nil, reg, logr.Discard())
		applied := pool.SetBaseProviderConfigIfAbsent(HostStaticProviderConfig("mycli", EntrypointMCP))
		require.True(t, applied)

		got := hostEntrypoint(&prepareConfig{pluginPool: pool})
		assert.Equal(t, EntrypointMCP, got)
	})

	t.Run("falls back to CLI heuristic when binary name set (per-call)", func(t *testing.T) {
		got := hostEntrypoint(&prepareConfig{pluginCfg: &plugin.ProviderConfig{BinaryName: "scafctl"}})
		assert.Equal(t, EntrypointCLI, got)
	})

	t.Run("unknown when no pool and no binary name", func(t *testing.T) {
		got := hostEntrypoint(&prepareConfig{})
		assert.Equal(t, EntrypointUnknown, got)
	})

	t.Run("unknown for a pool whose base config lacks a metadata entrypoint", func(t *testing.T) {
		reg := provider.NewRegistry()
		// Pool with no base provider config: a pool is never the CLI path, so it
		// must not fall through to the CLI heuristic even when a binary name is set.
		pool := plugin.NewPool(context.Background(), nil, reg, logr.Discard())
		got := hostEntrypoint(&prepareConfig{
			pluginPool: pool,
			pluginCfg:  &plugin.ProviderConfig{BinaryName: "scafctl"},
		})
		assert.Equal(t, EntrypointUnknown, got)
	})
}

func TestInjectHTTPClientSettings_NilConfig(t *testing.T) {
	// Must not panic.
	injectHTTPClientSettings(context.Background(), nil)
}

func TestInjectHTTPClientSettings_NilAppConfig(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	// No config in context.
	injectHTTPClientSettings(context.Background(), cfg)
	assert.Nil(t, cfg.Settings)
}

func TestInjectHTTPClientSettings_NilAllowPrivateIPs(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	appCfg := &config.Config{}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)
	assert.Nil(t, cfg.Settings)
}

func TestInjectHTTPClientSettings_InjectsValue(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	allow := true
	appCfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs: &allow,
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["httpClient"]
	require.True(t, ok)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, true, settings["allowPrivateIPs"])
}

func TestInjectHTTPClientSettings_InjectsTrustProxyResolution(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	trust := true
	appCfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			TrustProxyResolution: &trust,
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["httpClient"]
	require.True(t, ok)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, true, settings["trustProxyResolution"])
	// allowPrivateIPs was never set on the host config, so the plugin must not
	// see it silently defaulted to true.
	assert.Equal(t, false, settings["allowPrivateIPs"])
}

func TestInjectHTTPClientSettings_InjectsTrustedProxy(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	trusted := true
	appCfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			TrustedProxy: &trusted,
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["httpClient"]
	require.True(t, ok)

	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Equal(t, true, settings["trustedProxy"])
	// A plugin must default to the same disabled-proxy posture as the host
	// when the host never set TrustedProxy at all.
	assert.Equal(t, false, settings["trustProxyResolution"])
}

func TestInjectHTTPClientSettings_AbsentAllowlistMarshalsAsNull(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	allow := true
	appCfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs: &allow,
			// AllowedPrivateCIDRs left nil: absent, not an empty override.
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["httpClient"]
	require.True(t, ok)
	assert.Contains(t, string(raw), `"allowedPrivateCIDRs":null`,
		"an absent allowlist must marshal as null, not be omitted or widened to []")

	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	assert.Nil(t, settings["allowedPrivateCIDRs"])
	assert.Equal(t, true, settings["allowPrivateIPs"])
}

// This is the precedence regression the pointer-backed AllowedPrivateCIDRs
// field exists to prevent: allowPrivateIPs:true combined with an explicit
// empty allowlist must reach the plugin as a present-but-empty list ([], not
// null and not omitted), so the plugin narrows to "no exceptions" instead of
// silently widening back to every private range.
func TestInjectHTTPClientSettings_PresentEmptyAllowlistOverridesAllowPrivateIPs(t *testing.T) {
	cfg := &plugin.ProviderConfig{}
	allow := true
	appCfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{
			AllowPrivateIPs:     &allow,
			AllowedPrivateCIDRs: config.PrivateCIDRList(),
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)
	injectHTTPClientSettings(ctx, cfg)

	require.NotNil(t, cfg.Settings)
	raw, ok := cfg.Settings["httpClient"]
	require.True(t, ok)
	assert.Contains(t, string(raw), `"allowedPrivateCIDRs":[]`,
		"a present-but-empty allowlist must marshal as [], not null")

	var settings map[string]any
	require.NoError(t, json.Unmarshal(raw, &settings))
	cidrs, ok := settings["allowedPrivateCIDRs"].([]any)
	require.True(t, ok)
	assert.Empty(t, cidrs)
	assert.Equal(t, true, settings["allowPrivateIPs"])
}

func TestWithOfficialAuthHandlers(t *testing.T) {
	reg := authofficial.NewRegistry()
	var cfg prepareConfig
	WithOfficialAuthHandlers(reg)(&cfg)
	assert.Same(t, reg, cfg.officialAuthHandlers)
}

func TestMissingOfficialAuthHandlers_SkipsRegistered(t *testing.T) {
	authReg := auth.NewRegistry()
	_ = authReg.Register(auth.NewMockHandler("github"))

	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	missing := missingOfficialAuthHandlers(sol, authReg, officialReg)
	assert.Empty(t, missing)
}

func TestMissingOfficialAuthHandlers_FindsMissing(t *testing.T) {
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	missing := missingOfficialAuthHandlers(sol, authReg, officialReg)
	require.Len(t, missing, 1)
	assert.Equal(t, "github", missing[0].Name)
}

func TestAutoResolveOfficialAuthHandlers_NilRegistry(t *testing.T) {
	sol := &solution.Solution{}
	cfg := &prepareConfig{officialAuthHandlers: nil}
	clients, err := autoResolveOfficialAuthHandlers(context.Background(), sol, nil, cfg)
	assert.NoError(t, err)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialAuthHandlers_NoMissing(t *testing.T) {
	authReg := auth.NewRegistry()
	_ = authReg.Register(auth.NewMockHandler("github"))

	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg := &prepareConfig{officialAuthHandlers: officialReg}
	clients, err := autoResolveOfficialAuthHandlers(context.Background(), sol, authReg, cfg)
	assert.NoError(t, err)
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialAuthHandlers_StrictModeError(t *testing.T) {
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg := &prepareConfig{officialAuthHandlers: officialReg, strict: true}
	clients, err := autoResolveOfficialAuthHandlers(context.Background(), sol, authReg, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Contains(t, err.Error(), "github")
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialAuthHandlers_NoFetcher(t *testing.T) {
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	cfg := &prepareConfig{officialAuthHandlers: officialReg, pluginFetcher: nil}
	clients, err := autoResolveOfficialAuthHandlers(context.Background(), sol, authReg, cfg)
	assert.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_NilOfficialRegistryInContext(t *testing.T) {
	t.Parallel()
	authReg := auth.NewRegistry()
	ctx := context.Background()
	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)
	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_NilAuthRegistry(t *testing.T) {
	t.Parallel()
	officialReg := authofficial.NewRegistry()
	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)
	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)
	clients, err := ResolveOfficialAuthHandlers(ctx, nil, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_AllRegistered(t *testing.T) {
	t.Parallel()
	authReg := auth.NewRegistry()
	_ = authReg.Register(auth.NewMockHandler("github"))
	_ = authReg.Register(auth.NewMockHandler("entra"))
	_ = authReg.Register(auth.NewMockHandler("gcp"))
	_ = authReg.Register(auth.NewMockHandler("openshift"))

	officialReg := authofficial.NewRegistry()
	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)
	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)

	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_SkipsCooldown(t *testing.T) {
	t.Parallel()
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)
	// Record cooldown for all official handlers
	for _, name := range officialReg.Names() {
		require.NoError(t, cooldown.RecordFailure(name))
	}

	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients) // All on cooldown, nothing to fetch
}

// ── WithPluginConfig / WithClientOptions coverage ────────────────────────────

func TestWithPluginConfig(t *testing.T) {
	t.Parallel()

	cfg := &prepareConfig{}
	opt := WithPluginConfig(&plugin.ProviderConfig{
		Quiet:      true,
		NoColor:    true,
		BinaryName: "testcli",
	})
	opt(cfg)

	require.NotNil(t, cfg.pluginCfg)
	assert.True(t, cfg.pluginCfg.Quiet)
	assert.True(t, cfg.pluginCfg.NoColor)
	assert.Equal(t, "testcli", cfg.pluginCfg.BinaryName)
}

func TestWithClientOptions(t *testing.T) {
	t.Parallel()

	cfg := &prepareConfig{}
	opt1 := WithClientOptions(plugin.WithSanitizedEnv())
	opt1(cfg)

	assert.Len(t, cfg.clientOpts, 1)

	// Append another
	opt2 := WithClientOptions(plugin.WithSanitizedEnv())
	opt2(cfg)
	assert.Len(t, cfg.clientOpts, 2)
}

// ── ResolveOfficialAuthHandlers deeper paths ─────────────────────────────────

func TestResolveOfficialAuthHandlers_NilCooldown(t *testing.T) {
	// Not parallel: t.Setenv modifies process environment.

	// With nil cooldown, all missing handlers are considered (no cooldown filtering).
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)

	// Register xdg.Reload cleanup BEFORE t.Setenv so LIFO ordering
	// restores the env var first, then reloads xdg.
	// Use a regular file as XDG_DATA_HOME so MkdirAll fails (cross-platform).
	invalidBase := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(invalidBase, []byte{}, 0o600))
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_DATA_HOME", invalidBase)
	xdg.Reload() // pick up the new XDG_DATA_HOME value

	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, nil, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_FetcherUnavailable(t *testing.T) {
	// Not parallel: t.Setenv modifies process environment.

	// When BuildPluginFetcher fails (no catalog chain), the function returns
	// nil gracefully without error.
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistry()

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)
	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)

	// Register xdg.Reload cleanup BEFORE t.Setenv so LIFO ordering
	// restores the env var first, then reloads xdg.
	// Use a regular file as XDG_DATA_HOME so MkdirAll fails (cross-platform).
	invalidBase := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(invalidBase, []byte{}, 0o600))
	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("XDG_DATA_HOME", invalidBase)
	xdg.Reload() // pick up the new XDG_DATA_HOME value

	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

func TestResolveOfficialAuthHandlers_EmptyOfficialRegistry(t *testing.T) {
	t.Parallel()

	// An empty official registry (0 handlers) should short-circuit.
	authReg := auth.NewRegistry()
	officialReg := authofficial.NewRegistryFrom(nil)

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	ctx = authofficial.WithRegistry(ctx, officialReg)
	cooldown := plugin.NewFetchCooldown(t.TempDir(), plugin.DefaultFetchCooldown)

	clients, err := ResolveOfficialAuthHandlers(ctx, authReg, cooldown, nil)
	require.NoError(t, err)
	assert.Nil(t, clients)
}

// ── autoResolveOfficialAuthHandlers with nil authReg after fetch ──────────────

func TestAutoResolveOfficialAuthHandlers_NilAuthRegWithFetcher(t *testing.T) {
	t.Parallel()

	// When authReg is nil but officialAuthHandlers is set with a fetcher,
	// missingOfficialAuthHandlers should still find missing (authReg == nil
	// means nothing is registered) but the function returns nil after fetch
	// because it can't register.
	officialReg := authofficial.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"token": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "identity",
								Inputs: map[string]*resolver.ValueRef{
									"handler": {Literal: "github"},
								},
							},
						},
					},
				},
			},
		},
	}

	// No fetcher → returns nil (non-fatal) even though handlers are missing.
	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	cfg := &prepareConfig{officialAuthHandlers: officialReg, pluginFetcher: nil}
	clients, err := autoResolveOfficialAuthHandlers(ctx, sol, nil, cfg)
	assert.NoError(t, err)
	assert.Nil(t, clients)
}

// ── autoResolveOfficialProviders strict mode with multiple providers ──────────

func TestAutoResolveOfficialProviders_StrictModeMultipleProviders(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
		{Name: "helm", CatalogRef: "helm", DefaultVersion: "latest"},
	})

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "git"},
							{Provider: "helm"},
						},
					},
				},
			},
		},
	}

	cfg := &prepareConfig{officialProviders: officialReg, strict: true}
	clients, err := autoResolveOfficialProviders(context.Background(), sol, reg, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "strict mode")
	assert.Contains(t, err.Error(), "git")
	assert.Contains(t, err.Error(), "helm")
	assert.Nil(t, clients)
}

func TestAutoResolveOfficialProviders_NoFetcherWithMissing(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
	})

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "git"}},
					},
				},
			},
		},
	}

	lgr := logr.Discard()
	ctx := logger.WithLogger(context.Background(), &lgr)
	cfg := &prepareConfig{officialProviders: officialReg, pluginFetcher: nil}
	clients, err := autoResolveOfficialProviders(ctx, sol, reg, cfg)
	assert.NoError(t, err)
	assert.Nil(t, clients)
}

func TestSignaturePolicyFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want *plugin.SignaturePolicy
	}{
		{
			name: "nil config returns nil",
			cfg:  nil,
			want: nil,
		},
		{
			name: "empty mode returns nil",
			cfg: &config.Config{
				Plugins: config.PluginsConfig{
					Signatures: config.PluginSignaturesConfig{Mode: ""},
				},
			},
			want: nil,
		},
		{
			name: "off mode returns nil",
			cfg: &config.Config{
				Plugins: config.PluginsConfig{
					Signatures: config.PluginSignaturesConfig{Mode: "off"},
				},
			},
			want: nil,
		},
		{
			name: "invalid mode returns nil",
			cfg: &config.Config{
				Plugins: config.PluginsConfig{
					Signatures: config.PluginSignaturesConfig{Mode: "invalid"},
				},
			},
			want: nil,
		},
		{
			name: "warn mode returns policy",
			cfg: &config.Config{
				Plugins: config.PluginsConfig{
					Signatures: config.PluginSignaturesConfig{
						Mode:              "warn",
						TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
						TrustedIdentities: []string{"https://github.com/org/*"},
					},
				},
			},
			want: &plugin.SignaturePolicy{
				Mode:              plugin.SignatureModeWarn,
				TrustedIssuers:    []string{"https://token.actions.githubusercontent.com"},
				TrustedIdentities: []string{"https://github.com/org/*"},
			},
		},
		{
			name: "enforce mode returns policy",
			cfg: &config.Config{
				Plugins: config.PluginsConfig{
					Signatures: config.PluginSignaturesConfig{
						Mode:              "enforce",
						TrustedIssuers:    []string{"https://issuer.example.com"},
						TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
					},
				},
			},
			want: &plugin.SignaturePolicy{
				Mode:              plugin.SignatureModeEnforce,
				TrustedIssuers:    []string{"https://issuer.example.com"},
				TrustedIdentities: []string{"https://github.com/oakwood-commons/*"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := signaturePolicyFromConfig(tt.cfg, logr.Discard())
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProviderPoolDeps_BundleProvidersOnly(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "custom-a", Kind: solution.PluginKindProvider},
		{Name: "custom-b", Kind: solution.PluginKindProvider},
		{Name: "some-auth", Kind: solution.PluginKindAuthHandler},
	}

	deps := providerPoolDeps(sol, &prepareConfig{})

	names := depNames(deps)
	assert.ElementsMatch(t, []string{"custom-a", "custom-b"}, names,
		"only provider-kind bundle plugins are pool deps; auth handlers excluded")
}

func TestProviderPoolDeps_IncludesReferencedOfficialProviders(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "github"}},
					},
				},
			},
		},
	}
	cfg := &prepareConfig{officialProviders: official.NewRegistry()}

	deps := providerPoolDeps(sol, cfg)

	names := depNames(deps)
	assert.Contains(t, names, "github", "referenced official provider must be a pool dep")
}

func TestProviderPoolDeps_OmitsNonOfficialReferences(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "not-official"}},
					},
				},
			},
		},
	}
	cfg := &prepareConfig{officialProviders: official.NewRegistry()}

	deps := providerPoolDeps(sol, cfg)

	assert.Empty(t, deps, "unknown/non-official referenced providers are not pool deps")
}

func TestProviderPoolDeps_DedupsBundleAndOfficial(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "github"}},
					},
				},
			},
		},
	}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "github", Kind: solution.PluginKindProvider},
	}
	cfg := &prepareConfig{officialProviders: official.NewRegistry()}

	deps := providerPoolDeps(sol, cfg)

	assert.Len(t, deps, 1, "a provider present in both bundle and official set appears once")
	assert.Equal(t, "github", deps[0].Name)
}

func TestProviderPoolDeps_NilOfficialRegistry(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "github"}},
					},
				},
			},
		},
	}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "custom", Kind: solution.PluginKindProvider},
	}

	deps := providerPoolDeps(sol, &prepareConfig{})

	names := depNames(deps)
	assert.Equal(t, []string{"custom"}, names,
		"with no official registry, only bundle providers are pool deps")
}

func depNames(deps []solution.PluginDependency) []string {
	names := make([]string, len(deps))
	for i, d := range deps {
		names[i] = d.Name
	}
	return names
}

func TestRegisterBundleAuthHandlers_NoAuthHandlers(t *testing.T) {
	t.Parallel()

	// A bundle with only provider-kind plugins (or none) declares no auth
	// handlers, so the function returns early without touching the fetcher.
	sol := &solution.Solution{}
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "custom-provider", Kind: solution.PluginKindProvider},
	}

	clients, err := registerBundleAuthHandlers(context.Background(), sol, &prepareConfig{})
	require.NoError(t, err)
	assert.Nil(t, clients, "no auth-handler plugins means no clients and no fetch")
}

func BenchmarkBuildPerSolutionSettings(b *testing.B) {
	sol := minimalSolution()
	sol.Metadata.Source = "github.com/acme/solutions//demo"
	sol.Metadata.Tags = []string{"infra", "demo"}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if s := buildPerSolutionSettings(EntrypointMCP, sol); s == nil {
			b.Fatal("expected non-nil settings")
		}
	}
}

func TestApplyOCILockLayer(t *testing.T) {
	t.Parallel()

	validLock := &bundler.LockFile{
		Version: 1,
		Plugins: []bundler.LockPlugin{
			{Name: "github", Version: "1.2.3", Digest: "sha256:abc"},
		},
	}
	validData, err := json.Marshal(validLock)
	require.NoError(t, err)

	t.Run("no-op when lockData is nil", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		require.NoError(t, applyOCILockLayer(cfg, nil))
		assert.Nil(t, cfg.lockFile)
		assert.Nil(t, cfg.lockPlugins)
	})

	t.Run("no-op when caller already supplied a lock file", func(t *testing.T) {
		t.Parallel()
		existing := &bundler.LockFile{Version: 1}
		cfg := &prepareConfig{lockFile: existing}
		require.NoError(t, applyOCILockLayer(cfg, validData))
		assert.Same(t, existing, cfg.lockFile, "caller lock must not be replaced")
	})

	t.Run("parses and applies valid lock data", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		require.NoError(t, applyOCILockLayer(cfg, validData))
		require.NotNil(t, cfg.lockFile)
		assert.Equal(t, 1, cfg.lockFile.Version)
		require.Len(t, cfg.lockPlugins, 1)
		assert.Equal(t, "github", cfg.lockPlugins[0].Name)
	})

	t.Run("errors on malformed JSON and leaves cfg unchanged", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		require.Error(t, applyOCILockLayer(cfg, []byte("not-json{{{")))
		assert.Nil(t, cfg.lockFile)
		assert.Nil(t, cfg.lockPlugins)
	})

	t.Run("errors on unsupported lock format version", func(t *testing.T) {
		t.Parallel()
		badVersion, marshalErr := json.Marshal(&bundler.LockFile{Version: 9999})
		require.NoError(t, marshalErr)
		cfg := &prepareConfig{}
		require.Error(t, applyOCILockLayer(cfg, badVersion))
		assert.Nil(t, cfg.lockFile)
		assert.Nil(t, cfg.lockPlugins)
	})
}

func TestApplyDefaultLockMode(t *testing.T) {
	t.Parallel()

	lockFile := &bundler.LockFile{Version: 1}

	t.Run("no-op when caller already set a mode", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{lockMode: LockModeConstrained}
		applyDefaultLockMode(cfg, "my-app@1.0.0", true, nil)
		assert.Equal(t, LockModeConstrained, cfg.lockMode)
	})

	t.Run("local file -> BestEffort", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		applyDefaultLockMode(cfg, "/path/to/solution.yaml", false, nil)
		assert.Equal(t, LockModeBestEffort, cfg.lockMode)
	})

	t.Run("stdin -> BestEffort", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		applyDefaultLockMode(cfg, "-", false, nil)
		assert.Equal(t, LockModeBestEffort, cfg.lockMode)
	})

	t.Run("catalog ref with lock -> Strict", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{lockFile: lockFile}
		applyDefaultLockMode(cfg, "my-app@1.0.0", true, nil)
		assert.Equal(t, LockModeStrict, cfg.lockMode)
	})

	t.Run("remote OCI ref with lock -> Strict", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{lockFile: lockFile}
		applyDefaultLockMode(cfg, "ghcr.io/acme/my-app:v1.0.0", true, nil)
		assert.Equal(t, LockModeStrict, cfg.lockMode)
	})

	t.Run("catalog ref without lock -> BestEffort", func(t *testing.T) {
		t.Parallel()
		cfg := &prepareConfig{}
		applyDefaultLockMode(cfg, "my-app@1.0.0", true, nil)
		assert.Equal(t, LockModeBestEffort, cfg.lockMode)
	})

	t.Run("catalog ref without lock emits warning", func(t *testing.T) {
		t.Parallel()
		var buf strings.Builder
		lgr := funcr.New(func(_, args string) { buf.WriteString(args) }, funcr.Options{})
		cfg := &prepareConfig{}
		applyDefaultLockMode(cfg, "my-app@1.0.0", true, &lgr)
		assert.Equal(t, LockModeBestEffort, cfg.lockMode)
		assert.Contains(t, buf.String(), "no embedded lock layer")
	})
}

// prepareFromRouter classifies path and prepares it through a load.Router
// built from ctx with routerOpts, as the CLI does.
func prepareFromRouter(ctx context.Context, path string, routerOpts ...load.RouterOption) (*Result, error) {
	c, err := source.Classify(path)
	if err != nil {
		return nil, err
	}
	return SolutionFromLoader(ctx, load.NewRouterFromContext(ctx, logr.Discard(), routerOpts...), c)
}

func TestSolutionFromLoader_Router(t *testing.T) {
	const solYAML = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: v2-solution
  version: 1.0.0
`

	t.Run("local file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(path, []byte(solYAML), 0o600))

		result, err := prepareFromRouter(context.Background(), path)
		require.NoError(t, err)
		require.NotNil(t, result)
		defer result.Cleanup()

		assert.Equal(t, "v2-solution", result.Solution.Metadata.Name)
		assert.Equal(t, path, result.Solution.GetPath())
		assert.Equal(t, dir, result.SolutionDir)
		assert.NotNil(t, result.Registry)
		assert.NotNil(t, result.ProviderCtx)
		assert.Empty(t, result.DiscoveredFrom.Path)
	})

	t.Run("stdin", func(t *testing.T) {
		result, err := prepareFromRouter(context.Background(), "-",
			load.WithStdin(strings.NewReader(solYAML)))
		require.NoError(t, err)
		defer result.Cleanup()

		assert.Equal(t, "v2-solution", result.Solution.Metadata.Name)
		assert.Empty(t, result.SolutionDir)
	})

	t.Run("stdin without reader is not enabled", func(t *testing.T) {
		_, err := prepareFromRouter(context.Background(), "-")
		require.Error(t, err)
		assert.ErrorIs(t, err, load.ErrSourceNotEnabled)
	})

	t.Run("missing local file", func(t *testing.T) {
		_, err := prepareFromRouter(context.Background(), filepath.Join(t.TempDir(), "nope.yaml"))
		require.Error(t, err)
	})

	t.Run("auto-discovery reports other matches and chosen path", func(t *testing.T) {
		dir := t.TempDir()
		primary := filepath.Join(dir, "scafctl", "solution.yaml")
		other := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.MkdirAll(filepath.Dir(primary), 0o755))
		require.NoError(t, os.WriteFile(primary, []byte(solYAML), 0o600))
		require.NoError(t, os.WriteFile(other, []byte(solYAML), 0o600))

		ctx, errOut := verboseWriterCtx(provider.WithWorkingDirectory(context.Background(), dir))
		result, err := prepareFromRouter(ctx, "")
		require.NoError(t, err)
		defer result.Cleanup()

		assert.Equal(t, primary, result.DiscoveredFrom.Path)
		assert.Contains(t, errOut.String(), "Multiple solution files found (also: "+other+"); using first match")
		assert.Contains(t, errOut.String(), "Using "+primary)
	})

	t.Run("reports the source and the loaded solution", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "solution.yaml")
		require.NoError(t, os.WriteFile(path, []byte(solYAML), 0o600))

		ctx, errOut := verboseWriterCtx(context.Background())
		result, err := prepareFromRouter(ctx, path)
		require.NoError(t, err)
		defer result.Cleanup()

		assert.Contains(t, errOut.String(), "Loading solution from: "+path)
		assert.Contains(t, errOut.String(), "Solution loaded: v2-solution (version=1.0.0, dir="+dir+")")
		assert.Contains(t, errOut.String(), "Solution: v2-solution@1.0.0 ("+path+")")
	})

	// The adjacent solution.lock is read by the loader against the context
	// working directory, so it is honoured for relative paths under --cwd and
	// for auto-discovered solutions. An unsupported lock version proves the
	// lock was read rather than silently skipped.
	for _, ref := range []string{"./solution.yaml", ""} {
		t.Run("adjacent lock is read for "+strconv.Quote(ref), func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "solution.yaml"), []byte(solYAML), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(dir, bundler.DefaultLockFileName), []byte("version: 99\nplugins: []\n"), 0o600))

			_, err := prepareFromRouter(provider.WithWorkingDirectory(context.Background(), dir), ref)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unsupported lock file version 99")
		})
	}
}

// verboseWriterCtx returns ctx with a verbose, colorless writer whose stderr
// is captured in the returned buffer.
func verboseWriterCtx(ctx context.Context) (context.Context, *bytes.Buffer) {
	errOut := &bytes.Buffer{}
	cliParams := settings.NewCliParams()
	cliParams.Verbose = true
	cliParams.NoColor = true
	w := writer.New(&terminal.IOStreams{Out: &bytes.Buffer{}, ErrOut: errOut}, cliParams)
	return writer.WithWriter(ctx, w), errOut
}

// fakeLoader is a load.Loader that records the classification it was asked
// to load and returns sol (with bundle) or err.
type fakeLoader struct {
	sol    *solution.Solution
	bundle []byte
	err    error
	got    source.Classification
}

func (f *fakeLoader) Load(_ context.Context, c source.Classification) (*load.MaterializedSolution, error) {
	f.got = c
	if f.err != nil {
		return nil, f.err
	}
	return &load.MaterializedSolution{Solution: f.sol, Bundle: f.bundle, Source: c}, nil
}

var _ load.Loader = (*fakeLoader)(nil)

func TestSubSolutionLoader_Get(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		wantKind source.Kind
	}{
		{name: "absolute file", path: "/work/child.yaml", wantKind: source.KindLocalFile},
		{name: "catalog name with version", path: "deploy-to-k8s@2.0.0", wantKind: source.KindBareName},
		{name: "url", path: "https://example.com/solution.yaml", wantKind: source.KindURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fl := &fakeLoader{sol: minimalSolution()}
			sol, err := subSolutionLoader{loader: fl}.Get(context.Background(), tt.path)
			require.NoError(t, err)
			assert.Equal(t, "test-solution", sol.Metadata.Name)
			assert.Equal(t, tt.wantKind, fl.got.Kind)
		})
	}

	t.Run("load error is returned", func(t *testing.T) {
		t.Parallel()
		fl := &fakeLoader{err: assert.AnError}
		_, err := subSolutionLoader{loader: fl}.Get(context.Background(), "/work/child.yaml")
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("classify error is wrapped", func(t *testing.T) {
		t.Parallel()
		_, err := subSolutionLoader{loader: &fakeLoader{}}.Get(context.Background(), "foo://bar")
		require.ErrorIs(t, err, source.ErrInvalidRef)
		assert.Contains(t, err.Error(), "classifying sub-solution")
	})

	t.Run("stdin is rejected without loading", func(t *testing.T) {
		t.Parallel()
		fl := &fakeLoader{sol: minimalSolution()}
		_, err := subSolutionLoader{loader: fl}.Get(context.Background(), "-")
		require.ErrorIs(t, err, load.ErrSourceNotEnabled)
		assert.Equal(t, source.KindUnknown, fl.got.Kind, "loader must not be called")
	})
}

func TestSolutionFromLoader_ProviderCtxInjectsSubSolutionLoader(t *testing.T) {
	fl := &fakeLoader{sol: minimalSolution()}
	result, err := SolutionFromLoader(context.Background(), fl, source.Classification{Kind: source.KindLocalFile, Ref: "test.yaml"})
	require.NoError(t, err)
	defer result.Cleanup()
	require.NotNil(t, result.ProviderCtx)

	l := solutionprovider.LoaderFromContext(result.ProviderCtx(context.Background()))
	require.IsType(t, subSolutionLoader{}, l)
	assert.Same(t, fl, l.(subSolutionLoader).loader)
}

func TestIsCatalogSource(t *testing.T) {
	tests := []struct {
		kind source.Kind
		want bool
	}{
		{source.KindUnknown, false},
		{source.KindAutoDiscover, false},
		{source.KindStdin, false},
		{source.KindURL, false},
		{source.KindLocalFile, false},
		{source.KindRemoteRef, true},
		{source.KindCatalogQualified, true},
		{source.KindBareName, true},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, isCatalogSource(tt.kind))
		})
	}
}

func TestExtractBundle(t *testing.T) {
	newSol := func() *solution.Solution {
		s := &solution.Solution{}
		s.Metadata.Name = "test-solution"
		return s
	}

	validBundle := func(t *testing.T) []byte {
		t.Helper()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "templates"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "templates", "a.txt"), []byte("hello"), 0o600))
		files, err := bundler.ReadFileSnapshot(root, []bundler.FileEntry{{RelPath: "templates/a.txt"}})
		require.NoError(t, err)
		data, _, err := bundler.CreateBundleTar(files, nil)
		require.NoError(t, err)
		return data
	}

	t.Run("empty bundle returns noop", func(t *testing.T) {
		dir, cleanup, err := extractBundle(context.Background(), newSol(), nil)
		require.NoError(t, err)
		assert.Empty(t, dir)
		require.NotNil(t, cleanup)
		cleanup()
	})

	t.Run("valid bundle extracts and cleanup removes", func(t *testing.T) {
		lgr := logr.Discard()
		ctx := logger.WithLogger(context.Background(), &lgr)
		dir, cleanup, err := extractBundle(ctx, newSol(), validBundle(t))
		require.NoError(t, err)
		require.NotEmpty(t, dir)
		t.Cleanup(cleanup)

		assert.FileExists(t, filepath.Join(dir, bundleSolutionFileName))
		got, rdErr := os.ReadFile(filepath.Join(dir, "templates", "a.txt"))
		require.NoError(t, rdErr)
		assert.Equal(t, "hello", string(got))

		cleanup()
		assert.NoDirExists(t, dir)
	})

	t.Run("invalid bundle errors and leaves no temp dir", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		t.Setenv("TMP", tmp)

		dir, cleanup, err := extractBundle(context.Background(), newSol(), []byte("not a tar"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to extract bundle")
		assert.Empty(t, dir)
		require.NotNil(t, cleanup)

		entries, rdErr := os.ReadDir(tmp)
		require.NoError(t, rdErr)
		assert.Empty(t, entries, "partially-created bundle dir must be removed on error")
	})
}
