package prepare

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/solution"
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
}
