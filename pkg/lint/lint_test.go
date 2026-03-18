// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider implements provider.Provider for testing.
type fakeProvider struct {
	desc *provider.Descriptor
}

func (f *fakeProvider) Descriptor() *provider.Descriptor { return f.desc }

func (f *fakeProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return nil, nil
}

func newFakeProvider(name string, props map[string]*jsonschema.Schema) *fakeProvider {
	return &fakeProvider{
		desc: &provider.Descriptor{
			Name:       name,
			APIVersion: "v1",
			Version:    semver.MustParse("1.0.0"),
			Schema: &jsonschema.Schema{
				Type:       "object",
				Properties: props,
			},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {Type: "object"},
			},
			Description:  "Test provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
		},
	}
}

func TestLintProviderInputs_UnknownInput(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"name": {Type: "string"},
		"url":  {Type: "string"},
	})
	require.NoError(t, reg.Register(fp))

	expr := celexp.Expression("_.env")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"name":          {Literal: "hello"},
									"unknown_field": {Literal: "oops"},
									"url":           {Expr: &expr},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var unknownFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "unknown-provider-input" {
			unknownFindings = append(unknownFindings, f)
		}
	}
	require.Len(t, unknownFindings, 1)
	assert.Contains(t, unknownFindings[0].Message, "unknown_field")
	assert.Contains(t, unknownFindings[0].Message, "test-provider")
}

func TestLintProviderInputs_InvalidLiteralType(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"count": {Type: "integer"},
	})
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"count": {Literal: "not-a-number"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var typeFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "invalid-provider-input-type" {
			typeFindings = append(typeFindings, f)
		}
	}
	require.Len(t, typeFindings, 1)
	assert.Contains(t, typeFindings[0].Message, "count")
}

func TestLintProviderInputs_ValidLiteral(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"name": {Type: "string"},
	})
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"name": {Literal: "valid-string"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "unknown-provider-input", f.RuleName)
		assert.NotEqual(t, "invalid-provider-input-type", f.RuleName)
	}
}

func TestLintProviderInputs_SkipsDynamicValues(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("test-provider", map[string]*jsonschema.Schema{
		"count": {Type: "integer"},
	})
	require.NoError(t, reg.Register(fp))

	expr := celexp.Expression("1 + 2")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "test-provider",
								Inputs: map[string]*spec.ValueRef{
									"count": {Expr: &expr},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "invalid-provider-input-type", f.RuleName)
	}
}

func TestLintProviderInputs_ActionInputs(t *testing.T) {
	reg := provider.NewRegistry()
	fp := newFakeProvider("shell", map[string]*jsonschema.Schema{
		"command": {Type: "string"},
	})
	fp.desc.Capabilities = []provider.Capability{provider.CapabilityAction}
	fp.desc.OutputSchemas = map[provider.Capability]*jsonschema.Schema{
		provider.CapabilityAction: {
			Type: "object",
			Properties: map[string]*jsonschema.Schema{
				"success": {Type: "boolean"},
			},
			Required: []string{"success"},
		},
	}
	require.NoError(t, reg.Register(fp))

	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"run": {
						Name:     "run",
						Provider: "shell",
						Inputs: map[string]*spec.ValueRef{
							"command":     {Literal: "echo hello"},
							"bogus_input": {Literal: "oops"},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	var unknownFindings []*Finding
	for _, f := range result.Findings {
		if f.RuleName == "unknown-provider-input" {
			unknownFindings = append(unknownFindings, f)
		}
	}
	require.Len(t, unknownFindings, 1)
	assert.Contains(t, unknownFindings[0].Message, "bogus_input")
}

func TestLintProviderInputs_MissingProviderSkipped(t *testing.T) {
	reg := provider.NewRegistry()

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "nonexistent",
								Inputs: map[string]*spec.ValueRef{
									"key": {Literal: "value"},
								},
							},
						},
					},
				},
			},
		},
	}

	result := &Result{Findings: make([]*Finding, 0)}
	lintProviderInputs(sol, result, reg)

	for _, f := range result.Findings {
		assert.NotEqual(t, "unknown-provider-input", f.RuleName)
	}
}

func TestCollectReferencedResolvers_DirectRslvr(t *testing.T) {
	resolverName := "myResolver"
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"field": {Resolver: &resolverName},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["myResolver"], "should detect direct rslvr reference")
}

func TestCollectReferencedResolvers_NestedRslvrInLiteral(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"value": {
									Literal: map[string]any{
										"body": map[string]any{
											"rslvr": "emailBody",
										},
										"subject": map[string]any{
											"rslvr": "emailSubject",
										},
									},
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["emailBody"], "should detect nested rslvr in literal map")
	assert.True(t, refs["emailSubject"], "should detect nested rslvr in literal map")
}

func TestCollectReferencedResolvers_NestedExprInLiteral(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"value": {
									Literal: map[string]any{
										"ts": map[string]any{
											"expr": "string(_.timestamp)",
										},
									},
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["timestamp"], "should detect resolver ref in nested expr")
}

