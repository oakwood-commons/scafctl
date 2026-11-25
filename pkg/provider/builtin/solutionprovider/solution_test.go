// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"text/template"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
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

func TestExecute_NilLoader(t *testing.T) {
	// A bare provider (no loader, no registry) should return a clear error.
	p := New()
	_, err := p.Execute(context.Background(), map[string]any{
		"source": "child-solution",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution loader configured")
}

func TestExecute_NilLoaderWithRegistryOnly(t *testing.T) {
	// Provider with registry but no loader should still error about loader.
	p := New(WithRegistry(provider.NewRegistry()))
	_, err := p.Execute(context.Background(), map[string]any{
		"source": "child-solution",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no solution loader configured")
}

func TestExecute_LoaderFromContext(t *testing.T) {
	// A bare provider should resolve loader and registry from context.
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"ctx-child": {
				APIVersion: "scafctl.io/v1",
				Kind:       "Solution",
				Metadata:   solution.Metadata{Name: "ctx-child"},
				Spec:       solution.Spec{},
			},
		},
	}
	reg := provider.NewRegistry()
	ctx := WithLoaderCtx(context.Background(), loader)
	ctx = WithProviderRegistry(ctx, reg)

	p := New() // bare — no struct-level deps
	out, err := p.Execute(ctx, map[string]any{
		"source": "ctx-child",
	})
	require.NoError(t, err)
	data, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])
}

func TestConfigure(t *testing.T) {
	p := New()
	assert.Nil(t, p.loader)
	assert.Nil(t, p.registry)

	loader := &mockLoader{}
	reg := provider.NewRegistry()
	p.Configure(WithLoader(loader), WithRegistry(reg))

	assert.NotNil(t, p.loader)
	assert.NotNil(t, p.registry)
}

func TestContextRoundTrip(t *testing.T) {
	ctx := context.Background()

	t.Run("OfficialProviders", func(t *testing.T) {
		reg := official.NewRegistry()
		ctx := WithOfficialProvidersCtx(ctx, reg)
		assert.Equal(t, reg, OfficialProvidersFromContext(ctx))
	})

	t.Run("PluginFetcher", func(t *testing.T) {
		f := &plugin.Fetcher{}
		ctx := WithPluginFetcherCtx(ctx, f)
		assert.Equal(t, f, PluginFetcherFromContext(ctx))
	})

	t.Run("PluginConfig", func(t *testing.T) {
		cfg := &plugin.ProviderConfig{}
		ctx := WithPluginConfigCtx(ctx, cfg)
		assert.Equal(t, cfg, PluginConfigFromContext(ctx))
	})

	t.Run("ClientOptions", func(t *testing.T) {
		opts := []plugin.ClientOption{}
		ctx := WithClientOptionsCtx(ctx, opts)
		assert.Equal(t, opts, ClientOptionsFromContext(ctx))
	})

	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, OfficialProvidersFromContext(ctx))
		assert.Nil(t, PluginFetcherFromContext(ctx))
		assert.Nil(t, PluginConfigFromContext(ctx))
		assert.Nil(t, ClientOptionsFromContext(ctx))
	})

	t.Run("ChildClientTracker", func(t *testing.T) {
		tracker := NewChildClientTracker()
		ctx := WithChildClientTracker(ctx, tracker)
		assert.Equal(t, tracker, ChildClientTrackerFromContext(ctx))
	})

	t.Run("ChildClientTracker nil", func(t *testing.T) {
		assert.Nil(t, ChildClientTrackerFromContext(ctx))
	})
}

func TestChildClientTracker(t *testing.T) {
	t.Run("Add and Close", func(t *testing.T) {
		tracker := NewChildClientTracker()
		// Close on empty tracker should not panic.
		tracker.Close()

		tracker.Add()                    // no-op, zero clients
		assert.Empty(t, tracker.clients) //nolint:govet // test accesses unexported field
	})

	t.Run("Close clears list", func(t *testing.T) {
		tracker := NewChildClientTracker()
		tracker.Close()
		// After Close, clients should be nil.
		assert.Nil(t, tracker.clients)
	})
}

func TestContextOverridesStructFields(t *testing.T) {
	// When both struct fields and context are set, context should win.
	structLoader := &mockLoader{solutions: map[string]*solution.Solution{
		"struct-child": newTestSolution(nil, nil),
	}}
	ctxLoader := &mockLoader{solutions: map[string]*solution.Solution{
		"ctx-child": newTestSolution(nil, nil),
	}}
	structReg := provider.NewRegistry()
	ctxReg := provider.NewRegistry()

	p := New(WithLoader(structLoader), WithRegistry(structReg))

	// Put different deps in context.
	ctx := WithLoaderCtx(context.Background(), ctxLoader)
	ctx = WithProviderRegistry(ctx, ctxReg)

	// Execute with a source that only the context loader knows about.
	out, err := p.Execute(ctx, map[string]any{
		"source": "ctx-child",
	})
	require.NoError(t, err)
	data, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])

	// Verify the struct loader's solution is NOT loaded (context won).
	_, err = p.Execute(ctx, map[string]any{
		"source": "struct-child",
	})
	// struct-child is not in ctxLoader, so this should fail with a load error.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load")
}

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

func TestExecute_LoadFailure_PropagateFalse_From(t *testing.T) {
	loader := &mockLoader{
		err: fmt.Errorf("YAML parse error at line 12"),
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)
	input := &Input{
		Source:          "./broken-child.yaml",
		PropagateErrors: boolPtr(false),
	}

	output, err := p.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "failed", data["status"])

	errors, ok := data["errors"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, errors, 1)
	assert.Equal(t, "_loader", errors[0]["resolver"])
	assert.Contains(t, errors[0]["message"], "YAML parse error")
	assert.Contains(t, errors[0]["message"], "./broken-child.yaml")

	// from capability should not have workflow or success fields
	_, hasWorkflow := data["workflow"]
	assert.False(t, hasWorkflow)
	_, hasSuccess := data["success"]
	assert.False(t, hasSuccess)

	// Should have a warning
	require.NotEmpty(t, output.Warnings)
	assert.Contains(t, output.Warnings[0], "failed to load")
}

func TestExecute_LoadFailure_PropagateFalse_Action(t *testing.T) {
	loader := &mockLoader{
		err: fmt.Errorf("child resolver cycle detected"),
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)
	input := &Input{
		Source:          "./cyclic-child.yaml",
		PropagateErrors: boolPtr(false),
	}

	output, err := p.Execute(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, output)

	data, ok := output.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "failed", data["status"])
	assert.Equal(t, false, data["success"])

	errors, ok := data["errors"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, errors, 1)
	assert.Equal(t, "_loader", errors[0]["resolver"])
	assert.Contains(t, errors[0]["message"], "resolver cycle")
	assert.Contains(t, errors[0]["message"], "./cyclic-child.yaml")

	require.NotEmpty(t, output.Warnings)
	assert.Contains(t, output.Warnings[0], "failed to load")
}

func TestExecute_CircularRef_PropagateFalse_StillErrors(t *testing.T) {
	// Circular reference detection is a hard error regardless of propagateErrors.
	loader := &mockLoader{
		solutions: map[string]*solution.Solution{
			"./self.yaml": newTestSolution(nil, nil),
		},
	}

	p := New(
		WithLoader(loader),
		WithRegistry(provider.NewRegistry()),
	)

	ctx := testContext()
	// Push the canonical name as ancestor to simulate a cycle.
	ctx = WithAncestorStack(ctx, []string{})
	var err error
	ctx, err = PushAncestor(ctx, Canonicalize(ctx, "./self.yaml"))
	require.NoError(t, err)

	input := &Input{
		Source:          "./self.yaml",
		PropagateErrors: boolPtr(false),
	}

	_, execErr := p.Execute(ctx, input)
	require.Error(t, execErr)
	assert.Contains(t, execErr.Error(), "circular")
}

