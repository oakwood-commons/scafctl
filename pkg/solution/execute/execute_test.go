package execute

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
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
