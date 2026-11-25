package execute

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToSolutionMeta_Source(t *testing.T) {
	t.Run("uses solution path as provenance source", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Name = "demo"
		sol.SetPath("/path/to/solution.yaml")
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		meta := toSolutionMeta(sol)
		require.NotNil(t, meta)
		assert.Equal(t, "demo", meta.Name)
		assert.Equal(t, "/path/to/solution.yaml", meta.Source)
	})

	t.Run("falls back to metadata source when no path", func(t *testing.T) {
		sol := &solution.Solution{}
		sol.Metadata.Source = "github.com/acme/solutions//demo"

		meta := toSolutionMeta(sol)
		require.NotNil(t, meta)
		assert.Equal(t, "github.com/acme/solutions//demo", meta.Source)
	})
}

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

func TestResolversForPreview_NoResolvers(t *testing.T) {
	ctx := context.Background()
	sol := &solution.Solution{}
	sol.Metadata.Name = "test"

	result, err := ResolversForPreview(ctx, sol, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestResolversWithContextForPreview_NilRegistryWithResolvers(t *testing.T) {
	ctx := context.Background()
	sol := &solution.Solution{}
	sol.Metadata.Name = "test"
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"r1": {Name: "r1"},
	}

	result, err := ResolversWithContextForPreview(ctx, sol, nil, nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no provider registry configured")
}

func TestApplySolutionCELCostLimit(t *testing.T) {
	t.Run("no options returns same context", func(t *testing.T) {
		ctx := context.Background()
		sol := &solution.Solution{}

		got := ApplySolutionCELCostLimit(ctx, sol)
		_, ok := celexp.CostLimitFromContext(got)
		assert.False(t, ok)
	})

	t.Run("cost limit zero returns same context", func(t *testing.T) {
		ctx := context.Background()
		sol := &solution.Solution{}
		zero := uint64(0)
		sol.Spec.Options = &solution.SpecOptions{
			CEL: &solution.CELOptions{CostLimit: &zero},
		}

		got := ApplySolutionCELCostLimit(ctx, sol)
		_, ok := celexp.CostLimitFromContext(got)
		assert.False(t, ok)
	})

	t.Run("solution limit applied when below global", func(t *testing.T) {
		globalLimit := celexp.GetDefaultCostLimit()
		if globalLimit == 0 {
			t.Skip("global limit is disabled; solution overrides are ignored")
		}

		ctx := context.Background()
		sol := &solution.Solution{}
		limit := uint64(100)
		sol.Spec.Options = &solution.SpecOptions{
			CEL: &solution.CELOptions{CostLimit: &limit},
		}

		got := ApplySolutionCELCostLimit(ctx, sol)
		effective, ok := celexp.CostLimitFromContext(got)
		assert.True(t, ok)
		assert.Equal(t, uint64(100), effective)
	})

	t.Run("solution limit capped at global limit", func(t *testing.T) {
		ctx := context.Background()
		sol := &solution.Solution{}
		limit := uint64(99999999) // way above default
		sol.Spec.Options = &solution.SpecOptions{
			CEL: &solution.CELOptions{CostLimit: &limit},
		}

		globalLimit := celexp.GetDefaultCostLimit()
		got := ApplySolutionCELCostLimit(ctx, sol)
		effective, ok := celexp.CostLimitFromContext(got)
		if globalLimit > 0 {
			assert.True(t, ok)
			assert.Equal(t, globalLimit, effective)
		} else {
			// When global limit is disabled (0), solution overrides are ignored
			assert.False(t, ok)
		}
	})

	t.Run("solution limit capped at parent context limit", func(t *testing.T) {
		globalLimit := celexp.GetDefaultCostLimit()
		if globalLimit == 0 {
			t.Skip("global limit is disabled; solution overrides are ignored")
		}

		parentCtx := celexp.ContextWithCostLimit(context.Background(), 250)
		sol := &solution.Solution{}
		childLimit := uint64(1000)
		sol.Spec.Options = &solution.SpecOptions{
			CEL: &solution.CELOptions{CostLimit: &childLimit},
		}

		got := ApplySolutionCELCostLimit(parentCtx, sol)
		effective, ok := celexp.CostLimitFromContext(got)
		assert.True(t, ok)
		assert.Equal(t, uint64(250), effective, "child solution must not raise parent CEL cost limit")
	})

	t.Run("tighter child limit wins below parent context limit", func(t *testing.T) {
		globalLimit := celexp.GetDefaultCostLimit()
		if globalLimit == 0 {
			t.Skip("global limit is disabled; solution overrides are ignored")
		}

		parentCtx := celexp.ContextWithCostLimit(context.Background(), 500)
		sol := &solution.Solution{}
		childLimit := uint64(120)
		sol.Spec.Options = &solution.SpecOptions{
			CEL: &solution.CELOptions{CostLimit: &childLimit},
		}

		got := ApplySolutionCELCostLimit(parentCtx, sol)
		effective, ok := celexp.CostLimitFromContext(got)
		assert.True(t, ok)
		assert.Equal(t, uint64(120), effective)
	})
}

type stubExecProvider struct {
	name string
	fn   func(ctx context.Context, input any) (*provider.Output, error)
}

func (s *stubExecProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         s.name,
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Description:  "stub provider for execute package unit tests",
		Capabilities: []provider.Capability{provider.CapabilityFrom, provider.CapabilityAction},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.AnyProp("val"),
			provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("success"),
			}),
		},
	}
}