func TestExecute_DepthExceeded_PropagateFalse_StillErrors(t *testing.T) {
	// Depth limit is a hard error regardless of propagateErrors.
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
	// Push enough ancestors to exceed MaxDepth of 1.
	ctx = WithAncestorStack(ctx, []string{"/parent.yaml"})

	input := &Input{
		Source:          "./deep.yaml",
		MaxDepth:        intPtr(1),
		PropagateErrors: boolPtr(false),
	}

	_, err := p.Execute(ctx, input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "depth")
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

type dummyBinder struct{}

func (dummyBinder) Bind(context.Context) template.FuncMap {
	return template.FuncMap{"parentOnly": func() string { return "leaked" }}
}

func (dummyBinder) Fingerprint() string { return "parent-fp" }

func TestExecute_ContextIsolation(t *testing.T) {
	ctx := testContext()

	// Populate parent-only state across all isolated keys.
	ctx = provider.WithResolverContext(ctx, map[string]any{"parent_value": "secret"})
	ctx = provider.WithParameters(ctx, map[string]any{"parent_param": "leaked"})
	ctx = provider.WithIterationContext(ctx, &provider.IterationContext{
		Item:      "server-a",
		Index:     2,
		ItemAlias: "srv",
	})
	ctx = provider.WithTemplateFuncBinder(ctx, dummyBinder{})
	ctx = state.WithState(ctx, &state.Data{})
	ctx = provider.WithSecretProperties(ctx, map[string]bool{"source": true})
	ctx = provider.WithPreResolvedInputs(ctx)
	ctx = provider.WithDeclaredScalarType(ctx, "int")
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)
	ctx = provider.WithWorkingDirectory(ctx, "/parent/work")
	ctx = provider.WithSolutionDirectory(ctx, "/parent/solution")
	ctx = provider.WithOutputDirectory(ctx, "/parent/output")
	parentResCtx := resolver.NewContext()
	parentResCtx.Set("parent_res", "secret")
	ctx = resolver.WithContext(ctx, parentResCtx)

	subCtx := buildIsolatedContext(ctx, "child", map[string]any{"param1": "value1"}, "/child/solution")

	// Sub-context has empty resolver context.
	resolverCtx, ok := provider.ResolverContextFromContext(subCtx)
	require.True(t, ok)
	assert.Empty(t, resolverCtx)

	// Sub-context replaces parent parameters with explicit inputs.
	params, ok := provider.ParametersFromContext(subCtx)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"param1": "value1"}, params)

	// Parent working directory is masked; solution directory is replaced with child's;
	// parent output directory remains visible for child action writes.
	workDir, hasWork := provider.WorkingDirectoryFromContext(subCtx)
	assert.False(t, hasWork)
	assert.Empty(t, workDir)

	solDir, hasSol := provider.SolutionDirectoryFromContext(subCtx)
	assert.True(t, hasSol)
	assert.Equal(t, "/child/solution", solDir)

	outDir, hasOut := provider.OutputDirectoryFromContext(subCtx)
	assert.True(t, hasOut)
	assert.Equal(t, "/parent/output", outDir)

	// Parent iteration context, template func binder, state data, and step metadata are stripped.
	iterCtx, ok := provider.IterationContextFromContext(subCtx)
	assert.False(t, ok)
	assert.Nil(t, iterCtx)

	binder, ok := provider.TemplateFuncBinderFromContext(subCtx)
	assert.False(t, ok)
	assert.Nil(t, binder)

	stData, ok := state.FromContext(subCtx)
	assert.False(t, ok)
	assert.Nil(t, stData)

	assert.Nil(t, provider.SecretPropertiesFromContext(subCtx))
	assert.False(t, provider.PreResolvedInputsFromContext(subCtx))

	scalarType, ok := provider.DeclaredScalarTypeFromContext(subCtx)
	assert.False(t, ok)
	assert.Empty(t, scalarType)

	mode, ok := provider.ExecutionModeFromContext(subCtx)
	assert.False(t, ok)
	assert.Empty(t, mode)

	rc, ok := resolver.FromContext(subCtx)
	assert.False(t, ok)
	assert.Nil(t, rc)
}

func TestExecute_ContextIsolation_NilParams(t *testing.T) {
	ctx := provider.WithParameters(testContext(), map[string]any{"parent_param": "leaked"})
	ctx = provider.WithSolutionDirectory(ctx, "/parent/solution")
	subCtx := buildIsolatedContext(ctx, "child", nil, "")

	// Resolver context should be empty.
	resolverCtx, ok := provider.ResolverContextFromContext(subCtx)
	require.True(t, ok)
	assert.Empty(t, resolverCtx)

	// Parent parameters and parent solution directory are stripped when childSolutionDir is empty.
	_, ok = provider.ParametersFromContext(subCtx)
	assert.False(t, ok)
	_, hasSol := provider.SolutionDirectoryFromContext(subCtx)
	assert.False(t, hasSol)
}

// --- Composition, Workflow Filtering & Inheritance Tests ---

type callbackProvider struct {
	name string
	fn   func(ctx context.Context, input any) (*provider.Output, error)
}

func (c *callbackProvider) Descriptor() *provider.Descriptor {
	v := semver.MustParse("1.0.0")
	return &provider.Descriptor{
		Name:        c.name,
		APIVersion:  "v1",
		Version:     v,
		Description: "callback provider for testing composed execution",
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityAction,
		},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: schemahelper.AnyProp("value"),
			provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("success"),
			}),
		},
	}
}

func (c *callbackProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	return c.fn(ctx, input)
}

type progressLog struct {
	events []string
}

func (p *progressLog) OnActionStart(name, _ string) {
	p.events = append(p.events, "start:"+name)
}

func (p *progressLog) OnActionComplete(name string, _ any) {
	p.events = append(p.events, "complete:"+name)
}

func (p *progressLog) OnActionFailed(name string, _ error) {
	p.events = append(p.events, "failed:"+name)
}

func (p *progressLog) OnActionSkipped(name, _ string) {
	p.events = append(p.events, "skipped:"+name)
}

func (p *progressLog) OnActionTimeout(name string, _ time.Duration) {
	p.events = append(p.events, "timeout:"+name)
}

func (p *progressLog) OnActionCancelled(name string) {
	p.events = append(p.events, "cancelled:"+name)
}
func (p *progressLog) OnRetryAttempt(string, int, int, error) {}
func (p *progressLog) OnForEachProgress(string, int, int)     {}
func (p *progressLog) OnPhaseStart(int, []string)             {}
func (p *progressLog) OnPhaseComplete(int)                    {}
func (p *progressLog) OnFinallyStart()                        {}
func (p *progressLog) OnFinallyComplete()                     {}

func TestExecute_ActionMode_NoWorkflow_ReturnsActionEnvelope(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{
		name:   "static",
		output: &provider.Output{Data: "hello"},
	}))

	child := newTestSolution(map[string]*resolver.Resolver{
		"greeting": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
	}, nil)

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
	out, err := p.Execute(ctx, &Input{Source: "child"})
	require.NoError(t, err)
	require.NotNil(t, out)

	data, ok := out.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, true, data["success"], "action capability must emit success boolean even without child workflow")
	_, hasWorkflow := data["workflow"]
	assert.False(t, hasWorkflow)
	resolversMap, ok := data["resolvers"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "hello", resolversMap["greeting"])
}

func TestExecute_ActionMode_ExplicitActionsExcluded(t *testing.T) {
	var mu sync.Mutex
	var executed []string

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&callbackProvider{
		name: "recorder",
		fn: func(_ context.Context, input any) (*provider.Output, error) {
			m, _ := input.(map[string]any)
			step, _ := m["step"].(string)
			mu.Lock()
			executed = append(executed, step)
			mu.Unlock()
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	lit := func(v string) *spec.ValueRef { return &spec.ValueRef{Literal: v} }
	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "child-explicit"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"prep": {
						Provider: "recorder",
						Explicit: true,
						Inputs:   map[string]*spec.ValueRef{"step": lit("prep")},
					},
					"build": {
						Provider:  "recorder",
						DependsOn: []string{"prep"},
						Inputs:    map[string]*spec.ValueRef{"step": lit("build")},
					},
					"deploy-optional": {
						Provider: "recorder",
						Explicit: true,
						Inputs:   map[string]*spec.ValueRef{"step": lit("deploy-optional")},
					},
					"post-optional": {
						Provider:  "recorder",
						Explicit:  true,
						DependsOn: []string{"deploy-optional"},
						Inputs:    map[string]*spec.ValueRef{"step": lit("post-optional")},
					},
				},
				Finally: map[string]*action.Action{
					"cleanup": {
						Provider: "recorder",
						Inputs:   map[string]*spec.ValueRef{"step": lit("cleanup")},
					},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
	out, err := p.Execute(ctx, &Input{Source: "child"})
	require.NoError(t, err)

	data := out.Data.(map[string]any)
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, true, data["success"])

	assert.Equal(t, []string{"prep", "build", "cleanup"}, executed,
		"non-explicit action must pull in its explicit dependency and run finally, while standalone explicit actions stay excluded")
}

func TestExecute_ActionMode_InvalidWorkflow_Validation(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{
		name:   "static",
		output: &provider.Output{Data: "resolved-val"},
	}))

	// Action references unregistered provider -> fails action.ValidateWorkflow.
	invalidChild := &solution.Solution{
		Metadata: solution.Metadata{Name: "bad-wf"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "static"}},
					},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"broken": {Provider: "missing-provider"},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"bad": invalidChild}}),
		WithRegistry(reg),
	)

	t.Run("propagateErrors true returns validation error", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
		_, err := p.Execute(ctx, &Input{Source: "bad"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow validation failed")
	})

	t.Run("propagateErrors false records _workflow error in failed envelope", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
		out, err := p.Execute(ctx, &Input{
			Source:          "bad",
			PropagateErrors: boolPtr(false),
		})
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NotEmpty(t, out.Warnings)

		data := out.Data.(map[string]any)
		assert.Equal(t, "failed", data["status"])
		assert.Equal(t, false, data["success"])

		resolversMap := data["resolvers"].(map[string]any)
		assert.Equal(t, "resolved-val", resolversMap["r1"], "resolved values must survive workflow validation failure")

		errs := data["errors"].([]map[string]any)
		require.Len(t, errs, 1)
		assert.Equal(t, "_workflow", errs[0]["resolver"])
		assert.Contains(t, errs[0]["message"], "workflow validation failed")
	})
}

