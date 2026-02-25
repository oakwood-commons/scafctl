// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute

import (
	"bytes"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		resolver *resolver.Resolver
		expected string
	}{
		{
			name: "with provider",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static"}},
				},
			},
			expected: "static",
		},
		{
			name:     "nil resolve",
			resolver: &resolver.Resolver{},
			expected: "unknown",
		},
		{
			name: "empty with",
			resolver: &resolver.Resolver{
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{},
				},
			},
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, ResolverProviderName(tt.resolver))
		})
	}
}

func TestFilterResolversWithDependencies_Empty(t *testing.T) {
	t.Parallel()

	resolvers := []*resolver.Resolver{
		{Name: "a"},
		{Name: "b"},
	}

	// Empty targetNames returns all
	result := FilterResolversWithDependencies(resolvers, nil, nil)
	assert.Len(t, result, 2)
}

func TestFilterResolversWithDependencies_TargetOnly(t *testing.T) {
	t.Parallel()

	resolvers := []*resolver.Resolver{
		{Name: "a", Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{{Provider: "static"}},
		}},
		{Name: "b", Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{{Provider: "static"}},
		}},
		{Name: "c", Resolve: &resolver.ResolvePhase{
			With: []resolver.ProviderSource{{Provider: "static"}},
		}},
	}

	result := FilterResolversWithDependencies(resolvers, []string{"b"}, nil)
	assert.Len(t, result, 1)
	assert.Equal(t, "b", result[0].Name)
}

func TestBuildExecutionData(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("db", &resolver.ExecutionResult{
		Value:             "conn-string",
		Status:            resolver.ExecutionStatusSuccess,
		Phase:             2,
		TotalDuration:     250 * time.Millisecond,
		ProviderCallCount: 1,
		ValueSizeBytes:    42,
		DependencyCount:   1,
		PhaseMetrics: []resolver.PhaseMetrics{
			{Phase: "resolve", Duration: 200 * time.Millisecond},
			{Phase: "validate", Duration: 50 * time.Millisecond},
		},
	})
	resolverCtx.SetResult("env", &resolver.ExecutionResult{
		Value:             "prod",
		Status:            resolver.ExecutionStatusSuccess,
		Phase:             1,
		TotalDuration:     5 * time.Millisecond,
		ProviderCallCount: 1,
		ValueSizeBytes:    4,
		DependencyCount:   0,
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "env",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "db",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "cel"}},
			},
		},
	}

	data := BuildExecutionData(resolverCtx, resolvers, 300*time.Millisecond)

	assert.Contains(t, data, "resolvers")
	assert.Contains(t, data, "summary")

	resolversMeta, ok := data["resolvers"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, resolversMeta, 2)

	envMeta, ok := resolversMeta["env"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, envMeta["phase"])
	assert.Equal(t, "success", envMeta["status"])
	assert.Equal(t, "static", envMeta["provider"])

	dbMeta, ok := resolversMeta["db"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 2, dbMeta["phase"])
	assert.Equal(t, "cel", dbMeta["provider"])
	assert.Equal(t, "250ms", dbMeta["duration"])

	phaseMetrics, ok := dbMeta["phaseMetrics"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, phaseMetrics, 2)

	summary, ok := data["summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "300ms", summary["totalDuration"])
	assert.Equal(t, 2, summary["resolverCount"])
}

func TestBuildExecutionData_MissingResult(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolvers := []*resolver.Resolver{
		{
			Name: "missing",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		},
	}

	data := BuildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	missingMeta, ok := resolversMeta["missing"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0, missingMeta["phase"])
	assert.Equal(t, "0s", missingMeta["duration"])
	assert.Equal(t, "unknown", missingMeta["status"])
	assert.Equal(t, "http", missingMeta["provider"])
}

func TestBuildExecutionData_WithFailedAttempts(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("fallback", &resolver.ExecutionResult{
		Value:         "final-value",
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 100 * time.Millisecond,
		FailedAttempts: []resolver.ProviderAttempt{
			{
				Provider:   "http",
				Phase:      "resolve",
				Error:      "connection refused",
				Duration:   50 * time.Millisecond,
				OnError:    "continue",
				SourceStep: 0,
			},
		},
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "fallback",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
	}

	data := BuildExecutionData(resolverCtx, resolvers, 100*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	fbMeta := resolversMeta["fallback"].(map[string]any)

	attempts, ok := fbMeta["failedAttempts"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, attempts, 1)
	assert.Equal(t, "http", attempts[0]["provider"])
	assert.Equal(t, "connection refused", attempts[0]["error"])
}

func TestBuildExecutionData_CELDependencies(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("env", &resolver.ExecutionResult{
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 5 * time.Millisecond,
	})
	resolverCtx.SetResult("region", &resolver.ExecutionResult{
		Status:        resolver.ExecutionStatusSuccess,
		Phase:         1,
		TotalDuration: 5 * time.Millisecond,
	})

	celExpr := celexp.Expression("'api.' + _.env + '.' + _.region + '.example.com'")
	resolverCtx.SetResult("hostname", &resolver.ExecutionResult{
		Status:          resolver.ExecutionStatusSuccess,
		Phase:           2,
		TotalDuration:   10 * time.Millisecond,
		DependencyCount: 2,
	})

	resolvers := []*resolver.Resolver{
		{
			Name: "env",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "region",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "static"}},
			},
		},
		{
			Name: "hostname",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*resolver.ValueRef{
							"expression": {Expr: &celExpr},
						},
					},
				},
			},
		},
	}

	data := BuildExecutionData(resolverCtx, resolvers, 20*time.Millisecond)

	resolversMeta := data["resolvers"].(map[string]any)
	hostnameMeta := resolversMeta["hostname"].(map[string]any)

	deps, ok := hostnameMeta["dependencies"].([]string)
	require.True(t, ok)
	assert.Len(t, deps, 2)
	assert.Contains(t, deps, "env")
	assert.Contains(t, deps, "region")
}

func TestBuildProviderSummary(t *testing.T) {
	t.Parallel()

	resolverCtx := resolver.NewContext()
	resolverCtx.SetResult("a", &resolver.ExecutionResult{
		Status:            resolver.ExecutionStatusSuccess,
		TotalDuration:     100 * time.Millisecond,
		ProviderCallCount: 1,
	})
	resolverCtx.SetResult("b", &resolver.ExecutionResult{
		Status:            resolver.ExecutionStatusSuccess,
		TotalDuration:     200 * time.Millisecond,
		ProviderCallCount: 2,
	})

	resolvers := []*resolver.Resolver{
		{Name: "a", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}},
		{Name: "b", Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static"}}}},
	}

	summary := BuildProviderSummary(resolverCtx, resolvers)

	staticSummary, ok := summary["static"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 2, staticSummary["resolverCount"])
	assert.Equal(t, 3, staticSummary["callCount"])
	assert.Equal(t, 2, staticSummary["successCount"])
	assert.Equal(t, 0, staticSummary["failedCount"])
}

func TestRenderGraph_JSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	err := RenderGraph(&buf, nil, data, "json")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "key")
}

func TestRenderGraph_UnsupportedFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := RenderGraph(&buf, nil, nil, "invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported graph format")
}