func (s *stubExecProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	return s.fn(ctx, input)
}

func TestExecutionErrors_UnwrapAndError(t *testing.T) {
	root := errors.New("underlying failure")

	resErr := &ResolverExecutionError{
		PartialResult: &ResolverExecutionResult{Data: map[string]any{"ok": 1}},
		Err:           root,
	}
	assert.Contains(t, resErr.Error(), "resolver execution failed: underlying failure")
	assert.ErrorIs(t, resErr, root)
	assert.Equal(t, 1, resErr.PartialResult.Data["ok"])

	actErr := &ActionExecutionError{
		PartialResult: &action.ExecutionResult{FinalStatus: action.ExecutionFailed, FailedActions: []string{"deploy"}},
		Err:           root,
	}
	assert.Contains(t, actErr.Error(), "action execution failed: underlying failure")
	assert.ErrorIs(t, actErr, root)
	assert.Equal(t, []string{"deploy"}, actErr.PartialResult.FailedActions)
}

func TestResolvers_PreFilteredSliceAndPartialFailure(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&stubExecProvider{
		name: "good",
		fn: func(_ context.Context, _ any) (*provider.Output, error) {
			return &provider.Output{Data: "resolved-good"}, nil
		},
	}))
	require.NoError(t, reg.Register(&stubExecProvider{
		name: "bad",
		fn: func(_ context.Context, _ any) (*provider.Output, error) {
			return nil, errors.New("resolver exploded")
		},
	}))

	rGood := &resolver.Resolver{
		Name:    "good",
		Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "good"}}},
	}
	rBad := &resolver.Resolver{
		Name:      "bad",
		DependsOn: []string{"good"},
		Resolve:   &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "bad"}}},
	}
	sol := &solution.Solution{
		Metadata: solution.Metadata{Name: "res-test"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"good": rGood,
				"bad":  rBad,
			},
		},
	}

	t.Run("pre-filtered slice executes only selected resolvers", func(t *testing.T) {
		res, err := Resolvers(context.Background(), sol, nil, reg, ResolverExecutionConfig{
			Timeout:      5 * time.Second,
			PhaseTimeout: 5 * time.Second,
			Resolvers:    []*resolver.Resolver{rGood},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"good": "resolved-good"}, res.Data)
		assert.GreaterOrEqual(t, res.Elapsed, time.Duration(0))
	})

	t.Run("fatal failure returns ResolverExecutionError with partial data", func(t *testing.T) {
		res, err := Resolvers(context.Background(), sol, nil, reg, ResolverExecutionConfig{
			Timeout:      5 * time.Second,
			PhaseTimeout: 5 * time.Second,
		})
		require.Error(t, err)
		assert.Nil(t, res)
		var execErr *ResolverExecutionError
		require.ErrorAs(t, err, &execErr)
		require.NotNil(t, execErr.PartialResult)
		assert.Equal(t, "resolved-good", execErr.PartialResult.Data["good"])
	})
}

