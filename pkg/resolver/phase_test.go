// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPhases(t *testing.T) {
	tests := []struct {
		name      string
		resolvers []*Resolver
		wantErr   bool
		validate  func(t *testing.T, phases []*PhaseGroup)
	}{
		{
			name:      "empty resolvers",
			resolvers: []*Resolver{},
			wantErr:   false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				assert.Equal(t, 0, len(phases))
			},
		},
		{
			name: "single resolver no dependencies",
			resolvers: []*Resolver{
				{
					Name: "simple",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 1, len(phases))
				assert.Equal(t, 1, phases[0].Phase)
				assert.Equal(t, 1, len(phases[0].Resolvers))
				assert.Equal(t, "simple", phases[0].Resolvers[0].Name)
			},
		},
		{
			name: "two independent resolvers",
			resolvers: []*Resolver{
				{
					Name: "resolver1",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test1"},
								},
							},
						},
					},
				},
				{
					Name: "resolver2",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test2"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 1, len(phases))
				assert.Equal(t, 1, phases[0].Phase)
				assert.Equal(t, 2, len(phases[0].Resolvers))

				// Both resolvers should be in phase 1
				names := []string{phases[0].Resolvers[0].Name, phases[0].Resolvers[1].Name}
				assert.Contains(t, names, "resolver1")
				assert.Contains(t, names, "resolver2")
			},
		},
		{
			name: "simple dependency chain",
			resolvers: []*Resolver{
				{
					Name: "base",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "base"},
								},
							},
						},
					},
				},
				{
					Name: "dependent",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("base")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 2, len(phases))

				// Phase 1 should have base
				assert.Equal(t, 1, phases[0].Phase)
				require.Equal(t, 1, len(phases[0].Resolvers))
				assert.Equal(t, "base", phases[0].Resolvers[0].Name)

				// Phase 2 should have dependent
				assert.Equal(t, 2, phases[1].Phase)
				require.Equal(t, 1, len(phases[1].Resolvers))
				assert.Equal(t, "dependent", phases[1].Resolvers[0].Name)
			},
		},
		{
			name: "multi-level dependency chain",
			resolvers: []*Resolver{
				{
					Name: "level1",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "l1"},
								},
							},
						},
					},
				},
				{
					Name: "level2",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("level1")},
								},
							},
						},
					},
				},
				{
					Name: "level3",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("level2")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 3, len(phases))

				assert.Equal(t, "level1", phases[0].Resolvers[0].Name)
				assert.Equal(t, "level2", phases[1].Resolvers[0].Name)
				assert.Equal(t, "level3", phases[2].Resolvers[0].Name)
			},
		},
		{
			name: "diamond dependency pattern",
			resolvers: []*Resolver{
				{
					Name: "root",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "root"},
								},
							},
						},
					},
				},
				{
					Name: "left",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("root")},
								},
							},
						},
					},
				},
				{
					Name: "right",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("root")},
								},
							},
						},
					},
				},
				{
					Name: "bottom",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"left":  {Resolver: stringPtr("left")},
									"right": {Resolver: stringPtr("right")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 3, len(phases))

				// Phase 1: root
				assert.Equal(t, 1, len(phases[0].Resolvers))
				assert.Equal(t, "root", phases[0].Resolvers[0].Name)

				// Phase 2: left and right (parallel)
				assert.Equal(t, 2, len(phases[1].Resolvers))
				names := []string{phases[1].Resolvers[0].Name, phases[1].Resolvers[1].Name}
				assert.Contains(t, names, "left")
				assert.Contains(t, names, "right")

				// Phase 3: bottom
				assert.Equal(t, 1, len(phases[2].Resolvers))
				assert.Equal(t, "bottom", phases[2].Resolvers[0].Name)
			},
		},
		{
			name: "circular dependency should error",
			resolvers: []*Resolver{
				{
					Name: "a",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("b")},
								},
							},
						},
					},
				},
				{
					Name: "b",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("a")},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "self dependency should error",
			resolvers: []*Resolver{
				{
					Name: "self",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"value": {Resolver: stringPtr("self")},
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "conditional resolver with dependency",
			resolvers: []*Resolver{
				{
					Name: "enabled",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: true},
								},
							},
						},
					},
				},
				{
					Name: "conditional",
					When: &Condition{
						Expr: celExpPtr("_.enabled == true"),
					},
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "test"},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 2, len(phases))

				// enabled should be in phase 1
				assert.Equal(t, "enabled", phases[0].Resolvers[0].Name)

				// conditional should be in phase 2 (depends on enabled via when condition)
				assert.Equal(t, "conditional", phases[1].Resolvers[0].Name)
			},
		},
		{
			name: "cel expression dependencies",
			resolvers: []*Resolver{
				{
					Name: "env",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "prod"},
								},
							},
						},
					},
				},
				{
					Name: "region",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "us-east"},
								},
							},
						},
					},
				},
				{
					Name: "combined",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"expr": {Expr: celExpPtr("_.env + '-' + _.region")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 2, len(phases))

				// env and region should be in phase 1 (parallel)
				assert.Equal(t, 2, len(phases[0].Resolvers))

				// combined should be in phase 2
				assert.Equal(t, 1, len(phases[1].Resolvers))
				assert.Equal(t, "combined", phases[1].Resolvers[0].Name)
			},
		},
		{
			name: "template dependencies",
			resolvers: []*Resolver{
				{
					Name: "base",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: "base"},
								},
							},
						},
					},
				},
				{
					Name: "templated",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Tmpl: tmplPtr("prefix-{{ ._.base }}-suffix")},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 2, len(phases))
				assert.Equal(t, "base", phases[0].Resolvers[0].Name)
				assert.Equal(t, "templated", phases[1].Resolvers[0].Name)
			},
		},
		{
			name: "complex multi-phase scenario",
			resolvers: []*Resolver{
				// Phase 1: independent resolvers
				{
					Name: "config",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "config"}}}},
					},
				},
				{
					Name: "env",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "prod"}}}},
					},
				},
				// Phase 2: depends on phase 1
				{
					Name: "region",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("config")}}}},
					},
				},
				{
					Name: "account",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("env")}}}},
					},
				},
				// Phase 3: depends on phase 2
				{
					Name: "final",
					Resolve: &ResolvePhase{
						With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{
							"region":  {Resolver: stringPtr("region")},
							"account": {Resolver: stringPtr("account")},
						}}},
					},
				},
			},
			wantErr: false,
			validate: func(t *testing.T, phases []*PhaseGroup) {
				require.Equal(t, 3, len(phases))

				// Phase 1: config and env
				assert.Equal(t, 2, len(phases[0].Resolvers))

				// Phase 2: region and account
				assert.Equal(t, 2, len(phases[1].Resolvers))

				// Phase 3: final
				assert.Equal(t, 1, len(phases[2].Resolvers))
				assert.Equal(t, "final", phases[2].Resolvers[0].Name)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildPhases(tt.resolvers, nil)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.validate != nil {
				tt.validate(t, result.Phases)
			}
		})
	}
}

