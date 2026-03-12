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
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
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

func newMockProvider(name, mockBehavior string) *mockProvider {
	return &mockProvider{
		desc: &provider.Descriptor{
			Name:         name,
			APIVersion:   "v1",
			Version:      semver.MustParse("1.0.0"),
			Description:  fmt.Sprintf("Mock %s provider for testing", name),
			MockBehavior: mockBehavior,
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

// basic fields

func TestGenerate_BasicFields(t *testing.T) {
	sol := minimalSolution("test-app")
	sol.Metadata.Version = semver.MustParse("2.5.0")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.True(t, report.DryRun)
	assert.Equal(t, "test-app", report.Solution)
	assert.Equal(t, "2.5.0", report.Version)
	assert.False(t, report.HasResolvers)
	assert.False(t, report.HasWorkflow)
}

func TestGenerate_NilVersion(t *testing.T) {
	sol := minimalSolution("app")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	assert.Equal(t, "", report.Version)
}

func TestGenerate_Parameters(t *testing.T) {
	sol := minimalSolution("app")
	params := map[string]any{"env": "dev", "count": 3}

	report, err := Generate(context.Background(), sol, Options{Params: params})
	require.NoError(t, err)
	assert.Equal(t, params, report.Parameters)
}

// resolvers

func TestGenerate_ResolversResolved(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"name":   {},
		"region": {},
	}

	data := map[string]any{
		"name":   "my-app",
		"region": "us-east-1",
	}

	report, err := Generate(context.Background(), sol, Options{ResolverData: data})
	require.NoError(t, err)

	assert.True(t, report.HasResolvers)
	require.Len(t, report.Resolvers, 2)

	nameRes := report.Resolvers["name"]
	assert.Equal(t, "my-app", nameRes.Value)
	assert.Equal(t, "resolved", nameRes.Status)
	assert.True(t, nameRes.DryRun)
}

func TestGenerate_ResolverMissing(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"name": {},
	}

	report, err := Generate(context.Background(), sol, Options{ResolverData: map[string]any{}})
	require.NoError(t, err)

	nameRes := report.Resolvers["name"]
	assert.Equal(t, "not-resolved", nameRes.Status)
	assert.True(t, nameRes.DryRun)
	assert.Nil(t, nameRes.Value)

	require.Len(t, report.Warnings, 1)
	assert.Contains(t, report.Warnings[0], `resolver "name" did not produce a value`)
}

func TestGenerate_NilResolverData(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"x": {},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.Equal(t, "not-resolved", report.Resolvers["x"].Status)
	assert.Len(t, report.Warnings, 1)
}

func TestGenerate_NoResolvers(t *testing.T) {
	sol := minimalSolution("app")

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)

	assert.False(t, report.HasResolvers)
	assert.Empty(t, report.Resolvers)
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

// mock behaviors

func TestGenerate_MockBehaviorsFromRegistry(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"name": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "parameter"},
				},
			},
		},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	require.NoError(t, reg.Register(newMockProvider("parameter", "Returns configured default value")))
	require.NoError(t, reg.Register(newMockProvider("shell", "Echoes command without executing")))

	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)

	require.Len(t, report.MockBehaviors, 2)
	assert.Equal(t, "parameter", report.MockBehaviors[0].Provider)
	assert.Equal(t, "Returns configured default value", report.MockBehaviors[0].MockBehavior)
	assert.Equal(t, "shell", report.MockBehaviors[1].Provider)
}

func TestGenerate_NilRegistryNoMockBehaviors(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	report, err := Generate(context.Background(), sol, Options{})
	require.NoError(t, err)
	assert.Empty(t, report.MockBehaviors)
}

func TestGenerate_ActionMockBehaviorFromRegistry(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	require.NoError(t, reg.Register(newMockProvider("shell", "Echoes command")))

	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)
	require.Len(t, report.ActionPlan, 1)
	assert.Equal(t, "Echoes command", report.ActionPlan[0].MockBehavior)
}

// finally section

func TestGenerate_FinallyProvidersInMockBehaviors(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {Provider: "shell"},
		},
		Finally: map[string]*action.Action{
			"cleanup": {Provider: "http"},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	require.NoError(t, reg.Register(newMockProvider("shell", "mock shell behavior")))
	require.NoError(t, reg.Register(newMockProvider("http", "mock http request")))

	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)

	providers := make(map[string]bool)
	for _, mb := range report.MockBehaviors {
		providers[mb.Provider] = true
	}
	assert.True(t, providers["shell"])
	assert.True(t, providers["http"])
}

// empty provider filtered

func TestGenerate_EmptyProviderFiltered(t *testing.T) {
	sol := minimalSolution("app")
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"name": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: ""},
				},
			},
		},
	}

	reg := provider.NewRegistry(provider.WithAllowOverwrite(true))
	report, err := Generate(context.Background(), sol, Options{Registry: reg})
	require.NoError(t, err)
	assert.Empty(t, report.MockBehaviors)
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

func BenchmarkGenerate_WithResolvers(b *testing.B) {
	sol := minimalSolution("bench-app")
	sol.Spec.Resolvers = make(map[string]*resolver.Resolver, 20)
	data := make(map[string]any, 20)
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("resolver_%d", i)
		sol.Spec.Resolvers[name] = &resolver.Resolver{}
		data[name] = fmt.Sprintf("value_%d", i)
	}

	opts := Options{ResolverData: data}
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
