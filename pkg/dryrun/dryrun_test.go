// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package dryrun

import (
	"context"
	"fmt"
	"testing"

	semver "github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// helpers

func minimalSolution(name string) *solution.Solution {
	return &solution.Solution{
		Metadata: solution.Metadata{Name: name},
		Spec:     solution.Spec{},
	}
}

type mockProvider struct {
	desc *provider.Descriptor
}

func (m *mockProvider) Descriptor() *provider.Descriptor { return m.desc }
func (m *mockProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return nil, nil
}

func newMockProvider(name string) *mockProvider {
	return &mockProvider{
		desc: &provider.Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Description:  fmt.Sprintf("Mock %s provider for testing", name),
			Capabilities: []provider.Capability{provider.CapabilityAction},
			Schema: &jsonschema.Schema{
				Type: "object",
				Properties: map[string]*jsonschema.Schema{
					"input": {Type: "string"},
				},
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityAction: {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"success": {Type: "boolean"},
					},
				},
			},
		},
	}
}

func newMockProviderWithWhatIf(name string, whatIf func(context.Context, any) (string, error)) *mockProvider {
	mp := newMockProvider(name)
	mp.desc.WhatIf = whatIf
	return mp
}

// basic fields

func TestGenerate_BasicFields(t *testing.T) {
	sol := minimalSolution("test-app")
	sol.Metadata.Version = semver.MustParse("2.5.0")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.True(t, report.DryRun)
	assert.Equal(t, "test-app", report.Solution)
	assert.Equal(t, "2.5.0", report.Version)
	assert.False(t, report.HasWorkflow)
}

func TestGenerate_NilVersion(t *testing.T) {
	sol := minimalSolution("app")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	assert.Equal(t, "", report.Version)
}

// action plan

func TestGenerate_ActionPlanSimple(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell", Description: "Deploy app"},
		},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.True(t, report.HasWorkflow)
	assert.Equal(t, 1, report.TotalActions)
	assert.True(t, report.TotalPhases >= 1)
	require.Len(t, report.ActionPlan, 1)

	act := report.ActionPlan[0]
	assert.Equal(t, "deploy", act.Name)
	assert.Equal(t, "shell", act.Provider)
	assert.Equal(t, "Deploy app", act.Description)
	assert.Equal(t, "actions", act.Section)
	assert.Equal(t, 1, act.Phase)
}

func TestGenerate_ActionPlanSorted(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"z-action": {Provider: "shell"},
			"a-action": {Provider: "shell"},
			"m-action": {Provider: "shell"},
		},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 3)

	assert.Equal(t, "a-action", report.ActionPlan[0].Name)
	assert.Equal(t, "m-action", report.ActionPlan[1].Name)
	assert.Equal(t, "z-action", report.ActionPlan[2].Name)
}

func TestGenerate_NoWorkflow(t *testing.T) {
	sol := minimalSolution("app")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.False(t, report.HasWorkflow)
	assert.Empty(t, report.ActionPlan)
	assert.Zero(t, report.TotalActions)
}

// whatif messages

func TestGenerate_WhatIfFromProvider(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	require.NoError(t, reg.Register(newMockProvider("shell")))

	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Equal(t, "Would execute shell provider", report.ActionPlan[0].WhatIf)
}

func TestGenerate_WhatIfUsesWhatIfFunc(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "shell",
				Inputs:   map[string]*spec.ValueRef{"command": {Literal: "./deploy.sh"}},
			},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	require.NoError(t, reg.Register(newMockProviderWithWhatIf("shell",
		func(_ context.Context, input any) (string, error) {
			inputs, _ := input.(map[string]any)
			cmd, _ := inputs["command"].(string)
			return fmt.Sprintf("Would execute %s", cmd), nil
		},
	)))

	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Equal(t, "Would execute ./deploy.sh", report.ActionPlan[0].WhatIf)
}

func TestGenerate_WhatIfFallbackNoRegistry(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Equal(t, "Would execute shell provider", report.ActionPlan[0].WhatIf)
}

// resolver references in action inputs

func TestGenerate_ResolverRefInActionInputs(t *testing.T) {
	rslvrName := "environment"
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "shell",
				Inputs:   map[string]*spec.ValueRef{"target": {Resolver: &rslvrName}},
			},
		},
	}

	resolverData := map[string]any{
		"environment": "production",
	}

	report, err := Generate(context.Background(), sol, Options{
		ResolverData: resolverData,
		Verbose:      true,
	})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Equal(t, "production", report.ActionPlan[0].MaterializedInputs["target"],
		"rslvr: reference should resolve to the resolver data value")
}