func TestExecute_ActionMode_WorkflowFailure_PropagateFalse_PreservesPartialResult(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&callbackProvider{
		name: "failer",
		fn: func(_ context.Context, input any) (*provider.Output, error) {
			m, _ := input.(map[string]any)
			if m["fail"] == true {
				return nil, errors.New("boom")
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "partial-wf"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step1": {
						Provider: "failer",
						Inputs:   map[string]*spec.ValueRef{"fail": {Literal: true}},
					},
					"step2": {
						Provider:  "failer",
						DependsOn: []string{"step1"},
						Inputs:    map[string]*spec.ValueRef{"fail": {Literal: false}},
					},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
	out, err := p.Execute(ctx, &Input{
		Source:          "child",
		PropagateErrors: boolPtr(false),
	})
	require.NoError(t, err)
	require.NotEmpty(t, out.Warnings)

	data := out.Data.(map[string]any)
	assert.Equal(t, "failed", data["status"])
	assert.Equal(t, false, data["success"])

	wf, ok := data["workflow"].(map[string]any)
	require.True(t, ok, "PartialResult must populate workflow summary on action failure")
	assert.Equal(t, "failed", wf["finalStatus"])
	assert.Equal(t, []string{"step1"}, wf["failedActions"])
	assert.Equal(t, []string{"step2"}, wf["skippedActions"])
}

func TestExecute_PartialResolverValuesOnFailure_PropagateFalse(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{
		name:   "ok-prov",
		output: &provider.Output{Data: "good-val"},
	}))
	require.NoError(t, reg.Register(&mockProvider{
		name: "bad-prov",
		err:  errors.New("step2 failed"),
	}))

	child := newTestSolution(map[string]*resolver.Resolver{
		"good": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "ok-prov"}},
			},
		},
		"bad": {
			DependsOn: []string{"good"},
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "bad-prov"}},
			},
		},
	}, nil)

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)
	out, err := p.Execute(ctx, &Input{
		Source:          "child",
		PropagateErrors: boolPtr(false),
	})
	require.NoError(t, err)
	require.NotEmpty(t, out.Warnings)

	data := out.Data.(map[string]any)
	assert.Equal(t, "failed", data["status"])
	resolversMap := data["resolvers"].(map[string]any)
	assert.Equal(t, "good-val", resolversMap["good"])
}

func TestExecute_ChildMetadataAndCELCostLimit(t *testing.T) {
	globalLimit := celexp.GetDefaultCostLimit()
	if globalLimit == 0 {
		t.Skip("global CEL cost limit is disabled")
	}

	var capturedMeta *provider.SolutionMeta
	var capturedLimit uint64
	var hasLimit bool

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&callbackProvider{
		name: "inspector",
		fn: func(ctx context.Context, _ any) (*provider.Output, error) {
			capturedMeta, _ = provider.SolutionMetadataFromContext(ctx)
			capturedLimit, hasLimit = celexp.CostLimitFromContext(ctx)
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	childVer := semver.MustParse("2.3.4")
	childCost := uint64(800)
	child := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "child-meta-sol",
			Version: childVer,
		},
		Spec: solution.Spec{
			Options: &solution.SpecOptions{
				CEL: &solution.CELOptions{CostLimit: &childCost},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"inspect": {Provider: "inspector"},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	t.Run("child receives its own metadata and CEL cost limit", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
		ctx = provider.WithSolutionMetadata(ctx, &provider.SolutionMeta{Name: "parent-sol"})

		_, err := p.Execute(ctx, &Input{Source: "child"})
		require.NoError(t, err)
		require.NotNil(t, capturedMeta)
		assert.Equal(t, "child-meta-sol", capturedMeta.Name)
		assert.Equal(t, "2.3.4", capturedMeta.Version)
		assert.True(t, hasLimit)
		assert.Equal(t, uint64(800), capturedLimit)
	})

	t.Run("parent CEL cost limit caps higher child limit", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
		ctx = celexp.ContextWithCostLimit(ctx, 300)

		_, err := p.Execute(ctx, &Input{Source: "child"})
		require.NoError(t, err)
		assert.True(t, hasLimit)
		assert.Equal(t, uint64(300), capturedLimit)
	})
}

func TestExecute_InheritsParentExecutorConfig(t *testing.T) {
	t.Run("inherits max concurrency, progress prefix, ioStreams, and cwd", func(t *testing.T) {
		var inFlight atomic.Int32
		var maxInFlight atomic.Int32
		var streamBuf bytes.Buffer
		var observedCwd string
		var mu sync.Mutex

		reg := provider.NewRegistry()
		solProv := New(WithRegistry(reg))
		require.NoError(t, reg.Register(solProv))
		require.NoError(t, reg.Register(&callbackProvider{
			name: "worker",
			fn: func(ctx context.Context, input any) (*provider.Output, error) {
				cur := inFlight.Add(1)
				for {
					prev := maxInFlight.Load()
					if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(15 * time.Millisecond)
				inFlight.Add(-1)

				if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil && streams.Out != nil {
					mu.Lock()
					_, _ = streams.Out.Write([]byte("x"))
					mu.Unlock()
				}
				if m, ok := input.(map[string]any); ok {
					if cwd, isStr := m["cwd"].(string); isStr && cwd != "" {
						mu.Lock()
						observedCwd = cwd
						mu.Unlock()
					}
				}
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			},
		}))

		cwdExpr := celexp.Expression("__cwd")
		child := &solution.Solution{
			Metadata: solution.Metadata{Name: "child-conc"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"a1": {Provider: "worker", Inputs: map[string]*spec.ValueRef{"cwd": {Expr: &cwdExpr}}},
						"a2": {Provider: "worker"},
						"a3": {Provider: "worker"},
					},
				},
			},
		}
		solProv.Configure(WithLoader(&mockLoader{
			solutions: map[string]*solution.Solution{"child-pkg@1.0.0": child},
		}))

		rec := &progressLog{}
		streams := &provider.IOStreams{Out: &streamBuf}
		parentExec := action.NewExecutor(
			action.WithRegistry(reg),
			action.WithMaxConcurrency(1),
			action.WithProgressCallback(rec),
			action.WithIOStreams(streams),
			action.WithCwd("/inherited/parent/cwd"),
		)

		parentWorkflow := &action.Workflow{
			Actions: map[string]*action.Action{
				"invoke-child": {
					Provider: ProviderName,
					Inputs: map[string]*spec.ValueRef{
						"source": {Literal: "child-pkg@1.0.0"},
					},
				},
			},
		}

		res, err := parentExec.Execute(testContext(), parentWorkflow)
		require.NoError(t, err)
		assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
		assert.Equal(t, int32(1), maxInFlight.Load(), "child workflow must respect parent MaxConcurrency=1")
		assert.Equal(t, "xxx", streamBuf.String(), "child actions must inherit parent IOStreams")
		assert.Equal(t, "/inherited/parent/cwd", observedCwd, "child actions must inherit parent Cwd")

		assert.Contains(t, rec.events, "start:invoke-child")
		assert.Contains(t, rec.events, "start:[solution:child-pkg@1.0.0] a1")
		assert.Contains(t, rec.events, "complete:[solution:child-pkg@1.0.0] a1")
		assert.Contains(t, rec.events, "complete:invoke-child")
	})

	t.Run("inherits parent default timeout", func(t *testing.T) {
		reg := provider.NewRegistry()
		solProv := New(WithRegistry(reg))
		require.NoError(t, reg.Register(solProv))
		require.NoError(t, reg.Register(&callbackProvider{
			name: "sleeper",
			fn: func(ctx context.Context, _ any) (*provider.Output, error) {
				select {
				case <-time.After(2 * time.Second):
					return &provider.Output{Data: map[string]any{"success": true}}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}))

		child := &solution.Solution{
			Metadata: solution.Metadata{Name: "child-timeout"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"slow": {Provider: "sleeper"},
					},
				},
			},
		}
		solProv.Configure(WithLoader(&mockLoader{
			solutions: map[string]*solution.Solution{"child-slow": child},
		}))

		parentExec := action.NewExecutor(
			action.WithRegistry(reg),
			action.WithDefaultTimeout(40*time.Millisecond),
		)

		parentWorkflow := &action.Workflow{
			Actions: map[string]*action.Action{
				"invoke": {
					Provider: ProviderName,
					Inputs: map[string]*spec.ValueRef{
						"source": {Literal: "child-slow"},
					},
				},
			},
		}

		start := time.Now()
		_, err := parentExec.Execute(testContext(), parentWorkflow)
		require.Error(t, err)
		assert.Less(t, time.Since(start), time.Second, "child action must time out quickly under inherited DefaultTimeout")
	})
}

