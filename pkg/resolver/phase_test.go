// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
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
			phases, err := BuildPhases(tt.resolvers, nil)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, phases)

			if tt.validate != nil {
				tt.validate(t, phases)
			}
		})
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
	phases, err := BuildPhases([]*Resolver{}, nil)
	require.NoError(t, err)
	assert.Empty(t, phases)
}

func TestBuildPhases_StandaloneResolver(t *testing.T) {
	// Test with a resolver that has no dependencies
	resolvers := []*Resolver{
		{
			Name: "standalone",
		},
	}

	phases, err := BuildPhases(resolvers, nil)
	require.NoError(t, err)
	assert.Len(t, phases, 1)
	assert.Equal(t, 1, phases[0].Phase)
	assert.Len(t, phases[0].Resolvers, 1)
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