func TestCollectReferencedResolvers_NestedInArray(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"source": {
					Name: "source",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs: map[string]*spec.ValueRef{
								"args": {
									Literal: []any{
										map[string]any{"rslvr": "env"},
									},
								},
							},
						}},
					},
				},
			},
		},
	}

	refs := collectReferencedResolvers(sol)
	assert.True(t, refs["env"], "should detect rslvr inside array elements")
}

func TestLintResolverSelfReferences(t *testing.T) {
	validationProv := newFakeProvider("validation", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	celProv := newFakeProvider("cel", map[string]*jsonschema.Schema{
		"expression": {Type: "string"},
	})
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})

	reg := provider.NewRegistry()
	_ = reg.Register(validationProv)
	_ = reg.Register(celProv)
	_ = reg.Register(staticProv)

	selfExpr := celexp.Expression("_.publicSiteCheck.statusCode == 200")
	correctExpr := celexp.Expression("__self.statusCode == 200")
	otherExpr := celexp.Expression("_.otherResolver.value == 'ok'")

	tests := []struct {
		name          string
		resolverName  string
		resolver      *resolver.Resolver
		expectFinding bool
		findingRule   string
	}{
		{
			name:         "validate self-reference via _.name triggers finding",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &selfExpr}},
					}},
				},
			},
			expectFinding: true,
			findingRule:   "resolver-self-reference",
		},
		{
			name:         "validate using __self is clean",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &correctExpr}},
					}},
				},
			},
			expectFinding: false,
		},
		{
			name:         "validate referencing other resolver is fine",
			resolverName: "publicSiteCheck",
			resolver: &resolver.Resolver{
				Type: "object",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Validate: &resolver.ValidatePhase{
					With: []resolver.ProviderValidation{{
						Provider: "validation",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: &otherExpr}},
					}},
				},
			},
			expectFinding: false,
		},
		{
			name:         "transform self-reference via _.name triggers finding",
			resolverName: "myValue",
			resolver: &resolver.Resolver{
				Type: "string",
				Resolve: &resolver.ResolvePhase{
					With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*spec.ValueRef{"value": {Literal: "test"}}}},
				},
				Transform: &resolver.TransformPhase{
					With: []resolver.ProviderTransform{{
						Provider: "cel",
						Inputs:   map[string]*spec.ValueRef{"expression": {Expr: exprPtr("_.myValue + '-suffix'")}},
					}},
				},
			},
			expectFinding: true,
			findingRule:   "resolver-self-reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{
				Spec: solution.Spec{
					Resolvers: map[string]*resolver.Resolver{
						tt.resolverName: tt.resolver,
					},
				},
			}

			result := Solution(sol, "test.yaml", reg)

			selfRefFindings := filterFindingsByRule(result, tt.findingRule)
			if tt.expectFinding {
				require.NotEmpty(t, selfRefFindings, "expected resolver-self-reference finding")
				assert.Contains(t, selfRefFindings[0].Message, tt.resolverName)
			} else {
				assert.Empty(t, selfRefFindings, "expected no resolver-self-reference finding")
			}
		})
	}
}

func exprPtr(s string) *celexp.Expression {
	e := celexp.Expression(s)
	return &e
}

func filterFindingsByRule(result *Result, rule string) []*Finding {
	if rule == "" {
		return nil
	}
	var out []*Finding
	for _, f := range result.Findings {
		if f.RuleName == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestLintNilProviderInput(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"has-nil-input": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs: map[string]*spec.ValueRef{
								"value":        {Literal: "ok"},
								"dangling-key": nil,
							},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "nil-provider-input")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "dangling-key")
	assert.Contains(t, findings[0].Message, "no value")
}

func TestLintEmptyTransformWith(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"empty-transform": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "empty-transform-with")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "empty")
}

func TestLintEmptyValidateWith(t *testing.T) {
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	reg := provider.NewRegistry()
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"empty-validate": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "empty-validate-with")
	require.Len(t, findings, 1)
	assert.Contains(t, findings[0].Message, "empty")
}

func TestLintNullResolverValue(t *testing.T) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"map": nil,
				"hello": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "world"}},
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", reg)

	findings := filterFindingsByRule(result, "null-resolver")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityError, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "null value")
	assert.Contains(t, findings[0].Location, "map")
}

func BenchmarkLintNullResolver(b *testing.B) {
	reg := provider.NewRegistry()
	staticProv := newFakeProvider("static", map[string]*jsonschema.Schema{
		"value": {Type: "string"},
	})
	_ = reg.Register(staticProv)

	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"null_resolver": nil,
				"valid": {
					Type: "string",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "static",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "ok"}},
						}},
					},
				},
			},
		},
	}

	b.ResetTimer()
	for b.Loop() {
		_ = Solution(sol, "bench.yaml", reg)
	}
}