func TestExecute_ConcurrentSharedSolution_RaceFree(t *testing.T) {
	reg := provider.NewRegistry()
	solProv := New(WithRegistry(reg))
	require.NoError(t, reg.Register(solProv))
	require.NoError(t, reg.Register(&callbackProvider{
		name: "noop-step",
		fn: func(_ context.Context, _ any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	// Intentionally leave Resolver.Name and Action.Name empty on the shared
	// struct literal to verify concurrent executions do not race under -race.
	sharedChild := &solution.Solution{
		Metadata: solution.Metadata{Name: "shared-child"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"val": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "noop-step"}},
					},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step-a": {Provider: "noop-step"},
					"step-b": {Provider: "noop-step", DependsOn: []string{"step-a"}},
				},
				Finally: map[string]*action.Action{
					"cleanup": {Provider: "noop-step"},
				},
			},
		},
	}
	solProv.Configure(WithLoader(&mockLoader{
		solutions: map[string]*solution.Solution{"shared@1.0.0": sharedChild},
	}))

	rec := &progressLog{}
	parentActions := make(map[string]*action.Action, 8)
	for i := range 8 {
		parentActions[fmt.Sprintf("caller-%d", i)] = &action.Action{
			Provider: ProviderName,
			Inputs: map[string]*spec.ValueRef{
				"source": {Literal: "shared@1.0.0"},
			},
		}
	}

	parentExec := action.NewExecutor(
		action.WithRegistry(reg),
		action.WithProgressCallback(rec),
	)
	res, err := parentExec.Execute(testContext(), &action.Workflow{Actions: parentActions})
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
	assert.NotEmpty(t, rec.events)
}

func TestExecute_ActionMode_RedactsSensitiveChildResolvers(t *testing.T) {
	var actionObservedPin string
	var actionObservedCode any

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{
		name:   "short-pin",
		output: &provider.Output{Data: "99"},
	}))
	require.NoError(t, reg.Register(&mockProvider{
		name:   "num-code",
		output: &provider.Output{Data: 7},
	}))
	require.NoError(t, reg.Register(&mockProvider{
		name:   "pub-val",
		output: &provider.Output{Data: "visible"},
	}))
	require.NoError(t, reg.Register(&callbackProvider{
		name: "consumer",
		fn: func(_ context.Context, input any) (*provider.Output, error) {
			m, _ := input.(map[string]any)
			actionObservedPin, _ = m["pin"].(string)
			actionObservedCode = m["code"]
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	pinExpr := celexp.Expression("_.pin")
	codeExpr := celexp.Expression("_.code")
	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "child-sensitive"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"pin": {
					Sensitive: true,
					Resolve:   &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "short-pin"}}},
				},
				"code": {
					Sensitive: true,
					Resolve:   &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "num-code"}}},
				},
				"public": {
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "pub-val"}}},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"consume": {
						Provider: "consumer",
						Inputs: map[string]*spec.ValueRef{
							"pin":  {Expr: &pinExpr},
							"code": {Expr: &codeExpr},
						},
					},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"child": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
	out, err := p.Execute(ctx, &Input{Source: "child"})
	require.NoError(t, err)

	// Child actions still consume raw sensitive values internally.
	assert.Equal(t, "99", actionObservedPin)
	assert.EqualValues(t, 7, actionObservedCode)

	// Parent-visible action envelope masks sensitive resolvers regardless of length or scalar type.
	data := out.Data.(map[string]any)
	resolversMap := data["resolvers"].(map[string]any)
	assert.Equal(t, redact.RedactedPlaceholder, resolversMap["pin"])
	assert.Equal(t, redact.RedactedPlaceholder, resolversMap["code"])
	assert.Equal(t, "visible", resolversMap["public"])
}

func TestExecute_TimeoutVsCallerCancellation(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&callbackProvider{
		name: "slow",
		fn: func(ctx context.Context, _ any) (*provider.Output, error) {
			select {
			case <-time.After(2 * time.Second):
				return &provider.Output{Data: "late"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}))

	child := newTestSolution(map[string]*resolver.Resolver{
		"r": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "slow"}}}},
	}, nil)

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"slow-child": child}}),
		WithRegistry(reg),
	)

	short := "25ms"

	t.Run("input timeout with propagateErrors false returns failed envelope", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)
		out, err := p.Execute(ctx, &Input{
			Source:          "slow-child",
			Timeout:         &short,
			PropagateErrors: boolPtr(false),
		})
		require.NoError(t, err)
		data := out.Data.(map[string]any)
		assert.Equal(t, "failed", data["status"])
	})

	t.Run("input timeout with propagateErrors true returns error", func(t *testing.T) {
		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)
		_, err := p.Execute(ctx, &Input{
			Source:  "slow-child",
			Timeout: &short,
		})
		require.Error(t, err)
	})

	t.Run("caller context cancellation propagates even when propagateErrors is false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(provider.WithExecutionMode(testContext(), provider.CapabilityFrom))
		cancel()

		_, err := p.Execute(ctx, &Input{
			Source:          "slow-child",
			PropagateErrors: boolPtr(false),
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestExecute_ActionMode_GraphBuildFailure_PropagateFalse(t *testing.T) {
	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&mockProvider{
		name:   "noop",
		output: &provider.Output{Data: map[string]any{"success": true}},
	}))

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "bad-graph"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step": {
						Provider: "noop",
						ForEach:  &spec.ForEachClause{In: &spec.ValueRef{Literal: "not-an-array"}},
					},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"bad-graph": child}}),
		WithRegistry(reg),
	)

	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
	out, err := p.Execute(ctx, &Input{
		Source:          "bad-graph",
		PropagateErrors: boolPtr(false),
	})
	require.NoError(t, err)
	data := out.Data.(map[string]any)
	assert.Equal(t, "failed", data["status"])
	assert.Equal(t, false, data["success"])

	errs := data["errors"].([]map[string]any)
	require.Len(t, errs, 1)
	assert.Equal(t, "_workflow", errs[0]["resolver"])
	assert.Contains(t, errs[0]["message"], "forEach.in must evaluate to an array")
}

