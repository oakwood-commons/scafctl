package prepare

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGetter implements get.Interface for testing
type mockGetter struct {
	sol      *solution.Solution
	bundle   []byte
	err      error
	findPath string
}

func (m *mockGetter) FromLocalFileSystem(_ context.Context, _ string) (*solution.Solution, error) {
	return m.sol, m.err
}

func (m *mockGetter) FromURL(_ context.Context, _ string) (*solution.Solution, error) {
	return m.sol, m.err
}

func (m *mockGetter) Get(_ context.Context, _ string) (*solution.Solution, error) {
	return m.sol, m.err
}

func (m *mockGetter) GetWithBundle(_ context.Context, _ string) (*solution.Solution, []byte, error) {
	return m.sol, m.bundle, m.err
}

func (m *mockGetter) FindSolution() string {
	return m.findPath
}

var _ get.Interface = (*mockGetter)(nil)

func minimalSolution() *solution.Solution {
	sol := &solution.Solution{}
	sol.Metadata.Name = "test-solution"
	sol.Metadata.Version = semver.MustParse("1.0.0")
	return sol
}

func TestSolution(t *testing.T) {
	t.Run("loads solution from mock getter", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}

		result, err := Solution(context.Background(), "test.yaml",
			WithGetter(getter),
		)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "test-solution", result.Solution.Metadata.Name)
		assert.NotNil(t, result.Registry)
		assert.NotNil(t, result.Cleanup)

		result.Cleanup()
	})

	t.Run("returns error when getter fails", func(t *testing.T) {
		getter := &mockGetter{err: assert.AnError}

		result, err := Solution(context.Background(), "test.yaml",
			WithGetter(getter),
		)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("loads from stdin when path is dash", func(t *testing.T) {
		yamlContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-solution
  version: "1.0.0"
`
		stdinReader := strings.NewReader(yamlContent)

		result, err := Solution(context.Background(), "-",
			WithStdin(stdinReader),
		)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "stdin-solution", result.Solution.Metadata.Name)
		result.Cleanup()
	})

	t.Run("returns error when stdin requested but nil", func(t *testing.T) {
		result, err := Solution(context.Background(), "-",
			WithGetter(&mockGetter{}),
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "stdin requested but no reader provided")
		assert.Nil(t, result)
	})

	t.Run("cleanup function is always callable", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}

		result, err := Solution(context.Background(), "test.yaml",
			WithGetter(getter),
		)
		require.NoError(t, err)

		result.Cleanup()
		result.Cleanup()
	})

	t.Run("with custom registry", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}

		result, err := Solution(context.Background(), "test.yaml",
			WithGetter(getter),
		)
		require.NoError(t, err)
		assert.NotNil(t, result.Registry)
		result.Cleanup()
	})

	t.Run("sets SolutionDir from file path", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}

		// Use an absolute path to simulate -f /some/dir/solution.yaml
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "solution.yaml")

		result, err := Solution(context.Background(), filePath,
			WithGetter(getter),
		)
		require.NoError(t, err)
		assert.Equal(t, tmpDir, result.SolutionDir)
		result.Cleanup()
	})

	t.Run("SolutionDir empty for stdin", func(t *testing.T) {
		yamlContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-sol
  version: "1.0.0"
`
		result, err := Solution(context.Background(), "-",
			WithStdin(strings.NewReader(yamlContent)),
		)
		require.NoError(t, err)
		assert.Empty(t, result.SolutionDir)
		result.Cleanup()
	})

	t.Run("SolutionDir respects context working directory", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}

		// Simulate --cwd /custom/dir with a relative file path
		ctx := provider.WithWorkingDirectory(context.Background(), "/custom/dir")
		result, err := Solution(ctx, "subdir/solution.yaml",
			WithGetter(getter),
		)
		require.NoError(t, err)
		assert.Equal(t, "/custom/dir/subdir", result.SolutionDir)
		result.Cleanup()
	})

	t.Run("SolutionDir empty for catalog reference without bundle", func(t *testing.T) {
		sol := minimalSolution()
		// Simulate what get.Getter.fromCatalogWithBundle does: set a catalog: path
		sol.SetPath("catalog:starter-kit@1.0.0")
		getter := &mockGetter{sol: sol}

		result, err := Solution(context.Background(), "starter-kit@1.0.0",
			WithGetter(getter),
		)
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
		bundleData, _, err := bundler.CreateBundleTar(bundleRoot, []bundler.FileEntry{
			{RelPath: "template.txt"},
		}, nil)
		require.NoError(t, err)

		getter := &mockGetter{sol: sol, bundle: bundleData}

		result, err := Solution(context.Background(), "starter-kit@1.0.0",
			WithGetter(getter),
		)
		require.NoError(t, err)
		assert.NotEmpty(t, result.SolutionDir, "bundled catalog solutions must set solutionDir to extraction dir")
		assert.FileExists(t, filepath.Join(result.SolutionDir, "template.txt"))
		result.Cleanup()
	})
}

