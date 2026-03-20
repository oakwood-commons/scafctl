// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// --- Mock Loader ---

type mockLoader struct {
	solutions map[string]*solution.Solution
	err       error
}

func (m *mockLoader) Get(_ context.Context, path string) (*solution.Solution, error) {
	if m.err != nil {
		return nil, m.err
	}
	sol, ok := m.solutions[path]
	if !ok {
		return nil, fmt.Errorf("solution %q not found", path)
	}
	return sol, nil
}

// --- Mock Provider ---

type mockProvider struct {
	name   string
	output *provider.Output
	err    error
}

func (m *mockProvider) Descriptor() *provider.Descriptor {
	v, _ := semver.NewVersion("1.0.0")
	return &provider.Descriptor{
		Name:        m.name,
		APIVersion:  "v1",
		Version:     v,
		Description: "mock provider for testing purposes only",
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityAction,
		},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.ObjectSchema(nil, nil),
			provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("success"),
			}),
		},
	}
}

func (m *mockProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return m.output, m.err
}

// --- Helper Functions ---

func testContext() context.Context {
	ctx := context.Background()
	lgr := logger.GetNoopLogger()
	ctx = logger.WithLogger(ctx, lgr)
	return ctx
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

// newTestSolution creates a minimal solution with the given resolvers and optional workflow.
func newTestSolution(resolvers map[string]*resolver.Resolver, workflow *action.Workflow) *solution.Solution { //nolint:unparam // workflow is nil in current tests but exists for future test expansion
	return &solution.Solution{
		Spec: solution.Spec{
			Resolvers: resolvers,
			Workflow:  workflow,
		},
	}
}

// --- Descriptor Tests ---

func TestDescriptor(t *testing.T) {
	p := New()
	desc := p.Descriptor()

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.Equal(t, "1.0.0", desc.Version.String())
	assert.NotEmpty(t, desc.Description)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
	assert.NotNil(t, desc.Schema)
	assert.NotNil(t, desc.OutputSchemas)
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityFrom])
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityAction])
	assert.NotNil(t, desc.Decode)
	assert.NotNil(t, desc.ExtractDependencies)

	// Validate descriptor passes validation
	err := provider.ValidateDescriptor(desc)
	assert.NoError(t, err)
}

// --- Decode Tests ---

func TestDecodeInput(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		want    *Input
		wantErr bool
	}{
		{
			name: "minimal input",
			input: map[string]any{
				"source": "./child.yaml",
			},
			want: &Input{
				Source: "./child.yaml",
			},
		},
		{
			name: "full input",
			input: map[string]any{
				"source":          "deploy@2.0.0",
				"inputs":          map[string]any{"env": "prod"},
				"propagateErrors": false,
				"maxDepth":        5,
			},
			want: &Input{
				Source:          "deploy@2.0.0",
				Inputs:          map[string]any{"env": "prod"},
				PropagateErrors: boolPtr(false),
				MaxDepth:        intPtr(5),
			},
		},
		{
			name: "maxDepth as float64",
			input: map[string]any{
				"source":   "test.yaml",
				"maxDepth": float64(8),
			},
			want: &Input{
				Source:   "test.yaml",
				MaxDepth: intPtr(8),
			},
		},
		{
			name:    "missing source",
			input:   map[string]any{},
			wantErr: true,
		},
		{
			name: "empty source",
			input: map[string]any{
				"source": "",
			},
			wantErr: true,
		},
		{
			name: "invalid propagateErrors type",
			input: map[string]any{
				"source":          "test.yaml",
				"propagateErrors": "yes",
			},
			wantErr: true,
		},
		{
			name: "invalid maxDepth type",
			input: map[string]any{
				"source":   "test.yaml",
				"maxDepth": "ten",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeInput(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			got := result.(*Input)
			assert.Equal(t, tt.want.Source, got.Source)
			assert.Equal(t, tt.want.Inputs, got.Inputs)
			if tt.want.PropagateErrors != nil {
				require.NotNil(t, got.PropagateErrors)
				assert.Equal(t, *tt.want.PropagateErrors, *got.PropagateErrors)
			} else {
				assert.Nil(t, got.PropagateErrors)
			}
			if tt.want.MaxDepth != nil {
				require.NotNil(t, got.MaxDepth)
				assert.Equal(t, *tt.want.MaxDepth, *got.MaxDepth)
			} else {
				assert.Nil(t, got.MaxDepth)
			}
		})
	}
}