func TestExecute_SolutionDirectoryCanonicalizesRelativeSource(t *testing.T) {
	reg := provider.NewRegistry()
	solProv := New(WithRegistry(reg))
	require.NoError(t, reg.Register(solProv))
	require.NoError(t, reg.Register(&mockProvider{
		name:   "noop",
		output: &provider.Output{Data: map[string]any{"success": true}},
	}))

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "nested-child"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step": {Provider: "noop"},
				},
			},
		},
	}

	const resolvedPath = "/bundle/parent/sub/child.yaml"
	solProv.Configure(WithLoader(&mockLoader{
		solutions: map[string]*solution.Solution{resolvedPath: child},
	}))

	rec := &progressLog{}
	parentExec := action.NewExecutor(
		action.WithRegistry(reg),
		action.WithProgressCallback(rec),
	)

	ctx := testContext()
	ctx = provider.WithWorkingDirectory(ctx, "/unrelated/cwd")
	ctx = provider.WithSolutionDirectory(ctx, "/bundle/parent")

	res, err := parentExec.Execute(ctx, &action.Workflow{
		Actions: map[string]*action.Action{
			"run-nested": {
				Provider: ProviderName,
				Inputs: map[string]*spec.ValueRef{
					"source": {Literal: "./sub/child.yaml"},
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
	assert.Contains(t, rec.events, "start:[solution:/bundle/parent/sub/child.yaml] step")
}

func TestExecute_PreservesParentZeroConcurrencyAndEmptyOutputDirOverrides(t *testing.T) {
	unwantedOutDir := filepath.Join(t.TempDir(), "app-config-default-out")
	appCfg := &config.Config{
		Action: config.ActionConfig{
			DefaultTimeout: "30s",
			GracePeriod:    "5s",
			MaxConcurrency: 1,
			OutputDir:      unwantedOutDir,
		},
	}

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once

	reg := provider.NewRegistry()
	solProv := New(WithRegistry(reg))
	require.NoError(t, reg.Register(solProv))
	require.NoError(t, reg.Register(&callbackProvider{
		name: "barrier-worker",
		fn: func(ctx context.Context, _ any) (*provider.Output, error) {
			outDir, hasOut := provider.OutputDirectoryFromContext(ctx)
			if hasOut && outDir != "" {
				return nil, fmt.Errorf("unexpected child output directory %q", outDir)
			}
			cur := inFlight.Add(1)
			for {
				prev := maxInFlight.Load()
				if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
					break
				}
			}
			if cur >= 2 {
				releaseOnce.Do(func() { close(release) })
			}
			select {
			case <-release:
			case <-ctx.Done():
				inFlight.Add(-1)
				return nil, ctx.Err()
			}
			inFlight.Add(-1)
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	}))

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "child-overrides"},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"p1": {Provider: "barrier-worker"},
					"p2": {Provider: "barrier-worker"},
				},
			},
		},
	}
	solProv.Configure(WithLoader(&mockLoader{
		solutions: map[string]*solution.Solution{"child-overrides": child},
	}))

	// Parent explicitly runs with MaxConcurrency=0 (unlimited) and no OutputDirectory
	// on context (equivalent to --max-action-concurrency=0 --output-dir=""), while
	// application config carries MaxConcurrency=1 and OutputDir=unwantedOutDir.
	parentExec := action.NewExecutor(
		action.WithRegistry(reg),
		action.WithMaxConcurrency(0),
		action.WithDefaultTimeout(2*time.Second),
	)

	ctx := config.WithConfig(testContext(), appCfg)
	res, err := parentExec.Execute(ctx, &action.Workflow{
		Actions: map[string]*action.Action{
			"run-child": {
				Provider: ProviderName,
				Inputs: map[string]*spec.ValueRef{
					"source": {Literal: "child-overrides"},
				},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
	assert.GreaterOrEqual(t, maxInFlight.Load(), int32(2), "child workflow must inherit parent MaxConcurrency=0 (unlimited) instead of reverting to config MaxConcurrency=1")

	_, statErr := os.Stat(unwantedOutDir)
	assert.True(t, os.IsNotExist(statErr), "child workflow must not recreate application-config OutputDir when parent opted out")
}

func TestExecute_RedactsCredentialsFromProgressAndWarnings(t *testing.T) {
	const (
		trackedMarker = "tracked-fixture-value"
		rawURL        = "https://example.com/deploy.yaml?ref=" + trackedMarker
		redactedURL   = "https://example.com/deploy.yaml?ref=" + redact.RedactedPlaceholder
	)

	t.Run("redacts tracked URL credential from progress, warnings, WhatIf, and errors while preserving errors.Is", func(t *testing.T) {
		var observedAncestors []string
		sentinelErr := errors.New("step failed with " + trackedMarker)

		reg := provider.NewRegistry()
		solProv := New(WithRegistry(reg))
		require.NoError(t, reg.Register(solProv))
		require.NoError(t, reg.Register(&callbackProvider{
			name: "step-runner",
			fn: func(ctx context.Context, input any) (*provider.Output, error) {
				observedAncestors = AncestorStackFromContext(ctx)
				m, _ := input.(map[string]any)
				if m["fail"] == true {
					return nil, sentinelErr
				}
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			},
		}))

		okChild := &solution.Solution{
			Metadata: solution.Metadata{Name: "url-child"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"deploy": {Provider: "step-runner"},
					},
				},
			},
		}
		failChildURL := "https://example.com/fail.yaml?ref=" + trackedMarker
		failChild := &solution.Solution{
			Metadata: solution.Metadata{Name: "url-fail-child"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"deploy": {
							Provider: "step-runner",
							Inputs:   map[string]*spec.ValueRef{"fail": {Literal: true}},
						},
					},
				},
			},
		}
		solProv.Configure(WithLoader(&mockLoader{
			solutions: map[string]*solution.Solution{
				rawURL:       okChild,
				failChildURL: failChild,
			},
		}))

		tracker := redact.NewTracker()
		tracker.Track(trackedMarker)
		ctx := redact.WithTracker(testContext(), tracker)

		// WhatIf scrubs tracked credentials.
		whatIfMsg, err := solProv.Descriptor().WhatIf(ctx, &Input{Source: rawURL})
		require.NoError(t, err)
		assert.Contains(t, whatIfMsg, redactedURL)
		assert.NotContains(t, whatIfMsg, trackedMarker)

		rec := &progressLog{}
		parentExec := action.NewExecutor(
			action.WithRegistry(reg),
			action.WithProgressCallback(rec),
		)

		res, err := parentExec.Execute(ctx, &action.Workflow{
			Actions: map[string]*action.Action{
				"invoke-url": {
					Provider: ProviderName,
					Inputs: map[string]*spec.ValueRef{
						"source": {Literal: rawURL},
					},
				},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, action.ExecutionSucceeded, res.FinalStatus)
		assert.Equal(t, []string{rawURL}, observedAncestors)
		assert.Contains(t, rec.events, "start:[solution:"+redactedURL+"] deploy")
		for _, ev := range rec.events {
			assert.NotContains(t, ev, trackedMarker)
		}

		// propagateErrors: false scrubs Warnings as well.
		actionCtx := provider.WithExecutionMode(ctx, provider.CapabilityAction)
		out, err := solProv.Execute(actionCtx, &Input{
			Source:          failChildURL,
			PropagateErrors: boolPtr(false),
		})
		require.NoError(t, err)
		require.NotEmpty(t, out.Warnings)
		for _, w := range out.Warnings {
			assert.NotContains(t, w, trackedMarker)
		}

		// propagateErrors: true scrubs error text while preserving errors.Is.
		_, err = solProv.Execute(actionCtx, &Input{
			Source:          failChildURL,
			PropagateErrors: boolPtr(true),
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), trackedMarker)
		assert.ErrorIs(t, err, sentinelErr)
	})

	t.Run("redacts short marked source inputs across load and cycle errors", func(t *testing.T) {
		const shortSourceFixture = "s9"
		loadSentinel := fmt.Errorf("missing catalog entry %q", shortSourceFixture)

		solProv := New(
			WithLoader(&mockLoader{err: loadSentinel}),
			WithRegistry(provider.NewRegistry()),
		)

		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
		ctx = provider.WithSecretProperties(ctx, map[string]bool{"source": true})

		// propagateErrors: false load failure scrubs both _loader message and Warnings.
		out, err := solProv.Execute(ctx, &Input{
			Source:          shortSourceFixture,
			PropagateErrors: boolPtr(false),
		})
		require.NoError(t, err)
		data := out.Data.(map[string]any)
		errs := data["errors"].([]map[string]any)
		require.Len(t, errs, 1)
		assert.NotContains(t, errs[0]["message"], shortSourceFixture)
		require.NotEmpty(t, out.Warnings)
		assert.NotContains(t, out.Warnings[0], shortSourceFixture)

		// propagateErrors: true load failure scrubs error text while preserving errors.Is.
		_, err = solProv.Execute(ctx, &Input{
			Source:          shortSourceFixture,
			PropagateErrors: boolPtr(true),
		})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), shortSourceFixture)
		assert.ErrorIs(t, err, loadSentinel)

		// Circular reference error also scrubs the short source value.
		cycleCtx, pushErr := PushAncestor(ctx, shortSourceFixture)
		require.NoError(t, pushErr)
		_, err = solProv.Execute(cycleCtx, &Input{Source: shortSourceFixture})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular reference detected")
		assert.NotContains(t, err.Error(), shortSourceFixture)
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

func TestIsLocalFileSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{"absolute path", "/home/user/solution.yaml", true},
		{"relative dot-slash", "./child.yaml", true},
		{"relative parent", "../sibling/child.yaml", true},
		{"yaml extension", "child.yaml", true},
		{"yml extension", "child.yml", true},
		{"json extension", "child.json", true},
		{"subpath without extension", "sub/child", true},
		{"https url ending in yaml", "https://example.com/sol.yaml", false},
		{"http url ending in json", "http://example.com/sol.json", false},
		{"oci remote ref", "ghcr.io/example-org/deploy@2.0.0", false},
		{"oci scheme ref", "oci://ghcr.io/example-org/deploy:latest", false},
		{"catalog reference", "deploy-to-k8s@2.0.0", false},
		{"bare name", "my-solution", false},
		{"catalog with version", "infra-config@1.0.0", false},
		{"stdin", "-", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalFileSource(tt.source))
		})
	}
}

func TestClose_NoClients(t *testing.T) {
	p := New()
	// Must not panic.
	p.Close()
}

func TestClose_KillsClients(t *testing.T) {
	// Close should clear childClients even when they're nil (simulating no actual processes).
	p := New()
	p.mu.Lock()
	p.childClients = nil
	p.mu.Unlock()
	p.Close()
	assert.Nil(t, p.childClients)
}

func TestAutoResolveChildProviders_NilOfficialProviders(t *testing.T) {
	reg := provider.NewRegistry()
	p := New(WithRegistry(reg))
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "exec"}}}},
			},
		},
	}
	// Should return immediately — no panic, no error, nothing fetched.
	err := p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.NoError(t, err)
	assert.Empty(t, p.childClients)
}

func TestAutoResolveChildProviders_NilPluginFetcher(t *testing.T) {
	officialReg := official.NewRegistry()
	reg := provider.NewRegistry()
	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
		// No WithPluginFetcher — simulates missing fetcher.
	)
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "exec"}}}},
			},
		},
	}
	err := p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.NoError(t, err)
	assert.Empty(t, p.childClients)
}

func TestAutoResolveChildProviders_AllRegistered(t *testing.T) {
	// When all providers are already registered, nothing should be resolved.
	reg := provider.NewRegistry()
	// Register a dummy "exec" provider.
	_ = reg.Register(&mockProvider{name: "exec"})

	officialReg := official.NewRegistry()
	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
	)
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "exec"}}}},
			},
		},
	}
	err := p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.NoError(t, err)
	assert.Empty(t, p.childClients)
}