func TestBuildPhases_PlanData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resolvers []*Resolver
		wantErr   bool
		validate  func(t *testing.T, plan PlanData)
	}{
		{
			name:      "empty resolvers returns empty plan",
			resolvers: []*Resolver{},
			validate: func(t *testing.T, plan PlanData) {
				assert.Empty(t, plan)
			},
		},
		{
			name: "root resolver has phase 1 and no deps",
			resolvers: []*Resolver{
				{
					Name: "root",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "v"}}},
						},
					},
				},
			},
			validate: func(t *testing.T, plan PlanData) {
				require.Contains(t, plan, "root")
				rp := plan["root"]
				assert.Equal(t, 1, rp.Phase)
				assert.Empty(t, rp.DependsOn)
				assert.Equal(t, 0, rp.DependencyCount)
			},
		},
		{
			name: "dependent resolver reflects correct phase and deps",
			resolvers: []*Resolver{
				{
					Name: "base",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "b"}}},
						},
					},
				},
				{
					Name: "child",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("base")}}},
						},
					},
				},
			},
			validate: func(t *testing.T, plan PlanData) {
				require.Contains(t, plan, "base")
				require.Contains(t, plan, "child")

				basePlan := plan["base"]
				assert.Equal(t, 1, basePlan.Phase)
				assert.Empty(t, basePlan.DependsOn)
				assert.Equal(t, 0, basePlan.DependencyCount)

				childPlan := plan["child"]
				assert.Equal(t, 2, childPlan.Phase)
				assert.Equal(t, []string{"base"}, childPlan.DependsOn)
				assert.Equal(t, 1, childPlan.DependencyCount)
			},
		},
		{
			name: "three-level chain phases are correct",
			resolvers: []*Resolver{
				{
					Name: "l1",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "l1"}}},
						},
					},
				},
				{
					Name: "l2",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("l1")}}},
						},
					},
				},
				{
					Name: "l3",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: stringPtr("l2")}}},
						},
					},
				},
			},
			validate: func(t *testing.T, plan PlanData) {
				assert.Equal(t, 1, plan["l1"].Phase)
				assert.Equal(t, 2, plan["l2"].Phase)
				assert.Equal(t, 3, plan["l3"].Phase)
				assert.Equal(t, 0, plan["l1"].DependencyCount)
				assert.Equal(t, 1, plan["l2"].DependencyCount)
				assert.Equal(t, 1, plan["l3"].DependencyCount)
			},
		},
		{
			name: "parallel resolvers share the same phase in plan",
			resolvers: []*Resolver{
				{
					Name: "a",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "a"}}},
						},
					},
				},
				{
					Name: "b",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "b"}}},
						},
					},
				},
				{
					Name: "c",
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "cel",
								Inputs: map[string]*ValueRef{
									"a": {Resolver: stringPtr("a")},
									"b": {Resolver: stringPtr("b")},
								},
							},
						},
					},
				},
			},
			validate: func(t *testing.T, plan PlanData) {
				assert.Equal(t, 1, plan["a"].Phase)
				assert.Equal(t, 1, plan["b"].Phase)
				assert.Equal(t, 2, plan["c"].Phase)
				assert.Equal(t, 2, plan["c"].DependencyCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := BuildPhases(tt.resolvers, nil)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)
			tt.validate(t, result.Plan)
		})
	}
}