func TestGenerate_ResolverRefMissingData(t *testing.T) {
	rslvrName := "missing"
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "shell",
				Inputs:   map[string]*spec.ValueRef{"target": {Resolver: &rslvrName}},
			},
		},
	}

	// Empty resolver data — the rslvr: reference cannot resolve
	report, err := Generate(context.Background(), sol, Options{
		ResolverData: map[string]any{},
	})
	// Graph build should fail (materialization error) and surface as a warning
	require.NoError(t, err, "Generate should not fail — graph errors become warnings")
	require.NotNil(t, report, "Generate should return a report when graph errors are downgraded to warnings")
	require.NotEmpty(t, report.Warnings, "missing resolver data should be surfaced as a warning")
}

// verbose mode

func TestGenerate_VerboseIncludesMaterializedInputs(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "shell",
				Inputs:   map[string]*spec.ValueRef{"command": {Literal: "echo hello"}},
			},
		},
	}

	report, err := Generate(context.Background(), sol, Options{Verbose: true})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.NotEmpty(t, report.ActionPlan[0].MaterializedInputs)
}

func TestGenerate_NonVerboseExcludesMaterializedInputs(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "shell",
				Inputs:   map[string]*spec.ValueRef{"command": {Literal: "echo hello"}},
			},
		},
	}

	report, err := Generate(context.Background(), sol, Options{Verbose: false})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Empty(t, report.ActionPlan[0].MaterializedInputs)
}

// warnings

func TestGenerate_GraphBuildFailureWarning(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"a": {Provider: "shell", DependsOn: []string{"b"}},
			"b": {Provider: "shell", DependsOn: []string{"a"}},
		},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	require.NotEmpty(t, report.Warnings)
	assert.Contains(t, report.Warnings[0], "action graph build failed")
}

// describeWhatIf helper

func TestDescribeWhatIf_NilRegistry(t *testing.T) {
	msg := describeWhatIf(context.Background(), nil, "shell", nil)
	assert.Equal(t, "Would execute shell provider", msg)
}

func TestDescribeWhatIf_EmptyProvider(t *testing.T) {
	msg := describeWhatIf(context.Background(), nil, "", nil)
	assert.Equal(t, "Would execute action", msg)
}

func TestDescribeWhatIf_UnknownProvider(t *testing.T) {
	reg := provider.NewRegistry()
	msg := describeWhatIf(context.Background(), reg, "nonexistent", nil)
	assert.Equal(t, "Would execute nonexistent provider", msg)
}

// versionString

func TestVersionString_Nil(t *testing.T) {
	assert.Equal(t, "", versionString(nil))
}

func TestVersionString_NonNil(t *testing.T) {
	v := semver.MustParse("3.2.1")
	assert.Equal(t, "3.2.1", versionString(v))
}

// benchmarks

func BenchmarkGenerate_MinimalSolution(b *testing.B) {
	sol := minimalSolution("bench-app")
	opts := Options{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Generate(ctx, sol, opts)
	}
}

func BenchmarkGenerate_WithWorkflow(b *testing.B) {
	sol := minimalSolution("bench-app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: make(map[string]*action.Action, 10),
	}
	for i := 0; i < 10; i++ {
		sol.Spec.Workflow.Actions[fmt.Sprintf("action_%d", i)] = &action.Action{
			Provider:    "shell",
			Description: fmt.Sprintf("Action %d", i),
		}
	}

	opts := Options{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Generate(ctx, sol, opts)
	}
}

func BenchmarkGenerate_WithWhatIf(b *testing.B) {
	sol := minimalSolution("bench-app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: make(map[string]*action.Action, 10),
	}
	for i := 0; i < 10; i++ {
		sol.Spec.Workflow.Actions[fmt.Sprintf("action_%d", i)] = &action.Action{
			Provider: "shell",
			Inputs:   map[string]*spec.ValueRef{"command": {Literal: fmt.Sprintf("echo %d", i)}},
		}
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	_ = reg.Register(newMockProviderWithWhatIf("shell",
		func(_ context.Context, input any) (string, error) {
			inputs, _ := input.(map[string]any)
			cmd, _ := inputs["command"].(string)
			return fmt.Sprintf("Would execute %s", cmd), nil
		},
	))

	opts := Options{Registry: reg}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Generate(ctx, sol, opts)
	}
}