func TestActions_PipelineOptionsAndErrorPaths(t *testing.T) {
	t.Run("filters explicit actions, wires streams/progress/executionData, and creates outputDir", func(t *testing.T) {
		var executed []string
		var outBuf bytes.Buffer
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&stubExecProvider{
			name: "step",
			fn: func(ctx context.Context, input any) (*provider.Output, error) {
				m, _ := input.(map[string]any)
				if name, ok := m["name"].(string); ok {
					executed = append(executed, name)
				}
				if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil && streams.Out != nil {
					_, _ = streams.Out.Write([]byte("ok;"))
				}
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			},
		}))

		execExpr := celexp.Expression("string(__execution.summary.resolverCount)")
		sol := &solution.Solution{
			Metadata: solution.Metadata{Name: "act-pipeline"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"default-step": {
							Provider: "step",
							Inputs: map[string]*spec.ValueRef{
								"name":  {Literal: "default-step"},
								"count": {Expr: &execExpr},
							},
						},
						"explicit-step": {
							Provider: "step",
							Explicit: true,
							Inputs:   map[string]*spec.ValueRef{"name": {Literal: "explicit-step"}},
						},
					},
				},
			},
		}

		outDir := filepath.Join(t.TempDir(), "nested-out")
		cfg := ActionExecutionConfig{
			DefaultTimeout:   5 * time.Second,
			GracePeriod:      time.Second,
			MaxConcurrency:   2,
			OutputDir:        outDir,
			ExecutionData:    map[string]any{"summary": map[string]any{"resolverCount": 1}},
			ProgressCallback: action.NoOpProgressCallback{},
			IOStreams:        &provider.IOStreams{Out: &outBuf},
		}

		res, err := Actions(context.Background(), sol, map[string]any{}, reg, cfg)
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, action.ExecutionSucceeded, res.Result.FinalStatus)
		assert.Equal(t, []string{"default-step"}, executed)
		assert.Equal(t, "ok;", outBuf.String())
		info, statErr := os.Stat(outDir)
		require.NoError(t, statErr)
		assert.True(t, info.IsDir())
	})

	t.Run("failed action returns ActionExecutionError with PartialResult", func(t *testing.T) {
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&stubExecProvider{
			name: "fail-step",
			fn: func(_ context.Context, _ any) (*provider.Output, error) {
				return nil, errors.New("step boom")
			},
		}))

		sol := &solution.Solution{
			Metadata: solution.Metadata{Name: "act-fail"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"broken": {Provider: "fail-step"},
					},
				},
			},
		}

		res, err := Actions(context.Background(), sol, nil, reg, ActionExecutionConfig{
			DefaultTimeout: 5 * time.Second,
			GracePeriod:    time.Second,
		})
		require.Error(t, err)
		assert.Nil(t, res)
		var execErr *ActionExecutionError
		require.ErrorAs(t, err, &execErr)
		require.NotNil(t, execErr.PartialResult)
		assert.Equal(t, action.ExecutionFailed, execErr.PartialResult.FinalStatus)
		assert.Contains(t, execErr.PartialResult.FailedActions, "broken")
	})

	t.Run("cancelled context returns ActionExecutionError", func(t *testing.T) {
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&stubExecProvider{
			name: "noop",
			fn: func(_ context.Context, _ any) (*provider.Output, error) {
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			},
		}))

		sol := &solution.Solution{
			Metadata: solution.Metadata{Name: "act-cancel"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"step": {Provider: "noop"},
					},
				},
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := Actions(ctx, sol, nil, reg, ActionExecutionConfig{
			DefaultTimeout: 5 * time.Second,
			GracePeriod:    time.Second,
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}
