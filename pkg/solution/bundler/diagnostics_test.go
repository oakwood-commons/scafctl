// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

func ptrString(s string) *string                   { return &s }
func ptrExpr(s string) *celexp.Expression          { e := celexp.Expression(s); return &e }
func ptrTmpl(s string) *gotmpl.GoTemplatingContent { t := gotmpl.GoTemplatingContent(s); return &t }

func TestDetectDynamicPaths_NilSolution(t *testing.T) {
	warnings := DetectDynamicPaths(nil)
	assert.Nil(t, warnings)
}

func TestDetectDynamicPaths_EmptySolution(t *testing.T) {
	sol := &solution.Solution{}
	warnings := DetectDynamicPaths(sol)
	assert.Empty(t, warnings)
}

func TestDetectDynamicPaths_StaticPathsNoWarnings(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"config": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Literal: "config.yaml"},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Empty(t, warnings, "literal paths should not produce warnings")
}

func TestDetectDynamicPaths_ExprInFilePath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"template": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Expr: ptrExpr("'configs/' + environment + '.yaml'")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Equal(t, "expr", warnings[0].Kind)
	assert.Contains(t, warnings[0].Location, "resolver 'template'")
	assert.Contains(t, warnings[0].Expression, "configs/")
}

func TestDetectDynamicPaths_TmplInFilePath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"tmplResolver": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Tmpl: ptrTmpl("{{ .env }}/config.yaml")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Equal(t, "tmpl", warnings[0].Kind)
}

func TestDetectDynamicPaths_ResolverRefInFilePath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"dynamicFile": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Resolver: ptrString("configPath")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Equal(t, "rslvr", warnings[0].Kind)
	assert.Equal(t, "configPath", warnings[0].Expression)
}

func TestDetectDynamicPaths_SolutionProvider(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"nested": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "solution",
								Inputs: map[string]*spec.ValueRef{
									"source": {Expr: ptrExpr("dynamicPath()")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Location, "solution provider")
}

func TestDetectDynamicPaths_TransformFilePath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"xform": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "parameter", Inputs: map[string]*spec.ValueRef{"name": {Literal: "x"}}},
						},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Expr: ptrExpr("_.value + '.yaml'")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Location, "transform")
}

func TestDetectDynamicPaths_ActionDynamicPath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"deploy": {
						Provider: "file",
						Inputs: map[string]*spec.ValueRef{
							"path": {Tmpl: ptrTmpl("{{ .target }}/deploy.yaml")},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Location, "action 'deploy'")
	assert.Equal(t, "tmpl", warnings[0].Kind)
}

func TestDetectDynamicPaths_FinallyActionDynamicPath(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &actionpkg.Workflow{
				Finally: map[string]*actionpkg.Action{
					"cleanup": {
						Provider: "solution",
						Inputs: map[string]*spec.ValueRef{
							"source": {Resolver: ptrString("cleanupPath")},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Location, "action 'cleanup'")
}

func TestDetectDynamicPaths_MultipleWarnings(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Expr: ptrExpr("dynamic1()")},
								},
							},
						},
					},
				},
				"r2": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "file",
								Inputs: map[string]*spec.ValueRef{
									"path": {Tmpl: ptrTmpl("{{ .x }}")},
								},
							},
						},
					},
				},
			},
			Workflow: &actionpkg.Workflow{
				Actions: map[string]*actionpkg.Action{
					"a1": {
						Provider: "file",
						Inputs: map[string]*spec.ValueRef{
							"path": {Resolver: ptrString("ref")},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.GreaterOrEqual(t, len(warnings), 3)
}

func TestDetectDynamicPaths_NilResolverEntries(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"nilResolver": nil,
				"noResolve": {
					Resolve: nil,
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Empty(t, warnings, "nil resolvers should not cause panics")
}

func TestDetectDynamicPaths_NonFileProvider(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "env",
								Inputs: map[string]*spec.ValueRef{
									"name": {Expr: ptrExpr("'HOME'")},
								},
							},
						},
					},
				},
			},
		},
	}

	warnings := DetectDynamicPaths(sol)
	assert.Empty(t, warnings, "non-file/solution providers should not produce warnings")
}