func BenchmarkBuildPhases_WithPlan(b *testing.B) {
	resolvers := make([]*Resolver, 0, 10)
	resolvers = append(resolvers, &Resolver{
		Name: "root",
		Resolve: &ResolvePhase{
			With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "v"}}}},
		},
	})
	for i := 1; i < 10; i++ {
		prev := resolvers[i-1].Name
		resolvers = append(resolvers, &Resolver{
			Name: fmt.Sprintf("r%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{Provider: "cel", Inputs: map[string]*ValueRef{"value": {Resolver: &prev}}},
				},
			},
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildPhases(resolvers, nil)
	}
}

func TestGetPhaseForResolver(t *testing.T) {
	phases := []*PhaseGroup{
		{
			Phase: 1,
			Resolvers: []*Resolver{
				{Name: "r1"},
				{Name: "r2"},
			},
		},
		{
			Phase: 2,
			Resolvers: []*Resolver{
				{Name: "r3"},
			},
		},
	}

	tests := []struct {
		name         string
		resolverName string
		want         int
	}{
		{
			name:         "resolver in phase 1",
			resolverName: "r1",
			want:         1,
		},
		{
			name:         "resolver in phase 2",
			resolverName: "r3",
			want:         2,
		},
		{
			name:         "resolver not found",
			resolverName: "nonexistent",
			want:         0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPhaseForResolver(phases, tt.resolverName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetMaxPhase(t *testing.T) {
	tests := []struct {
		name   string
		phases []*PhaseGroup
		want   int
	}{
		{
			name:   "empty phases",
			phases: []*PhaseGroup{},
			want:   0,
		},
		{
			name: "single phase",
			phases: []*PhaseGroup{
				{Phase: 1},
			},
			want: 1,
		},
		{
			name: "multiple phases",
			phases: []*PhaseGroup{
				{Phase: 1},
				{Phase: 2},
				{Phase: 3},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetMaxPhase(tt.phases)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetResolversInPhase(t *testing.T) {
	phases := []*PhaseGroup{
		{
			Phase: 1,
			Resolvers: []*Resolver{
				{Name: "r1"},
				{Name: "r2"},
			},
		},
		{
			Phase: 2,
			Resolvers: []*Resolver{
				{Name: "r3"},
			},
		},
	}

	tests := []struct {
		name     string
		phaseNum int
		want     []string
	}{
		{
			name:     "phase 1",
			phaseNum: 1,
			want:     []string{"r1", "r2"},
		},
		{
			name:     "phase 2",
			phaseNum: 2,
			want:     []string{"r3"},
		},
		{
			name:     "non-existent phase",
			phaseNum: 99,
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetResolversInPhase(phases, tt.phaseNum)

			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				require.Equal(t, len(tt.want), len(got))
				for i, name := range tt.want {
					assert.Equal(t, name, got[i].Name)
				}
			}
		})
	}
}

func TestBuildPhases_EmptyResolvers(t *testing.T) {
	result, err := BuildPhases([]*Resolver{}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Phases)
}

func TestBuildPhases_StandaloneResolver(t *testing.T) {
	// Test with a resolver that has no dependencies
	resolvers := []*Resolver{
		{
			Name: "standalone",
		},
	}

	result, err := BuildPhases(resolvers, nil)
	require.NoError(t, err)
	assert.Len(t, result.Phases, 1)
	assert.Equal(t, 1, result.Phases[0].Phase)
	assert.Len(t, result.Phases[0].Resolvers, 1)
}

func TestBuildPhases_DropsUnknownInferredDeps(t *testing.T) {
	// A go-template resolver whose inline template references a bare {{ .field }}
	// accessor to an unknown root must NOT hard-fail phase building. Such an
	// inferred edge points at a non-existent resolver and is dropped as a
	// best-effort ordering hint rather than producing a "depends on X but X
	// wasn't present" DAG error.
	resolvers := []*Resolver{
		{
			Name: "rendered",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .doesNotExist }}`},
						},
					},
				},
			},
		},
	}

	result, err := BuildPhases(resolvers, nil)
	require.NoError(t, err)
	assert.Len(t, result.Phases, 1)
	assert.Equal(t, 1, result.Phases[0].Phase)
	assert.Len(t, result.Phases[0].Resolvers, 1)
}

func TestBuildPhases_KeepsKnownInferredDeps(t *testing.T) {
	// An inferred edge to an existing resolver is retained and orders the phases.
	resolvers := []*Resolver{
		{
			Name: "rendered",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ ._.appName }}`},
						},
					},
				},
			},
		},
		{Name: "appName"},
	}

	result, err := BuildPhases(resolvers, nil)
	require.NoError(t, err)
	require.Len(t, result.Phases, 2)
	// appName has no deps -> phase 1; rendered depends on appName -> phase 2.
	assert.Equal(t, "appName", result.Phases[0].Resolvers[0].Name)
	assert.Equal(t, "rendered", result.Phases[1].Resolvers[0].Name)
}