func TestAutoResolveChildProviders_NoMissing(t *testing.T) {
	// When solution references no providers at all.
	reg := provider.NewRegistry()
	officialReg := official.NewRegistry()
	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
		WithPluginFetcher(&plugin.Fetcher{}),
	)
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
			},
		},
	}
	// "cel" is not in the official registry so nothing to resolve.
	err := p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.NoError(t, err)
	assert.Empty(t, p.childClients)
}

func TestAutoResolveChildProviders_DeduplicatesProviders(t *testing.T) {
	// When the same provider appears multiple times, it should only appear once.
	reg := provider.NewRegistry()
	_ = reg.Register(&mockProvider{name: "exec"}) // already registered

	officialReg := official.NewRegistry()
	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
	)
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "exec"}}}},
				"r2": {Transform: &resolver.TransformPhase{With: []resolver.ProviderTransform{{Provider: "exec"}}}},
			},
		},
	}
	err := p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.NoError(t, err)
	assert.Empty(t, p.childClients)
}

func TestAutoResolveChildProviders_FetchFails(t *testing.T) {
	// When a provider IS in the official registry but the fetcher returns an
	// error (no catalog has the binary), the function should surface the error.
	reg := provider.NewRegistry()
	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "fake-provider", CatalogRef: "fake-provider", DefaultVersion: "latest"},
	})

	// Create a fetcher with an empty local catalog — resolution will fail.
	local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	catalogFetcher, err := catalog.NewPluginFetcher(logr.Discard(), local)
	require.NoError(t, err)

	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
		WithPluginFetcher(plugin.NewFetcher(plugin.FetcherConfig{
			CatalogFetcher: catalogFetcher,
			Cache:          plugin.NewCache(t.TempDir()),
			Platform:       "linux/amd64",
			Logger:         logr.Discard(),
		})),
	)
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"test": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "fake-provider"}}}},
			},
		},
	}
	err = p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "auto-fetching child solution providers")
}

func TestAutoResolveChildProviders_WorkflowActions(t *testing.T) {
	// Providers in workflow.actions and workflow.finally are also resolved.
	reg := provider.NewRegistry()
	_ = reg.Register(&mockProvider{name: "exec"})

	officialReg := official.NewRegistryFrom([]official.Provider{
		{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
		{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
	})

	local, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	catalogFetcher, err := catalog.NewPluginFetcher(logr.Discard(), local)
	require.NoError(t, err)

	p := New(
		WithRegistry(reg),
		WithOfficialProviders(officialReg),
		WithPluginFetcher(plugin.NewFetcher(plugin.FetcherConfig{
			CatalogFetcher: catalogFetcher,
			Cache:          plugin.NewCache(t.TempDir()),
			Platform:       "linux/amd64",
			Logger:         logr.Discard(),
		})),
	)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"a1": {Provider: "exec"}, // already registered
					"a2": {Provider: "git"},  // missing, official, will fail fetch
				},
				Finally: map[string]*action.Action{
					"cleanup": {Provider: "exec"}, // already registered
				},
			},
		},
	}

	// "git" is official but not registered and not in any catalog — errors on fetch.
	err = p.autoResolveChildProviders(context.Background(), sol, reg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "auto-fetching child solution providers")
}

func TestAutoResolveChildProviders_Close(t *testing.T) {
	// Verify Close() can be called on a provider with no child clients.
	p := New(WithRegistry(provider.NewRegistry()))
	p.Close() // should not panic
	assert.Empty(t, p.childClients)
}

// --- Child Path Resolution & Bundle Extraction Tests ---

type mockBundleLoader struct {
	solutions map[string]*solution.Solution
	bundles   map[string][]byte
	err       error
	gotPaths  []string
}

func (m *mockBundleLoader) Get(ctx context.Context, path string) (*solution.Solution, error) {
	sol, _, err := m.GetWithBundle(ctx, path)
	return sol, err
}

func (m *mockBundleLoader) GetWithBundle(_ context.Context, path string) (*solution.Solution, []byte, error) {
	m.gotPaths = append(m.gotPaths, path)
	if m.err != nil {
		return nil, nil, m.err
	}
	sol, ok := m.solutions[path]
	if !ok {
		return nil, nil, fmt.Errorf("solution %q not found", path)
	}
	return sol, m.bundles[path], nil
}

func buildTestBundleTar(t testing.TB, entries map[string]string) []byte {
	t.Helper()
	root := t.TempDir()
	fileEntries := make([]bundler.FileEntry, 0, len(entries))
	for rel, content := range entries {
		full := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
		fileEntries = append(fileEntries, bundler.FileEntry{RelPath: rel})
	}
	snap, err := bundler.ReadFileSnapshot(root, fileEntries)
	require.NoError(t, err)
	tarData, _, err := bundler.CreateBundleTar(snap, nil)
	require.NoError(t, err)
	return tarData
}

func TestExecute_ChildPathResolution_TwoPhaseAndThreeLevel(t *testing.T) {
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "sub")
	grandDir := filepath.Join(childDir, "grand")
	workDir := t.TempDir()
	outDir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(childDir, "templates"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(grandDir, "templates"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(childDir, "templates", "child.tmpl"), []byte("from-child-dir"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(grandDir, "templates", "grand.tmpl"), []byte("from-grand-dir"), 0o600))

	// Pre-create an existing file in outDir so ConflictStrategy=overwrite + Backup=true are exercised.
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "written.txt"), []byte("previous"), 0o600))

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(fileprovider.NewFileProvider()))
	solProv := New(WithRegistry(reg))
	require.NoError(t, reg.Register(solProv))

	grandPath := filepath.Join(grandDir, "grandchild.yaml")
	grandSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "grandchild"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"grand_tmpl": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation": {Literal: "read"},
								"path":      {Literal: "templates/grand.tmpl"},
							},
						}},
					},
				},
			},
		},
	}
	grandSol.SetPath(grandPath)

	childPath := filepath.Join(childDir, "child.yaml")
	childTmplRef := "child_tmpl"
	childSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "child"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"child_tmpl": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation": {Literal: "read"},
								"path":      {Literal: "templates/child.tmpl"},
							},
						}},
					},
				},
				"grand": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: ProviderName,
							Inputs: map[string]*spec.ValueRef{
								"source": {Literal: "./grand/grandchild.yaml"},
							},
						}},
					},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"write_out": {
						Provider: "file",
						Inputs: map[string]*spec.ValueRef{
							"operation": {Literal: "write"},
							"path":      {Literal: "written.txt"},
							"content":   {Resolver: &childTmplRef},
						},
					},
				},
			},
		},
	}
	childSol.SetPath(childPath)

	solProv.Configure(WithLoader(&mockLoader{
		solutions: map[string]*solution.Solution{
			childPath: childSol,
			grandPath: grandSol,
		},
	}))

	// Simulate parent action context: WorkingDirectory AND SolutionDirectory are both set!
	// Without masking parentWorkingDirectoryKey in isolatedContext, child resolver reads
	// would hit workDir instead of childDir.
	ctx := testContext()
	ctx = provider.WithWorkingDirectory(ctx, workDir)
	ctx = provider.WithSolutionDirectory(ctx, parentDir)
	ctx = provider.WithOutputDirectory(ctx, outDir)
	ctx = provider.WithConflictStrategy(ctx, "overwrite")
	ctx = provider.WithBackup(ctx, true)
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)

	// Child write_out references child_tmpl.content via CEL expression.
	contentExpr := celexp.Expression("_.child_tmpl.content + ':' + _.grand.resolvers.grand_tmpl.content")
	childSol.Spec.Workflow.Actions["write_out"].Inputs["content"] = &spec.ValueRef{Expr: &contentExpr}

	out, err := solProv.Execute(ctx, &Input{Source: "./sub/child.yaml"})
	require.NoError(t, err)

	data := out.Data.(map[string]any)
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, true, data["success"])

	written, err := os.ReadFile(filepath.Join(outDir, "written.txt"))
	require.NoError(t, err)
	assert.Equal(t, "from-child-dir:from-grand-dir", string(written))
	assert.FileExists(t, filepath.Join(outDir, "written.txt.bak"), "parent Backup setting must propagate to child action writes")
	assert.NoFileExists(t, filepath.Join(workDir, "written.txt"))
	assert.NoFileExists(t, filepath.Join(childDir, "written.txt"))
}

func TestExecute_URLSource_NotCorruptedByParentSolutionDirectory(t *testing.T) {
	var childObservedSolDir string
	var childHasSolDir bool

	reg := provider.NewRegistry()
	require.NoError(t, reg.Register(&callbackProvider{
		name: "probe",
		fn: func(ctx context.Context, _ any) (*provider.Output, error) {
			childObservedSolDir, childHasSolDir = provider.SolutionDirectoryFromContext(ctx)
			return &provider.Output{Data: "ok"}, nil
		},
	}))

	urlSrc := "https://example.com/reusable/sol.yaml"
	urlSol := &solution.Solution{
		Metadata: solution.Metadata{Name: "url-child"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"check": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "probe"}},
					},
				},
			},
		},
	}

	loader := &mockBundleLoader{
		solutions: map[string]*solution.Solution{urlSrc: urlSol},
	}
	p := New(WithLoader(loader), WithRegistry(reg))

	ctx := testContext()
	ctx = provider.WithSolutionDirectory(ctx, "/parent/solution")
	ctx = provider.WithWorkingDirectory(ctx, "/parent/work")
	ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)

	out, err := p.Execute(ctx, &Input{Source: urlSrc})
	require.NoError(t, err)
	assert.Equal(t, []string{urlSrc}, loader.gotPaths, "URL source must not be joined with parent SolutionDirectory")
	assert.False(t, childHasSolDir, "URL child must not inherit parent SolutionDirectory")
	assert.Empty(t, childObservedSolDir)
	assert.Equal(t, "success", out.Data.(map[string]any)["status"])
}

