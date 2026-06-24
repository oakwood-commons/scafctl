// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeSolution writes the given YAML to a temp solution file and returns its path.
func writeSolution(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	solFile := filepath.Join(dir, "solution.yaml")
	require.NoError(t, os.WriteFile(solFile, []byte(content), 0o600))
	return solFile
}

func TestDiagnosticsFromError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		assert.Nil(t, DiagnosticsFromError(nil))
	})

	t.Run("aggregated execution error maps each failed resolver", func(t *testing.T) {
		aggErr := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				{ResolverName: "a", Phase: 1, ErrMessage: "boom a"},
				{ResolverName: "b", Phase: 2, Err: errors.New("boom b")},
				nil, // nil entries are skipped
			},
		}

		diags := DiagnosticsFromError(aggErr)
		require.Len(t, diags, 2)
		assert.Equal(t, "a", diags[0].Resolver)
		assert.Equal(t, 1, diags[0].Phase)
		assert.Equal(t, "boom a", diags[0].Message)
		assert.Equal(t, "b", diags[1].Resolver)
		assert.Equal(t, 2, diags[1].Phase)
		assert.Equal(t, "boom b", diags[1].Message)
	})

	t.Run("generic error falls back to single diagnostic", func(t *testing.T) {
		diags := DiagnosticsFromError(errors.New("something broke"))
		require.Len(t, diags, 1)
		assert.Empty(t, diags[0].Resolver)
		assert.Equal(t, "something broke", diags[0].Message)
	})
}

const badValidationSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: badval
  version: 0.0.1
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Bob"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^Alice$"
              message: "name must be Alice"
`

func TestResolvers_NonFatalValidation(t *testing.T) {
	ctx := context.Background()
	reg, err := builtin.DefaultRegistry(ctx)
	require.NoError(t, err)

	solFile := writeSolution(t, badValidationSolution)
	sol, err := inspect.LoadSolution(ctx, solFile)
	require.NoError(t, err)

	t.Run("non-fatal returns partial values plus diagnostics and no error", func(t *testing.T) {
		cfg := ResolverExecutionConfig{
			Timeout:            ResolverExecutionConfigFromContext(ctx).Timeout,
			PhaseTimeout:       ResolverExecutionConfigFromContext(ctx).PhaseTimeout,
			NonFatalValidation: true,
		}

		result, err := Resolvers(ctx, sol, nil, reg, cfg)
		require.NoError(t, err, "non-fatal mode must not return an error on validation failure")
		require.NotNil(t, result)
		assert.Equal(t, "Bob", result.Data["name"], "partial value must still be returned")
		require.Error(t, result.Diagnostics, "diagnostics must be populated on validation failure")

		diags := DiagnosticsFromError(result.Diagnostics)
		require.NotEmpty(t, diags)
		assert.Equal(t, "name", diags[0].Resolver)
	})

	t.Run("fatal mode returns error on validation failure", func(t *testing.T) {
		cfg := ResolverExecutionConfig{
			Timeout:      ResolverExecutionConfigFromContext(ctx).Timeout,
			PhaseTimeout: ResolverExecutionConfigFromContext(ctx).PhaseTimeout,
		}

		_, err := Resolvers(ctx, sol, nil, reg, cfg)
		require.Error(t, err, "default (fatal) mode must return an error on validation failure")
	})
}

const resolvePhaseFailureSolution = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolvefail
  version: 0.0.1
spec:
  resolvers:
    data:
      resolve:
        with:
          - provider: solution
            inputs:
              file: "./nonexistent.yaml"
`

func TestResolvers_NonFatalValidation_ResolvePhaseStaysFatal(t *testing.T) {
	ctx := context.Background()
	reg, err := builtin.DefaultRegistry(ctx)
	require.NoError(t, err)

	solFile := writeSolution(t, resolvePhaseFailureSolution)
	sol, err := inspect.LoadSolution(ctx, solFile)
	require.NoError(t, err)

	cfg := ResolverExecutionConfig{
		Timeout:            ResolverExecutionConfigFromContext(ctx).Timeout,
		PhaseTimeout:       ResolverExecutionConfigFromContext(ctx).PhaseTimeout,
		NonFatalValidation: true,
	}

	_, err = Resolvers(ctx, sol, nil, reg, cfg)
	require.Error(t, err, "resolve-phase failures must remain fatal even in non-fatal mode")
}

func TestIsValidationOnlyFailure(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.False(t, IsValidationOnlyFailure(nil))
	})

	t.Run("aggregated validation error is validation-only", func(t *testing.T) {
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				{ResolverName: "a", Phase: 1, Err: &resolver.AggregatedValidationError{ResolverName: "a"}},
			},
		}
		assert.True(t, IsValidationOnlyFailure(err))
	})

	t.Run("validate-phase execution error is validation-only", func(t *testing.T) {
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				{ResolverName: "a", Phase: 1, Err: &resolver.ExecutionError{ResolverName: "a", Phase: "validate"}},
			},
		}
		assert.True(t, IsValidationOnlyFailure(err))
	})

	t.Run("resolve-phase execution error is not validation-only", func(t *testing.T) {
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				{ResolverName: "a", Phase: 1, Err: &resolver.ExecutionError{ResolverName: "a", Phase: "resolve"}},
			},
		}
		assert.False(t, IsValidationOnlyFailure(err))
	})

	t.Run("mixed failures are not validation-only", func(t *testing.T) {
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				{ResolverName: "a", Phase: 1, Err: &resolver.AggregatedValidationError{ResolverName: "a"}},
				{ResolverName: "b", Phase: 1, Err: &resolver.ExecutionError{ResolverName: "b", Phase: "resolve"}},
			},
		}
		assert.False(t, IsValidationOnlyFailure(err))
	})

	t.Run("empty aggregated error is not validation-only", func(t *testing.T) {
		assert.False(t, IsValidationOnlyFailure(&resolver.AggregatedExecutionError{}))
	})

	t.Run("aggregated error with only nil entries is not validation-only", func(t *testing.T) {
		// A non-empty Errors slice whose entries are all nil has no concrete
		// failure to classify, so it must not be treated as validation-only.
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{nil, nil},
		}
		assert.False(t, IsValidationOnlyFailure(err))
	})

	t.Run("aggregated error with nils and one validation failure is validation-only", func(t *testing.T) {
		err := &resolver.AggregatedExecutionError{
			Errors: []*resolver.FailedResolver{
				nil,
				{ResolverName: "a", Phase: 1, Err: &resolver.AggregatedValidationError{ResolverName: "a"}},
			},
		}
		assert.True(t, IsValidationOnlyFailure(err))
	})

	t.Run("bare validation error is validation-only", func(t *testing.T) {
		assert.True(t, IsValidationOnlyFailure(&resolver.AggregatedValidationError{ResolverName: "a"}))
	})

	t.Run("bare generic error is not validation-only", func(t *testing.T) {
		assert.False(t, IsValidationOnlyFailure(errors.New("boom")))
	})
}