func TestBuildPhases_KeepsUnknownStrictDeps(t *testing.T) {
	// Unknown targets reached via a *strict* reference -- dependsOn, a CEL
	// `_.name` reference, or an rslvr: ValueRef -- are almost always typos and
	// must fail fast during graph construction rather than being silently
	// dropped. Each subtest wires a single strict reference to a non-existent
	// resolver and expects BuildPhases to error.
	tests := []struct {
		name     string
		resolver *Resolver
	}{
		{
			name: "dependsOn typo",
			resolver: &Resolver{
				Name:      "app",
				DependsOn: []string{"missing"},
				Resolve: &ResolvePhase{
					With: []ProviderSource{{Provider: "parameter"}},
				},
			},
		},
		{
			name: "cel reference typo",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"expression": {Expr: exprPtr("_.missing")},
						},
					}},
				},
			},
		},
		{
			name: "rslvr reference typo",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Resolver: stringPtr("missing")},
						},
					}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildPhases([]*Resolver{tt.resolver}, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "missing")
		})
	}
}

func TestExtractStrictDependencies(t *testing.T) {
	tests := []struct {
		name     string
		resolver *Resolver
		want     []string
	}{
		{
			name: "dependsOn is strict",
			resolver: &Resolver{
				Name:      "app",
				DependsOn: []string{"env", "region"},
			},
			want: []string{"env", "region"},
		},
		{
			name: "cel and rslvr refs are strict",
			resolver: &Resolver{
				Name: "app",
				When: &Condition{Expr: exprPtr("_.gate")},
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Resolver: stringPtr("env")},
							"expr":  {Expr: exprPtr("_.region")},
						},
					}},
				},
			},
			want: []string{"gate", "env", "region"},
		},
		{
			name: "bare template accessor is not strict",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"template": {Literal: `{{ .notStrict }}`},
						},
					}},
				},
			},
			want: nil,
		},
		{
			name: "tmpl ValueRef is not strict",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"value": {Tmpl: tmplPtr(`{{ .notStrict }}`)},
						},
					}},
				},
			},
			want: nil,
		},
		{
			name: "rslvr and expr nested in literal map are strict",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Literal: map[string]any{
								"env":  map[string]any{"rslvr": "region"},
								"zone": map[string]any{"expr": "_.zone"},
							}},
						},
					}},
				},
			},
			want: []string{"region", "zone"},
		},
		{
			name: "cel string in literal array is strict",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "cel",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{"_.alpha", "plain-string"}},
						},
					}},
				},
			},
			want: []string{"alpha"},
		},
		{
			name: "tmpl nested in literal map is not strict",
			resolver: &Resolver{
				Name: "app",
				Resolve: &ResolvePhase{
					With: []ProviderSource{{
						Provider: "go-template",
						Inputs: map[string]*ValueRef{
							"value": {Literal: map[string]any{
								"t": map[string]any{"tmpl": "{{ .beta }}"},
							}},
						},
					}},
				},
			},
			want: nil,
		},
		{
			name: "self reference is excluded",
			resolver: &Resolver{
				Name:      "app",
				DependsOn: []string{"app", "env"},
			},
			want: []string{"env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStrictDependencies(tt.resolver)
			assert.Len(t, got, len(tt.want))
			for _, dep := range tt.want {
				assert.True(t, got[dep], "expected strict dep %q", dep)
			}
		})
	}
}