func TestExecute_BundleLoader_ExtractionCleanupAndDryRun(t *testing.T) {
	t.Run("extracts bundle with custom binary name, resolves child files, writes to parent cwd, and removes temp dir", func(t *testing.T) {
		bundleData := buildTestBundleTar(t, map[string]string{
			"templates/msg.tmpl": "bundled-hello",
		})
		parentWorkDir := t.TempDir()

		var capturedResolverSolDir string
		var capturedResolverWorkDir string
		var capturedResolverHasWork bool
		var capturedActionWorkDir string
		var capturedActionSolDir string

		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(fileprovider.NewFileProvider()))
		require.NoError(t, reg.Register(&callbackProvider{
			name: "ctx-inspector",
			fn: func(ctx context.Context, _ any) (*provider.Output, error) {
				mode, _ := provider.ExecutionModeFromContext(ctx)
				if mode == provider.CapabilityFrom {
					capturedResolverSolDir, _ = provider.SolutionDirectoryFromContext(ctx)
					capturedResolverWorkDir, capturedResolverHasWork = provider.WorkingDirectoryFromContext(ctx)
					return &provider.Output{Data: "inspected"}, nil
				}
				capturedActionWorkDir, _ = provider.WorkingDirectoryFromContext(ctx)
				capturedActionSolDir, _ = provider.SolutionDirectoryFromContext(ctx)
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			},
		}))

		contentExpr := celexp.Expression("_.tmpl.content")
		childSol := &solution.Solution{
			Metadata: solution.Metadata{Name: "bundled-child"},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"inspect": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{Provider: "ctx-inspector"}},
						},
					},
					"tmpl": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"operation": {Literal: "read"},
									"path":      {Literal: "templates/msg.tmpl"},
								},
							}},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"inspect_action": {Provider: "ctx-inspector"},
						"write_file": {
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation": {Literal: "write"},
								"path":      {Literal: "out.txt"},
								"content":   {Expr: &contentExpr},
							},
						},
					},
				},
			},
		}
		childSol.SetPath("catalog:reusable@1.0.0")

		loader := &mockBundleLoader{
			solutions: map[string]*solution.Solution{"reusable@1.0.0": childSol},
			bundles:   map[string][]byte{"reusable@1.0.0": bundleData},
		}
		p := New(WithLoader(loader), WithRegistry(reg))

		// Verify embedder custom BinaryName is honored in temp directory prefix.
		ctx := settings.IntoContext(testContext(), &settings.Run{BinaryName: "mycli"})
		ctx = provider.WithWorkingDirectory(ctx, parentWorkDir)
		ctx = provider.WithSolutionDirectory(ctx, "/parent/irrelevant")
		ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)

		out, err := p.Execute(ctx, &Input{Source: "reusable@1.0.0"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])

		// Resolver saw child bundle dir with WorkingDirectory masked.
		require.NotEmpty(t, capturedResolverSolDir)
		assert.Contains(t, filepath.Base(capturedResolverSolDir), "mycli-bundle-")
		assert.False(t, capturedResolverHasWork)
		assert.Empty(t, capturedResolverWorkDir)

		// Action saw restored parentWorkDir alongside child SolutionDirectory.
		assert.Equal(t, parentWorkDir, capturedActionWorkDir)
		assert.Equal(t, capturedResolverSolDir, capturedActionSolDir)

		// Action write landed in parentWorkDir, and ephemeral bundle dir was cleaned up.
		written, err := os.ReadFile(filepath.Join(parentWorkDir, "out.txt"))
		require.NoError(t, err)
		assert.Equal(t, "bundled-hello", string(written))
		assert.NoDirExists(t, capturedResolverSolDir, "bundle temp dir must be cleaned up when Execute returns")
	})

	t.Run("cleans up bundle temp dir when child fails or context cancels", func(t *testing.T) {
		bundleData := buildTestBundleTar(t, map[string]string{"data.txt": "val"})
		var observedBundleDir string

		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&callbackProvider{
			name: "fail-after-capture",
			fn: func(ctx context.Context, _ any) (*provider.Output, error) {
				observedBundleDir, _ = provider.SolutionDirectoryFromContext(ctx)
				return nil, errors.New("boom")
			},
		}))

		childSol := &solution.Solution{
			Metadata: solution.Metadata{Name: "failing-bundled-child"},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{Provider: "fail-after-capture"}},
						},
					},
				},
			},
		}

		p := New(
			WithLoader(&mockBundleLoader{
				solutions: map[string]*solution.Solution{"fail-pkg@1.0.0": childSol},
				bundles:   map[string][]byte{"fail-pkg@1.0.0": bundleData},
			}),
			WithRegistry(reg),
		)

		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)
		_, err := p.Execute(ctx, &Input{Source: "fail-pkg@1.0.0"})
		require.Error(t, err)
		require.NotEmpty(t, observedBundleDir)
		assert.NoDirExists(t, observedBundleDir, "bundle temp dir must be removed on child error")
	})

	t.Run("corrupt bundle tar honors propagateErrors and cleans up temp dir", func(t *testing.T) {
		childSol := newTestSolution(nil, nil)
		p := New(
			WithLoader(&mockBundleLoader{
				solutions: map[string]*solution.Solution{"bad-bundle@1.0.0": childSol},
				bundles:   map[string][]byte{"bad-bundle@1.0.0": []byte("corrupt-tar-bytes")},
			}),
			WithRegistry(provider.NewRegistry()),
		)

		ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)

		// propagateErrors: true returns error.
		_, err := p.Execute(ctx, &Input{Source: "bad-bundle@1.0.0"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "extracting bundle")

		// propagateErrors: false returns degraded envelope with _loader error.
		out, err := p.Execute(ctx, &Input{
			Source:          "bad-bundle@1.0.0",
			PropagateErrors: boolPtr(false),
		})
		require.NoError(t, err)
		data := out.Data.(map[string]any)
		assert.Equal(t, "failed", data["status"])
		errs := data["errors"].([]map[string]any)
		require.Len(t, errs, 1)
		assert.Equal(t, "_loader", errs[0]["resolver"])
		require.NotEmpty(t, out.Warnings)
	})

	t.Run("dry-run returns before extracting bundle archive", func(t *testing.T) {
		// Pass deliberately invalid tar bytes: if DryRun attempted bundle extraction,
		// Execute would fail with a tar parse error instead of succeeding.
		childSol := newTestSolution(nil, nil)
		p := New(
			WithLoader(&mockBundleLoader{
				solutions: map[string]*solution.Solution{"dry-pkg@1.0.0": childSol},
				bundles:   map[string][]byte{"dry-pkg@1.0.0": []byte("invalid-tar-must-not-extract-in-dry-run")},
			}),
			WithRegistry(provider.NewRegistry()),
		)

		ctx := provider.WithDryRun(testContext(), true)
		ctx = provider.WithExecutionMode(ctx, provider.CapabilityAction)

		out, err := p.Execute(ctx, &Input{Source: "dry-pkg@1.0.0"})
		require.NoError(t, err)
		data := out.Data.(map[string]any)
		assert.Equal(t, true, data["dryRun"])
		assert.Equal(t, "success", data["status"])
	})

	t.Run("merges child bundle plugin defaults across nil inputs, finally actions, and concurrent runs", func(t *testing.T) {
		var mu sync.Mutex
		var resolverSeen []map[string]any
		var finallySeen []map[string]any

		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&callbackProvider{
			name: "custom-prov",
			fn: func(ctx context.Context, input any) (*provider.Output, error) {
				m, _ := input.(map[string]any)
				copied := make(map[string]any, len(m))
				for k, v := range m {
					copied[k] = v
				}
				mode, _ := provider.ExecutionModeFromContext(ctx)
				mu.Lock()
				if mode == provider.CapabilityFrom {
					resolverSeen = append(resolverSeen, copied)
				} else {
					finallySeen = append(finallySeen, copied)
				}
				mu.Unlock()
				if mode == provider.CapabilityAction {
					return &provider.Output{Data: map[string]any{"success": true}}, nil
				}
				return &provider.Output{Data: "ok"}, nil
			},
		}))

		childSol := &solution.Solution{
			Metadata: solution.Metadata{Name: "plugin-defaults-child"},
			Bundle: solution.Bundle{
				Plugins: []solution.PluginDependency{{
					Name:    "custom-prov",
					Kind:    solution.PluginKindProvider,
					Version: "^1.0.0",
					Defaults: map[string]*spec.ValueRef{
						"region": {Literal: "us-west-2"},
						"tier":   {Literal: "default-tier"},
					},
				}},
			},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"with_override": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "custom-prov",
								Inputs: map[string]*spec.ValueRef{
									"tier": {Literal: "inline-override"},
								},
							}},
						},
					},
					"nil_inputs": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "custom-prov",
								// Inputs omitted (nil) -- must still receive plugin defaults.
							}},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"act": {Provider: "custom-prov"},
					},
					Finally: map[string]*action.Action{
						"cleanup": {Provider: "custom-prov"},
					},
				},
			},
		}

		p := New(
			WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"defaults@1.0.0": childSol}}),
			WithRegistry(reg),
		)

		// Concurrent executions sharing the same *solution.Solution must be race-free.
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx := provider.WithExecutionMode(testContext(), provider.CapabilityAction)
				out, err := p.Execute(ctx, &Input{Source: "defaults@1.0.0"})
				assert.NoError(t, err)
				if out != nil {
					assert.Equal(t, "success", out.Data.(map[string]any)["status"])
				}
			}()
		}
		wg.Wait()

		assert.Len(t, resolverSeen, 8)
		for _, seen := range resolverSeen {
			assert.Equal(t, "us-west-2", seen["region"])
			assert.Contains(t, []any{"default-tier", "inline-override"}, seen["tier"])
		}
		assert.Len(t, finallySeen, 8)
		for _, seen := range finallySeen {
			assert.Equal(t, "us-west-2", seen["region"])
			assert.Equal(t, "default-tier", seen["tier"])
		}

		// Nil action entry in a synthetic solution must not panic normalizeSolutionNames.
		nilActionSol := &solution.Solution{
			Bundle: childSol.Bundle,
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{"nil_entry": nil},
					Finally: map[string]*action.Action{"nil_finally": nil},
				},
			},
		}
		p.normalizeSolutionNames(nilActionSol)
	})

	t.Run("relative sol.GetPath resolves against parent SolutionDirectory before WorkingDirectory", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "sub")
		otherWorkDir := t.TempDir()

		var gotSolDir string
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(&callbackProvider{
			name: "dir-probe",
			fn: func(ctx context.Context, _ any) (*provider.Output, error) {
				gotSolDir, _ = provider.SolutionDirectoryFromContext(ctx)
				return &provider.Output{Data: "ok"}, nil
			},
		}))

		childSol := &solution.Solution{
			Metadata: solution.Metadata{Name: "rel-path-child"},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"r": {
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{Provider: "dir-probe"}},
						},
					},
				},
			},
		}
		childSol.SetPath("./sub/child.yaml")

		resolvedPath := filepath.Join(childDir, "child.yaml")
		p := New(
			WithLoader(&mockLoader{solutions: map[string]*solution.Solution{resolvedPath: childSol}}),
			WithRegistry(reg),
		)

		ctx := testContext()
		ctx = provider.WithWorkingDirectory(ctx, otherWorkDir)
		ctx = provider.WithSolutionDirectory(ctx, parentDir)
		ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)

		out, err := p.Execute(ctx, &Input{Source: "./sub/child.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])
		assert.Equal(t, childDir, gotSolDir)
	})
}