// --- Input Defaults Tests ---

func TestInput_ShouldPropagateErrors(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		in := &Input{}
		assert.True(t, in.shouldPropagateErrors())
	})

	t.Run("explicit true", func(t *testing.T) {
		in := &Input{PropagateErrors: boolPtr(true)}
		assert.True(t, in.shouldPropagateErrors())
	})

	t.Run("explicit false", func(t *testing.T) {
		in := &Input{PropagateErrors: boolPtr(false)}
		assert.False(t, in.shouldPropagateErrors())
	})
}

func TestInput_MaxDepthOrDefault(t *testing.T) {
	t.Run("nil defaults to 10", func(t *testing.T) {
		in := &Input{}
		assert.Equal(t, defaultMaxDepth, in.maxDepthOrDefault())
	})

	t.Run("explicit value", func(t *testing.T) {
		in := &Input{MaxDepth: intPtr(5)}
		assert.Equal(t, 5, in.maxDepthOrDefault())
	})
}

// --- Dependency Extraction Tests ---

func TestExtractDependencies(t *testing.T) {
	tests := []struct {
		name   string
		inputs map[string]any
		want   []string
	}{
		{
			name:   "empty inputs",
			inputs: map[string]any{},
			want:   nil,
		},
		{
			name: "CEL expression in source",
			inputs: map[string]any{
				"source": "_.config",
			},
			want: []string{"config"},
		},
		{
			name: "resolver ref in inputs",
			inputs: map[string]any{
				"source": "child.yaml",
				"inputs": map[string]any{
					"env": map[string]any{"rslvr": "environment"},
				},
			},
			want: []string{"environment"},
		},
		{
			name: "CEL expr in inputs",
			inputs: map[string]any{
				"source": "child.yaml",
				"inputs": map[string]any{
					"msg": map[string]any{"expr": "_.greeting + ' world'"},
				},
			},
			want: []string{"greeting"},
		},
		{
			name: "Go template in inputs",
			inputs: map[string]any{
				"source": "child.yaml",
				"inputs": map[string]any{
					"msg": map[string]any{"tmpl": "Hello {{.name}}!"},
				},
			},
			want: []string{"name"},
		},
		{
			name: "multiple refs",
			inputs: map[string]any{
				"source": "_.base_path",
				"inputs": map[string]any{
					"a": map[string]any{"rslvr": "first"},
					"b": map[string]any{"expr": "_.second + _.third"},
				},
			},
			want: []string{"base_path", "first", "second", "third"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDependencies(tt.inputs)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Extract Refs Tests ---

func TestExtractRefsFromString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple ref",
			input: "_.env",
			want:  []string{"env"},
		},
		{
			name:  "multiple refs",
			input: "_.first + _.second",
			want:  []string{"first", "second"},
		},
		{
			name:  "no refs",
			input: "hello world",
			want:  nil,
		},
		{
			name:  "underscored identifiers",
			input: "_.my_var_1",
			want:  []string{"my_var_1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRefsFromString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractRefsFromTemplate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple template",
			input: "{{.env}}",
			want:  []string{"env"},
		},
		{
			name:  "multiple fields",
			input: "{{.first}}-{{.second}}",
			want:  []string{"first", "second"},
		},
		{
			name:  "no templates",
			input: "plain text",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRefsFromTemplate(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- Registry Adapter Tests ---

func TestResolverRegistryAdapter(t *testing.T) {
	reg := provider.NewRegistry()
	mp := &mockProvider{name: "test-provider"}
	require.NoError(t, reg.Register(mp))

	adapter := &resolverRegistryAdapter{registry: reg}

	t.Run("Get existing provider", func(t *testing.T) {
		p, err := adapter.Get("test-provider")
		require.NoError(t, err)
		assert.Equal(t, "test-provider", p.Descriptor().Name)
	})

	t.Run("Get missing provider", func(t *testing.T) {
		_, err := adapter.Get("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("List providers", func(t *testing.T) {
		providers := adapter.List()
		assert.Len(t, providers, 1)
	})

	t.Run("DescriptorLookup", func(t *testing.T) {
		lookup := adapter.DescriptorLookup()
		assert.NotNil(t, lookup)
	})
}

func TestActionRegistryAdapter(t *testing.T) {
	reg := provider.NewRegistry()
	mp := &mockProvider{name: "test-provider"}
	require.NoError(t, reg.Register(mp))

	adapter := &actionRegistryAdapter{registry: reg}

	t.Run("Get existing provider", func(t *testing.T) {
		p, ok := adapter.Get("test-provider")
		assert.True(t, ok)
		assert.Equal(t, "test-provider", p.Descriptor().Name)
	})

	t.Run("Get missing provider", func(t *testing.T) {
		_, ok := adapter.Get("nonexistent")
		assert.False(t, ok)
	})

	t.Run("Has existing provider", func(t *testing.T) {
		assert.True(t, adapter.Has("test-provider"))
	})

	t.Run("Has missing provider", func(t *testing.T) {
		assert.False(t, adapter.Has("nonexistent"))
	})
}

// --- WorkflowResult Builder Tests ---

func TestBuildWorkflowResult(t *testing.T) {
	t.Run("succeeded", func(t *testing.T) {
		result := &action.ExecutionResult{
			FinalStatus:    action.ExecutionSucceeded,
			FailedActions:  nil,
			SkippedActions: nil,
		}
		wr := buildWorkflowResult(result)
		assert.Equal(t, "succeeded", wr.FinalStatus)
		assert.Empty(t, wr.FailedActions)
		assert.Empty(t, wr.SkippedActions)
	})

	t.Run("failed", func(t *testing.T) {
		result := &action.ExecutionResult{
			FinalStatus:   action.ExecutionFailed,
			FailedActions: []string{"deploy"},
		}
		wr := buildWorkflowResult(result)
		assert.Equal(t, "failed", wr.FinalStatus)
		assert.Equal(t, []string{"deploy"}, wr.FailedActions)
		assert.Empty(t, wr.SkippedActions)
	})

	t.Run("partial success", func(t *testing.T) {
		result := &action.ExecutionResult{
			FinalStatus:    action.ExecutionPartialSuccess,
			FailedActions:  []string{"deploy"},
			SkippedActions: []string{"notify"},
		}
		wr := buildWorkflowResult(result)
		assert.Equal(t, "partial-success", wr.FinalStatus)
		assert.Equal(t, []string{"deploy"}, wr.FailedActions)
		assert.Equal(t, []string{"notify"}, wr.SkippedActions)
	})
}

// --- Execute Tests ---

func TestExecute_CircularDetection(t *testing.T) {
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./a.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	// Simulate already being inside a.yaml by pushing it as an ancestor
	ctx = WithAncestorStack(ctx, []string{})
	var err error
	ctx, err = PushAncestor(ctx, Canonicalize(ctx, "./a.yaml"))
	require.NoError(t, err)

	// Now try to execute a.yaml again — should detect circular reference
	input := &Input{Source: "./a.yaml"}
	_, execErr := p.Execute(ctx, input)
	require.Error(t, execErr)
	assert.Contains(t, execErr.Error(), "circular reference detected")
}

func TestExecute_MaxDepthExceeded(t *testing.T) {
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./deep.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	// Fill the ancestor stack to exactly maxDepth
	stack := make([]string, 3)
	for i := range stack {
		stack[i] = fmt.Sprintf("level-%d", i)
	}
	ctx = WithAncestorStack(ctx, stack)

	input := &Input{
		Source:   "./deep.yaml",
		MaxDepth: intPtr(3), // stack already has 3 entries, pushed ancestor makes 4
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max nesting depth")
}

func TestExecute_LoaderError(t *testing.T) {
	loader := &mockLoader{
		err: fmt.Errorf("network timeout"),
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	input := &Input{Source: "https://example.com/missing.yaml"}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load")
	assert.Contains(t, err.Error(), "network timeout")
}

func TestExecute_DryRun_From(t *testing.T) {
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./child.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	ctx = provider.WithDryRun(ctx, true)
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)

	input := &Input{Source: "./child.yaml"}
	output, err := p.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, true, data["dryRun"])
	// from capability should NOT have workflow or success
	_, hasWorkflow := data["workflow"]
	assert.False(t, hasWorkflow)
	_, hasSuccess := data["success"]
	assert.False(t, hasSuccess)
}

func TestExecute_DryRun_Action(t *testing.T) {
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./child.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	ctx = provider.WithDryRun(ctx, true)
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)

	input := &Input{Source: "./child.yaml"}
	output, err := p.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, true, data["dryRun"])
	assert.Equal(t, true, data["success"])
	assert.NotNil(t, data["workflow"])
}

func TestExecute_InvalidInputType(t *testing.T) {
	p := New()
	ctx := testContext()

	_, err := p.Execute(ctx, "not-an-input")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *Input")
}

func TestExecute_NoResolvers(t *testing.T) {
	// Solution with no resolvers should return an empty envelope.
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./empty.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)

	input := &Input{Source: "./empty.yaml"}
	output, err := p.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])
	resolvers, ok := data["resolvers"].(map[string]any)
	require.True(t, ok)
	assert.Empty(t, resolvers)
}

func TestExecute_ContextIsolation(t *testing.T) {
	// Verify sub-solution gets isolated context.
	// We can't test the full flow without real providers, but we can test
	// that buildIsolatedContext creates proper isolation.
	ctx := testContext()

	// Set parent resolver data.
	ctx = provider.WithResolverContext(ctx, map[string]any{
		"parent_value": "secret",
	})

	subCtx := buildIsolatedContext(ctx, "child", map[string]any{"param1": "value1"})

	// Sub-context should have empty resolver context.
	resolverCtx, ok := provider.ResolverContextFromContext(subCtx)
	require.True(t, ok)
	assert.Empty(t, resolverCtx)

	// Sub-context should have parameters.
	params, ok := provider.ParametersFromContext(subCtx)
	require.True(t, ok)
	assert.Equal(t, "value1", params["param1"])
}

func TestExecute_ContextIsolation_NilParams(t *testing.T) {
	ctx := testContext()
	subCtx := buildIsolatedContext(ctx, "child", nil)

	// Resolver context should be empty.
	resolverCtx, ok := provider.ResolverContextFromContext(subCtx)
	require.True(t, ok)
	assert.Empty(t, resolverCtx)

	// No parameters should be set.
	_, ok = provider.ParametersFromContext(subCtx)
	assert.False(t, ok)
}

// --- extractResolverData Tests ---

func TestExtractResolverData(t *testing.T) {
	t.Run("with results", func(t *testing.T) {
		rctx := resolver.NewContext()
		rctx.SetResult("greeting", &resolver.ExecutionResult{
			Value:  "hello",
			Status: resolver.ExecutionStatusSuccess,
		})
		rctx.SetResult("failed_one", &resolver.ExecutionResult{
			Value:  nil,
			Status: resolver.ExecutionStatusFailed,
		})

		ctx := context.Background()
		ctx = resolver.WithContext(ctx, rctx)

		sol := newTestSolution(map[string]*resolver.Resolver{
			"greeting":   {},
			"failed_one": {},
		}, nil)

		data := extractResolverData(ctx, sol)
		assert.Equal(t, "hello", data["greeting"])
		_, hasFailed := data["failed_one"]
		assert.False(t, hasFailed)
	})

	t.Run("no resolver context", func(t *testing.T) {
		ctx := context.Background()
		sol := newTestSolution(map[string]*resolver.Resolver{
			"greeting": {},
		}, nil)

		data := extractResolverData(ctx, sol)
		assert.Empty(t, data)
	})
}

// --- New Constructor Tests ---

func TestNew(t *testing.T) {
	loader := &mockLoader{}
	reg := provider.NewRegistry()

	p := New(
		WithLoader(loader),
		WithRegistry(reg),
	)

	assert.NotNil(t, p)
	assert.NotNil(t, p.loader)
	assert.NotNil(t, p.registry)
	assert.NotNil(t, p.descriptor)
}

func TestNew_NoOptions(t *testing.T) {
	p := New()
	assert.NotNil(t, p)
	assert.Nil(t, p.loader)
	assert.Nil(t, p.registry)
	assert.NotNil(t, p.descriptor)
}

// --- ident helpers ---

func TestIsIdentChar(t *testing.T) {
	assert.True(t, isIdentChar('a'))
	assert.True(t, isIdentChar('Z'))
	assert.True(t, isIdentChar('0'))
	assert.True(t, isIdentChar('_'))
	assert.False(t, isIdentChar('.'))
	assert.False(t, isIdentChar(' '))
	assert.False(t, isIdentChar('-'))
}

func TestIsIdentStartChar(t *testing.T) {
	assert.True(t, isIdentStartChar('a'))
	assert.True(t, isIdentStartChar('Z'))
	assert.True(t, isIdentStartChar('_'))
	assert.False(t, isIdentStartChar('0'))
	assert.False(t, isIdentStartChar('.'))
}

// --- filterResolvers Tests ---

func TestFilterResolvers(t *testing.T) {
	allResolvers := []*resolver.Resolver{
		{Name: "greeting"},
		{Name: "echo-param"},
		{Name: "db-config"},
	}

	t.Run("empty allowlist returns all", func(t *testing.T) {
		result, err := filterResolvers(allResolvers, nil)
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("empty slice allowlist returns all", func(t *testing.T) {
		result, err := filterResolvers(allResolvers, []string{})
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("single resolver selected", func(t *testing.T) {
		result, err := filterResolvers(allResolvers, []string{"greeting"})
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "greeting", result[0].Name)
	})

	t.Run("multiple resolvers selected", func(t *testing.T) {
		result, err := filterResolvers(allResolvers, []string{"greeting", "db-config"})
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "greeting", result[0].Name)
		assert.Equal(t, "db-config", result[1].Name)
	})

	t.Run("preserves requested order", func(t *testing.T) {
		result, err := filterResolvers(allResolvers, []string{"db-config", "greeting"})
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "db-config", result[0].Name)
		assert.Equal(t, "greeting", result[1].Name)
	})

	t.Run("nonexistent resolver returns error", func(t *testing.T) {
		_, err := filterResolvers(allResolvers, []string{"greeting", "nonexistent"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nonexistent")
		assert.Contains(t, err.Error(), "does not exist")
	})
}

// --- timeout Tests ---

func TestInput_TimeoutDuration(t *testing.T) {
	t.Run("nil timeout returns false", func(t *testing.T) {
		in := &Input{Source: "test.yaml"}
		_, ok, err := in.timeoutDuration()
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("empty string returns false", func(t *testing.T) {
		empty := ""
		in := &Input{Source: "test.yaml", Timeout: &empty}
		_, ok, err := in.timeoutDuration()
		assert.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("valid duration", func(t *testing.T) {
		s := "30s"
		in := &Input{Source: "test.yaml", Timeout: &s}
		d, ok, err := in.timeoutDuration()
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, 30*time.Second, d)
	})

	t.Run("invalid duration string", func(t *testing.T) {
		s := "not-a-duration"
		in := &Input{Source: "test.yaml", Timeout: &s}
		_, _, err := in.timeoutDuration()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid timeout")
	})

	t.Run("negative duration", func(t *testing.T) {
		s := "-5s"
		in := &Input{Source: "test.yaml", Timeout: &s}
		_, _, err := in.timeoutDuration()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must be positive")
	})
}

// --- decodeInput Tests for new fields ---

func TestDecodeInput_Resolvers(t *testing.T) {
	t.Run("resolvers as []any of strings", func(t *testing.T) {
		result, err := decodeInput(map[string]any{
			"source":    "child.yaml",
			"resolvers": []any{"greeting", "config"},
		})
		require.NoError(t, err)
		in := result.(*Input)
		assert.Equal(t, []string{"greeting", "config"}, in.Resolvers)
	})

	t.Run("resolvers as []string", func(t *testing.T) {
		result, err := decodeInput(map[string]any{
			"source":    "child.yaml",
			"resolvers": []string{"greeting"},
		})
		require.NoError(t, err)
		in := result.(*Input)
		assert.Equal(t, []string{"greeting"}, in.Resolvers)
	})

	t.Run("resolvers with non-string element", func(t *testing.T) {
		_, err := decodeInput(map[string]any{
			"source":    "child.yaml",
			"resolvers": []any{"greeting", 42},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resolvers[1]")
	})

	t.Run("resolvers invalid type", func(t *testing.T) {
		_, err := decodeInput(map[string]any{
			"source":    "child.yaml",
			"resolvers": "not-an-array",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "array of strings")
	})
}

func TestDecodeInput_Timeout(t *testing.T) {
	t.Run("valid timeout", func(t *testing.T) {
		result, err := decodeInput(map[string]any{
			"source":  "child.yaml",
			"timeout": "30s",
		})
		require.NoError(t, err)
		in := result.(*Input)
		require.NotNil(t, in.Timeout)
		assert.Equal(t, "30s", *in.Timeout)
	})

	t.Run("timeout invalid type", func(t *testing.T) {
		_, err := decodeInput(map[string]any{
			"source":  "child.yaml",
			"timeout": 30,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})
}

func TestBuildDescriptor_WhatIf(t *testing.T) {
	p := New()
	ctx := context.Background()
	desc := p.Descriptor()
	require.NotNil(t, desc.WhatIf)

	t.Run("struct input with source", func(t *testing.T) {
		src := "my-solution.yaml"
		input := &Input{Source: src}
		msg, err := desc.WhatIf(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, msg, "my-solution.yaml")
	})

	t.Run("map input with source", func(t *testing.T) {
		input := map[string]any{"source": "child.yaml"}
		msg, err := desc.WhatIf(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, msg, "child.yaml")
	})

	t.Run("map input without source", func(t *testing.T) {
		input := map[string]any{}
		msg, err := desc.WhatIf(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, msg, "sub-solution")
	})

	t.Run("unknown input type", func(t *testing.T) {
		msg, err := desc.WhatIf(ctx, 42)
		require.NoError(t, err)
		assert.Contains(t, msg, "sub-solution")
	})
}

func TestExtractRefsFromValue_Default(t *testing.T) {
	// nil and other non-string/non-map types hit the default branch
	result := extractRefsFromValue(nil)
	assert.Nil(t, result)

	result = extractRefsFromValue(42)
	assert.Nil(t, result)

	result = extractRefsFromValue([]string{"a", "b"})
	assert.Nil(t, result)
}