func TestResolverDagObject_GetDependencyKeys(t *testing.T) {
	resolver := &Resolver{
		Name: "test",
		Resolve: &ResolvePhase{
			With: []ProviderSource{
				{
					Provider: "cel",
					Inputs: map[string]*ValueRef{
						"value": {Resolver: stringPtr("dependency")},
					},
				},
			},
		},
	}

	// Pre-compute dependencies as the actual implementation does
	deps := extractDependencies(resolver, nil)
	obj := &resolverDagObject{
		resolver: resolver,
		deps:     deps,
	}

	// Call with the required parameters (empty maps for this test)
	keys := obj.GetDependencyKeys(map[string]string{}, map[string][]string{}, map[string]string{})
	assert.ElementsMatch(t, []string{"dependency"}, keys)
}

func TestExtractDepsFromTemplate_UnderscoreVariant(t *testing.T) {
	// Test extractDepsFromTemplate with different underscore patterns
	deps := make(map[string]bool)

	// Template with ._. pattern
	extractDepsFromTemplate("{{ ._.var1 }}", deps)
	assert.Contains(t, deps, "var1")

	// Clear deps
	deps = make(map[string]bool)

	// Template with ._ pattern (without second dot)
	extractDepsFromTemplate("{{ ._var2 }}", deps)
	assert.Contains(t, deps, "var2")
}

func TestBuildPhases_DeferredWork(t *testing.T) {
	staticSource := &ResolvePhase{
		With: []ProviderSource{{
			Provider: "static",
			Inputs:   map[string]*ValueRef{"value": {Literal: "x"}},
		}},
	}

	t.Run("no deferred work when validations are self-only", func(t *testing.T) {
		resolvers := []*Resolver{
			{
				Name:    "env",
				Resolve: staticSource,
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
					},
				},
			},
		}
		result, err := BuildPhases(resolvers, nil)
		require.NoError(t, err)
		assert.Empty(t, result.DeferredWork)
	})

	t.Run("cross-resolver validation is deferred and excluded from DAG", func(t *testing.T) {
		resolvers := []*Resolver{
			{Name: "env", Resolve: staticSource},
			{Name: "region", Resolve: staticSource},
			{
				Name:    "checker",
				Resolve: staticSource,
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("_.env != _.region")}}},
					},
				},
			},
		}
		result, err := BuildPhases(resolvers, nil)
		require.NoError(t, err)

		// The cross-resolver validation must NOT create resolution-graph edges.
		assert.Empty(t, result.Deps["checker"], "validate refs must not be resolution deps")

		require.Len(t, result.DeferredWork, 1)
		unit := result.DeferredWork[0]
		assert.Equal(t, "checker", unit.ResolverName)
		assert.Equal(t, []int{1}, unit.RuleIndices)
		assert.Equal(t, []string{"env", "region"}, unit.DependsOn)
		assert.False(t, unit.PhaseWhenDeferred)
	})

	t.Run("phase-level foreign when defers entire block", func(t *testing.T) {
		resolvers := []*Resolver{
			{Name: "gate", Resolve: staticSource},
			{
				Name:    "checker",
				Resolve: staticSource,
				Validate: &ValidatePhase{
					When: &Condition{Expr: celExpPtr("_.gate == true")},
					With: []ProviderValidation{
						{Provider: "validation", Inputs: map[string]*ValueRef{"expression": {Expr: celExpPtr("__self != ''")}}},
					},
				},
			},
		}
		result, err := BuildPhases(resolvers, nil)
		require.NoError(t, err)
		assert.Empty(t, result.Deps["checker"])

		require.Len(t, result.DeferredWork, 1)
		unit := result.DeferredWork[0]
		assert.Equal(t, "checker", unit.ResolverName)
		assert.Equal(t, []int{0}, unit.RuleIndices)
		assert.Equal(t, []string{"gate"}, unit.DependsOn)
		assert.True(t, unit.PhaseWhenDeferred)
	})
}
