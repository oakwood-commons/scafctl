package prepare

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
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