func TestWithMetrics(t *testing.T) {
	t.Run("metrics output is written on cleanup", func(t *testing.T) {
		sol := minimalSolution()
		getter := &mockGetter{sol: sol}
		metricsOut := &bytes.Buffer{}

		result, err := Solution(context.Background(), "test.yaml",
			WithGetter(getter),
			WithMetrics(metricsOut),
		)
		require.NoError(t, err)
		result.Cleanup()
	})
}

func TestOptions(t *testing.T) {
	t.Run("WithGetter sets getter", func(t *testing.T) {
		cfg := &prepareConfig{}
		g := &mockGetter{}
		WithGetter(g)(cfg)
		assert.Equal(t, g, cfg.getter)
	})

	t.Run("WithStdin sets stdin", func(t *testing.T) {
		cfg := &prepareConfig{}
		r := strings.NewReader("test")
		WithStdin(r)(cfg)
		assert.Equal(t, r, cfg.stdin)
	})

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

	t.Run("WithNoCache disables cache", func(t *testing.T) {
		cfg := &prepareConfig{}
		WithNoCache()(cfg)
		assert.True(t, cfg.noCache)
	})

	t.Run("WithPluginFetcher sets pluginFetcher", func(t *testing.T) {
		cfg := &prepareConfig{}
		f := plugin.NewFetcher(plugin.FetcherConfig{})
		WithPluginFetcher(f)(cfg)
		assert.Equal(t, f, cfg.pluginFetcher)
	})

	t.Run("WithDiscoveryMode sets discoveryMode", func(t *testing.T) {
		cfg := &prepareConfig{}
		WithDiscoveryMode(settings.DiscoveryModeAction)(cfg)
		assert.Equal(t, settings.DiscoveryModeAction, cfg.discoveryMode)
	})
}

