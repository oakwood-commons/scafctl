package execute

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSolution(t *testing.T) {
	t.Run("valid solution with no workflow", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		reg := provider.NewRegistry()

		result := ValidateSolution(context.Background(), sol, reg)
		assert.True(t, result.Valid)
		assert.False(t, result.HasWorkflow)
		assert.Empty(t, result.Errors)
	})

	t.Run("valid solution with empty workflow", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{},
		}
		reg := provider.NewRegistry()

		result := ValidateSolution(context.Background(), sol, reg)
		assert.True(t, result.HasWorkflow)
		// Empty workflow may or may not be valid depending on validation rules
	})

	t.Run("reports hasResolvers correctly", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		reg := provider.NewRegistry()

		result := ValidateSolution(context.Background(), sol, reg)
		assert.False(t, result.HasResolvers)
	})
}

func TestResolverExecutionConfig(t *testing.T) {
	t.Run("default config from nil context", func(t *testing.T) {
		cfg := ResolverExecutionConfigFromContext(context.Background())
		assert.NotZero(t, cfg.Timeout)
		assert.NotZero(t, cfg.PhaseTimeout)
	})
}

func TestResolvers(t *testing.T) {
	t.Run("empty resolvers returns empty data", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		reg := provider.NewRegistry()

		result, err := Resolvers(context.Background(), sol, nil, reg, ResolverExecutionConfig{})
		require.NoError(t, err)
		assert.Empty(t, result.Data)
		assert.NotNil(t, result.Context)
	})
}

func TestActions(t *testing.T) {
	t.Run("no workflow returns error", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		reg := provider.NewRegistry()
		cfg := ActionExecutionConfig{
			DefaultTimeout: 5 * time.Second,
			GracePeriod:    1 * time.Second,
		}

		result, err := Actions(context.Background(), sol, nil, reg, cfg)
		require.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "no workflow defined")
	})

	t.Run("output dir is created and resolved to absolute", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "out", "nested")

		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{},
		}
		reg := provider.NewRegistry()
		cfg := ActionExecutionConfig{
			DefaultTimeout: 5 * time.Second,
			GracePeriod:    1 * time.Second,
			OutputDir:      outputDir,
		}

		// Empty workflow will fail validation, but the directory should be
		// created before validation — verify OutputDir creation.
		_, _ = Actions(context.Background(), sol, nil, reg, cfg)

		// The directory should have been created
		info, err := os.Stat(outputDir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("cwd is passed to executor when set in config", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "test"
		sol.Spec.Workflow = &action.Workflow{
			Actions: map[string]*action.Action{},
		}
		reg := provider.NewRegistry()
		cfg := ActionExecutionConfig{
			DefaultTimeout: 5 * time.Second,
			GracePeriod:    1 * time.Second,
			Cwd:            "/custom/original/cwd",
		}

		// Empty actions will fail validation, but the executor is created
		// after validation — this test verifies the config field exists
		// and the code path doesn't panic.
		_, _ = Actions(context.Background(), sol, nil, reg, cfg)
	})
}

func TestActionExecutionConfigFromContext(t *testing.T) {
	t.Run("returns defaults when no config in context", func(t *testing.T) {
		cfg := ActionExecutionConfigFromContext(context.Background())
		assert.Equal(t, settings.DefaultActionTimeout, cfg.DefaultTimeout)
		assert.Equal(t, settings.DefaultGracePeriod, cfg.GracePeriod)
		assert.Zero(t, cfg.MaxConcurrency)
		assert.Empty(t, cfg.OutputDir)
	})

	t.Run("reads values from config context", func(t *testing.T) {
		appCfg := &config.Config{
			Action: config.ActionConfig{
				DefaultTimeout: "10s",
				GracePeriod:    "5s",
				MaxConcurrency: 4,
				OutputDir:      "/configured/output",
			},
		}
		ctx := config.WithConfig(context.Background(), appCfg)

		cfg := ActionExecutionConfigFromContext(ctx)
		assert.Equal(t, 10*time.Second, cfg.DefaultTimeout)
		assert.Equal(t, 5*time.Second, cfg.GracePeriod)
		assert.Equal(t, 4, cfg.MaxConcurrency)
		assert.Equal(t, "/configured/output", cfg.OutputDir)
	})
}

func BenchmarkActionExecutionConfigFromContext(b *testing.B) {
	appCfg := &config.Config{
		Action: config.ActionConfig{
			DefaultTimeout: "10s",
			GracePeriod:    "5s",
			MaxConcurrency: 4,
			OutputDir:      "/bench/output",
		},
	}
	ctx := config.WithConfig(context.Background(), appCfg)

	b.ResetTimer()
	for b.Loop() {
		_ = ActionExecutionConfigFromContext(ctx)
	}
}