func TestExecute_ChildWorkflowActionReads_LocalAndBundled_WithAndWithoutOutputDir(t *testing.T) {
	buildActionReadChild := func(name string) *solution.Solution {
		writeExpr := celexp.Expression("__actions['read_tmpl'].results.content + ':exists=' + string(__actions['check_tmpl'].results.exists)")
		return &solution.Solution{
			Metadata: solution.Metadata{Name: name},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"check_tmpl": {
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation":  {Literal: "exists"},
								"path":       {Literal: "templates/action.tmpl"},
								"relativeTo": {Literal: "solution"},
							},
						},
						"read_tmpl": {
							Provider:  "file",
							DependsOn: []string{"check_tmpl"},
							Inputs: map[string]*spec.ValueRef{
								"operation":  {Literal: "read"},
								"path":       {Literal: "templates/action.tmpl"},
								"relativeTo": {Literal: "solution"},
							},
						},
						"write_out": {
							Provider:  "file",
							DependsOn: []string{"read_tmpl"},
							Inputs: map[string]*spec.ValueRef{
								"operation": {Literal: "write"},
								"path":      {Literal: "action-read-result.txt"},
								"content":   {Expr: &writeExpr},
							},
						},
					},
				},
			},
		}
	}

	t.Run("local child workflow reads relativeTo solution with and without output-dir", func(t *testing.T) {
		parentDir := t.TempDir()
		childDir := filepath.Join(parentDir, "sub")
		require.NoError(t, os.MkdirAll(filepath.Join(childDir, "templates"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(childDir, "templates", "action.tmpl"), []byte("local-action-tmpl"), 0o600))

		childPath := filepath.Join(childDir, "child.yaml")
		childSol := buildActionReadChild("local-action-read")
		childSol.SetPath(childPath)

		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(fileprovider.NewFileProvider()))
		p := New(
			WithLoader(&mockLoader{solutions: map[string]*solution.Solution{childPath: childSol}}),
			WithRegistry(reg),
		)

		// Without --output-dir: writes land in workDir, read comes from childDir.
		workDir := t.TempDir()
		ctxNoOut := testContext()
		ctxNoOut = provider.WithWorkingDirectory(ctxNoOut, workDir)
		ctxNoOut = provider.WithSolutionDirectory(ctxNoOut, parentDir)
		ctxNoOut = provider.WithExecutionMode(ctxNoOut, provider.CapabilityAction)

		out, err := p.Execute(ctxNoOut, &Input{Source: "./sub/child.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])
		gotNoOut, err := os.ReadFile(filepath.Join(workDir, "action-read-result.txt"))
		require.NoError(t, err)
		assert.Equal(t, "local-action-tmpl:exists=true", string(gotNoOut))

		// With --output-dir: writes land in outDir, read still succeeds from childDir.
		outDir := t.TempDir()
		ctxWithOut := provider.WithOutputDirectory(ctxNoOut, outDir)
		out, err = p.Execute(ctxWithOut, &Input{Source: "./sub/child.yaml"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])
		gotWithOut, err := os.ReadFile(filepath.Join(outDir, "action-read-result.txt"))
		require.NoError(t, err)
		assert.Equal(t, "local-action-tmpl:exists=true", string(gotWithOut))
	})

	t.Run("bundled catalog child workflow reads relativeTo solution with and without output-dir and blocks escape", func(t *testing.T) {
		bundleData := buildTestBundleTar(t, map[string]string{
			"templates/action.tmpl": "bundled-action-tmpl",
		})

		childSol := buildActionReadChild("bundled-action-read")
		childSol.SetPath("catalog:action-pkg@1.0.0")

		escapeSol := &solution.Solution{
			Metadata: solution.Metadata{Name: "bundled-escape-read"},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"escape_read": {
							Provider: "file",
							Inputs: map[string]*spec.ValueRef{
								"operation":  {Literal: "read"},
								"path":       {Literal: "../outside.txt"},
								"relativeTo": {Literal: "solution"},
							},
						},
					},
				},
			},
		}
		escapeSol.SetPath("catalog:escape-pkg@1.0.0")

		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(fileprovider.NewFileProvider()))
		p := New(
			WithLoader(&mockBundleLoader{
				solutions: map[string]*solution.Solution{
					"action-pkg@1.0.0": childSol,
					"escape-pkg@1.0.0": escapeSol,
				},
				bundles: map[string][]byte{
					"action-pkg@1.0.0": bundleData,
					"escape-pkg@1.0.0": bundleData,
				},
			}),
			WithRegistry(reg),
		)

		workDir := t.TempDir()
		outDir := t.TempDir()
		baseCtx := testContext()
		baseCtx = provider.WithWorkingDirectory(baseCtx, workDir)
		baseCtx = provider.WithExecutionMode(baseCtx, provider.CapabilityAction)

		// Without --output-dir.
		out, err := p.Execute(baseCtx, &Input{Source: "action-pkg@1.0.0"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])
		gotNoOut, err := os.ReadFile(filepath.Join(workDir, "action-read-result.txt"))
		require.NoError(t, err)
		assert.Equal(t, "bundled-action-tmpl:exists=true", string(gotNoOut))

		// With --output-dir.
		out, err = p.Execute(provider.WithOutputDirectory(baseCtx, outDir), &Input{Source: "action-pkg@1.0.0"})
		require.NoError(t, err)
		assert.Equal(t, "success", out.Data.(map[string]any)["status"])
		gotWithOut, err := os.ReadFile(filepath.Join(outDir, "action-read-result.txt"))
		require.NoError(t, err)
		assert.Equal(t, "bundled-action-tmpl:exists=true", string(gotWithOut))

		// Traversal out of extracted bundle dir fails containment.
		_, err = p.Execute(provider.WithOutputDirectory(baseCtx, outDir), &Input{Source: "escape-pkg@1.0.0"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "outside solution directory")
	})
}