func TestLoadSolutionWithBundle_Stdin_NilReader(t *testing.T) {
	getter := &mockGetter{}
	_, _, err := loadSolutionWithBundle(context.Background(), getter, "-", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin requested but no reader provided")
}

func TestLoadSolutionWithBundle_Stdin_ValidYAML(t *testing.T) {
	getter := &mockGetter{}
	yamlContent := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: stdin-sol
  version: "1.0.0"
`
	stdin := strings.NewReader(yamlContent)
	sol, tmpDir, err := loadSolutionWithBundle(context.Background(), getter, "-", stdin)
	require.NoError(t, err)
	assert.Equal(t, "stdin-sol", sol.Metadata.Name)
	assert.Empty(t, tmpDir)
}

func TestLoadSolutionWithBundle_NoBundle(t *testing.T) {
	sol := minimalSolution()
	getter := &mockGetter{sol: sol, bundle: nil}

	result, tmpDir, err := loadSolutionWithBundle(context.Background(), getter, "test.yaml", nil)
	require.NoError(t, err)
	assert.Equal(t, "test-solution", result.Metadata.Name)
	assert.Empty(t, tmpDir)
}

func TestLoadSolutionWithBundle_GetterError(t *testing.T) {
	getter := &mockGetter{err: assert.AnError}
	_, _, err := loadSolutionWithBundle(context.Background(), getter, "test.yaml", nil)
	require.Error(t, err)
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

func TestSolution_WithMetricsEnabled(t *testing.T) {
	sol := minimalSolution()
	getter := &mockGetter{sol: sol}
	metricsOut := &bytes.Buffer{}

	result, err := Solution(context.Background(), "test.yaml",
		WithGetter(getter),
		WithMetrics(metricsOut),
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Cleanup should call writeMetrics
	result.Cleanup()
	// No provider executions, so metrics output may be empty
}

func TestSolution_WithCustomRegistry(t *testing.T) {
	sol := minimalSolution()
	getter := &mockGetter{sol: sol}
	reg := provider.NewRegistry()

	result, err := Solution(context.Background(), "test.yaml",
		WithGetter(getter),
		WithRegistry(reg),
	)
	require.NoError(t, err)
	assert.Equal(t, reg, result.Registry)
	result.Cleanup()
}

func TestSolution_InvalidStdin(t *testing.T) {
	stdinReader := strings.NewReader("{{invalid yaml content here")

	result, err := Solution(context.Background(), "-",
		WithStdin(stdinReader),
	)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestSolution_WithPlugins(t *testing.T) {
	// Solution with plugins but no plugin fetcher — plugins should be skipped
	sol := minimalSolution()
	sol.Bundle.Plugins = []solution.PluginDependency{
		{Name: "test-plugin", Version: "1.0.0"},
	}
	getter := &mockGetter{sol: sol}

	result, err := Solution(context.Background(), "test.yaml",
		WithGetter(getter),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	result.Cleanup()
}

func TestLoadSolutionWithBundle_InvalidStdin(t *testing.T) {
	getter := &mockGetter{}
	stdin := strings.NewReader("{{not valid")
	_, _, err := loadSolutionWithBundle(context.Background(), getter, "-", stdin)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse solution from stdin")
}

func TestNewDefaultGetter_UsesContextBinaryName(t *testing.T) {
	// Not parallel: os.Chdir is process-wide and would race with other tests.

	// Create a temp directory with cldctl/solution.yaml
	tmpDir := t.TempDir()
	cldctlDir := filepath.Join(tmpDir, "cldctl")
	require.NoError(t, os.MkdirAll(cldctlDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cldctlDir, "solution.yaml"), []byte("name: test\nversion: 1.0.0\n"), 0o644))

	// Create a context with a custom binary name
	run := &settings.Run{BinaryName: "cldctl"}
	ctx := settings.IntoContext(context.Background(), run)
	lgr := logr.Discard()
	ctx = logger.WithLogger(ctx, &lgr)

	getter := NewDefaultGetter(ctx, false)
	require.NotNil(t, getter, "getter should be created from context with custom binary name")

	// chdir to tmpDir so FindSolution can discover cldctl/solution.yaml
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	found := getter.FindSolution()
	assert.Equal(t, filepath.Join("cldctl", "solution.yaml"), found)
}

func TestNewDefaultGetter_DefaultBinaryName(t *testing.T) {
	// Not parallel: os.Chdir is process-wide and would race with other tests.

	// Create a temp directory with scafctl/solution.yaml
	tmpDir := t.TempDir()
	scafctlDir := filepath.Join(tmpDir, settings.CliBinaryName)
	require.NoError(t, os.MkdirAll(scafctlDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scafctlDir, "solution.yaml"), []byte("name: test\nversion: 1.0.0\n"), 0o644))

	ctx := context.Background()
	lgr := logr.Discard()
	ctx = logger.WithLogger(ctx, &lgr)

	getter := NewDefaultGetter(ctx, false)
	require.NotNil(t, getter, "getter should be created with default binary name")

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	found := getter.FindSolution()
	assert.Equal(t, filepath.Join(settings.CliBinaryName, "solution.yaml"), found)
}

func TestNewDefaultGetter_CustomBinaryDoesNotFindDefault(t *testing.T) {
	// Not parallel: os.Chdir is process-wide and would race with other tests.

	// Create a temp directory with only scafctl/solution.yaml (no cldctl/)
	tmpDir := t.TempDir()
	scafctlDir := filepath.Join(tmpDir, settings.CliBinaryName)
	require.NoError(t, os.MkdirAll(scafctlDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(scafctlDir, "solution.yaml"), []byte("name: test\nversion: 1.0.0\n"), 0o644))

	run := &settings.Run{BinaryName: "cldctl"}
	ctx := settings.IntoContext(context.Background(), run)
	lgr := logr.Discard()
	ctx = logger.WithLogger(ctx, &lgr)

	getter := NewDefaultGetter(ctx, false)
	require.NotNil(t, getter)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Should NOT find scafctl/solution.yaml when binary name is "cldctl"
	found := getter.FindSolution()
	assert.Empty(t, found, "custom binary name getter should not discover default binary's solution folder")
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
